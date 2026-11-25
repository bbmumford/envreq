// Package envreq provides a centralized environment variable registry with validation.
//
// Any package can declare its env needs close to the code that uses them.
// A single validation step in main() lists what is missing or invalid.
// Sensitive marking ensures reports never expose secrets in CLI output or logs.
//
// Lifecycle:
//  1. Call Check() anywhere to register and load environment variables
//  2. Call MustValidate() early to validate all registered vars (exits if required vars missing)
//  3. Call Freeze() right before serving to lock the registry
//  4. After Freeze():
//     - Re-accessing already registered vars: allowed (normal caching)
//     - New OPTIONAL vars: allowed with warning logged
//     - New REQUIRED vars: immediate panic with full environment report
package envreq

import (
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Requirement declares an environment variable need with validation and metadata.
type Requirement struct {
	Name        string             // ENV var name, e.g. "STRIPE_API_KEY"
	Source      string             // Owning package/component for reporting
	Description string             // Short help text for humans
	Optional    bool               // Default is required
	Default     string             // Optional default if missing
	Validate    func(string) error // Optional value validator
	Sensitive   bool               // If true, never show value, redact in reports
	// EmptyIsUnset makes a variable that is set to the empty string count as unset:
	// Default applies, and a required variable reports missing. Without it an empty
	// value is a present value. Like Optional and Sensitive, it holds for a name
	// once any declaration of the name sets it.
	EmptyIsUnset bool
}

// Result contains the loaded and validated environment variable.
type Result struct {
	Requirement
	Present bool   // whether env or default was available
	Value   string // loaded value (never printed in reports if Sensitive)
	Err     error  // validator error (if any)
}

var (
	mu     sync.RWMutex
	reg    = map[string]Requirement{}
	cache  = map[string]Result{}
	reads  = map[string]envRead{}
	gens   = map[string]uint64{}
	frozen atomic.Bool
)

// envRead is a variable's environment value as first read: the process reads
// each name once, so every declaration of it judges the same value.
type envRead struct {
	value string
	set   bool
}

// Check declares (or references) a requirement, reads & validates immediately,
// caches the value, and returns a Result you can use inline like os.Getenv.
//
// Every declaration of a name merges into one requirement, the strictest of
// them (required wins over optional; Sensitive and EmptyIsUnset win once set;
// the first Description, Source, Validate and Default given are kept), and the
// Result is judged against that merged requirement on every Check, so a later,
// stricter declaration is enforced: a variable first declared optional and
// then required reports missing, one later declared Sensitive is redacted,
// and a validator added later runs. The environment itself is read once per
// name.
func Check(r Requirement) Result {
	if frozen.Load() {
		// Check if this is a new registration after freeze
		mu.RLock()
		_, exists := reg[r.Name]
		mu.RUnlock()

		if !exists {
			// New registration after freeze
			if r.Optional {
				// Optional: just log a warning
				log.Printf("⚠️  envreq: Optional environment variable registered after Freeze(): %s (from %s)", r.Name, r.Source)
			} else {
				// Required: panic immediately with full context
				log.Printf("🚨 envreq: REQUIRED environment variable registered after Freeze(): %s (from %s)", r.Name, r.Source)
				log.Println("📋 envreq: Complete environment state at time of panic:")

				// Show current state before panicking
				results := CheckAll()
				Report(os.Stderr, results)

				panic(fmt.Sprintf(
					"envreq: REQUIRED environment variable '%s' registered after Freeze() (from: %s)\n"+
						"All required environment variables must be registered before Freeze().\n"+
						"Move this Check() call earlier in initialization.",
					r.Name, r.Source,
				))
			}
		}

		// If already registered, allow re-access (normal caching behavior)
	}

	mu.Lock()
	// Merge into registry (stricter wins)
	if existing, ok := reg[r.Name]; ok {
		merged := existing
		// Required wins over optional
		if !existing.Optional || !r.Optional {
			merged.Optional = false
		}
		// Fill in missing metadata
		if merged.Description == "" && r.Description != "" {
			merged.Description = r.Description
		}
		if merged.Source == "" && r.Source != "" {
			merged.Source = r.Source
		}
		if merged.Validate == nil && r.Validate != nil {
			merged.Validate = r.Validate
		}
		if merged.Default == "" && r.Default != "" {
			merged.Default = r.Default
		}
		// Sensitive wins (more restrictive)
		if existing.Sensitive || r.Sensitive {
			merged.Sensitive = true
		}
		if existing.EmptyIsUnset || r.EmptyIsUnset {
			merged.EmptyIsUnset = true
		}
		reg[r.Name] = merged
		r = merged
	} else {
		reg[r.Name] = r
	}
	gens[r.Name]++
	read, readBefore := reads[r.Name]
	if !readBefore {
		read.value, read.set = os.LookupEnv(r.Name)
		reads[r.Name] = read
	}
	gen := gens[r.Name]
	mu.Unlock()

	// The Result is judged outside the lock, so a validator may itself call
	// Check; it is stored only if no other declaration merged in meanwhile,
	// else it is judged again against the newer merge.
	for {
		res := judge(r, read)
		mu.Lock()
		if gens[r.Name] == gen {
			cache[r.Name] = res
			mu.Unlock()
			return res
		}
		r, gen = reg[r.Name], gens[r.Name]
		mu.Unlock()
	}
}

// judge loads and validates one requirement against its environment read.
func judge(r Requirement, read envRead) Result {
	val, ok := read.value, read.set
	if ok && val == "" && r.EmptyIsUnset {
		ok = false
	}
	if !ok && r.Default != "" {
		val, ok = r.Default, true
	}

	var verr error
	if ok && r.Validate != nil {
		verr = r.Validate(val)
	}

	return Result{
		Requirement: r,
		Present:     ok,
		Value:       val,
		Err:         verr,
	}
}

// Value fetches a cached value by name. Returns empty string and false if not found.
func Value(name string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()

	if res, ok := cache[name]; ok {
		return res.Value, res.Present
	}
	return "", false
}

// CheckAll returns a snapshot of all known results (merged from prior Check calls).
func CheckAll() []Result {
	mu.RLock()

	// Copy cached results first (populated by Check calls)
	out := make([]Result, 0, len(reg))
	unchecked := make([]Requirement, 0)

	for name, req := range reg {
		if res, ok := cache[name]; ok {
			out = append(out, res)
		} else {
			unchecked = append(unchecked, req)
		}
	}
	mu.RUnlock()

	// Check any requirements that haven't been loaded yet
	for _, req := range unchecked {
		res := Check(req)
		out = append(out, res)
	}

	// Sort by name for consistent output
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out
}

// Report writes a safe report (no values printed; sensitive redacted).
// Returns count of missing required variables.
func Report(w io.Writer, results []Result) (missing int) {
	showValues := os.Getenv("ENVREQ_SHOW_VALUES") == "1"

	// row writes one line of the table. The count Report returns is what its
	// callers act on (MustValidate exits on it), and a line that fails to write
	// leaves that count as it is, so each write's error is dropped.
	row := func(cols ...any) {
		_, _ = fmt.Fprintf(w, "%-20s %-12s %-8s %-9s %-8s %s\n", cols...)
	}
	row("ENV", "SOURCE", "REQUIRED", "SENSITIVE", "STATUS", "DETAILS")
	row(strings.Repeat("-", 20),
		strings.Repeat("-", 12),
		strings.Repeat("-", 8),
		strings.Repeat("-", 9),
		strings.Repeat("-", 8),
		strings.Repeat("-", 20))

	for _, res := range results {
		required := "no"
		if !res.Optional {
			required = "yes"
		}

		sensitive := "no"
		if res.Sensitive {
			sensitive = "yes"
		}

		status := "ok"
		details := res.Description

		if !res.Present && !res.Optional {
			status = "missing"
			missing++
		} else if res.Err != nil {
			status = "invalid"
			details = fmt.Sprintf("Error: %v", res.Err)
			if !res.Optional {
				missing++
			}
		} else if showValues && res.Present && !res.Sensitive {
			// Only show values in debug mode for non-sensitive vars
			if len(res.Value) > 20 {
				details = fmt.Sprintf("%s (value: %s...)", res.Description, res.Value[:17])
			} else {
				details = fmt.Sprintf("%s (value: %s)", res.Description, res.Value)
			}
		} else if showValues && res.Present && res.Sensitive {
			// Show redacted value for sensitive vars in debug mode
			if len(res.Value) >= 4 {
				details = fmt.Sprintf("%s (value: ••••%s)", res.Description, res.Value[len(res.Value)-4:])
			} else {
				details = fmt.Sprintf("%s (value: ••••)", res.Description)
			}
		}

		row(res.Name, res.Source, required, sensitive, status, details)
	}

	return missing
}

// MustValidate runs CheckAll + Report and exits 2 if any required item is missing/invalid.
func MustValidate() {
	results := CheckAll()
	missing := Report(os.Stderr, results)
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "\n%d required environment variable(s) missing or invalid\n", missing)
		os.Exit(2)
	}
}

// Freeze prevents new required registrations after validation.
// Call this right before the application starts serving external traffic.
// - New REQUIRED variables: panic immediately with full environment report
// - New OPTIONAL variables: log warning but allow
// - Re-accessing existing variables: always allowed (normal caching)
func Freeze() {
	frozen.Store(true)
	log.Println("envreq: Registry frozen - new required registrations will panic")
}

// Reset clears all registrations and cache. Useful for testing.
func Reset() {
	mu.Lock()
	defer mu.Unlock()

	reg = map[string]Requirement{}
	cache = map[string]Result{}
	reads = map[string]envRead{}
	gens = map[string]uint64{}
	frozen.Store(false)
}

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
    frozen atomic.Bool
)

// Check declares (or references) a requirement, reads & validates immediately,
// caches the value, and returns a Result you can use inline like os.Getenv.
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
        reg[r.Name] = merged
        r = merged
    } else {
        reg[r.Name] = r
    }
    mu.Unlock()

    // Check if already cached
    mu.RLock()
    if cached, ok := cache[r.Name]; ok {
        mu.RUnlock()
        return cached
    }
    mu.RUnlock()

    // Load & validate, cache the Result
    val, ok := os.LookupEnv(r.Name)
    if !ok && r.Default != "" {
        val, ok = r.Default, true
    }

    var verr error
    if ok && r.Validate != nil {
        verr = r.Validate(val)
    }

    res := Result{
        Requirement: r,
        Present:     ok,
        Value:       val,
        Err:         verr,
    }

    mu.Lock()
    cache[r.Name] = res
    mu.Unlock()

    return res
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

    fmt.Fprintf(w, "%-20s %-12s %-8s %-9s %-8s %s\n",
        "ENV", "SOURCE", "REQUIRED", "SENSITIVE", "STATUS", "DETAILS")
    fmt.Fprintf(w, "%-20s %-12s %-8s %-9s %-8s %s\n",
        strings.Repeat("-", 20),
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

        fmt.Fprintf(w, "%-20s %-12s %-8s %-9s %-8s %s\n",
            res.Name, res.Source, required, sensitive, status, details)
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
    frozen.Store(false)
}

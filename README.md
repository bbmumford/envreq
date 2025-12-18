# envreq

A Go package for centralized environment variable management with validation, freeze lifecycle, and sensitive value protection.

## Overview

`envreq` provides a registry-based approach to environment variable management where packages declare their environment needs close to the code that uses them. A single validation step in `main()` identifies missing or invalid variables before the application starts.

## Features

- **Centralized Registry**: Declare environment variable requirements anywhere, validate once
- **Validation**: Built-in validators for common types (URL, Duration, Port, Base64, etc.)
- **Sensitive Values**: Mark secrets to prevent accidental logging or display
- **Freeze Lifecycle**: Lock the registry to catch late registrations
- **Safe Reporting**: Generate environment reports without exposing values

## Installation

```bash
go get github.com/bbmumford/envreq
```

## Usage

### Basic Check

```go
package main

import (
    "github.com/bbmumford/envreq"
)

func main() {
    // Check required environment variable
    result := envreq.Check(envreq.Requirement{
        Name:        "DATABASE_URL",
        Source:      "database",
        Description: "PostgreSQL connection string",
        Validate:    envreq.URL,
    })
    
    // Use the value
    db.Connect(result.Value)
}
```

### With Defaults and Validation

```go
// Optional with default
port := envreq.Check(envreq.Requirement{
    Name:     "PORT",
    Source:   "server",
    Optional: true,
    Default:  "8080",
    Validate: envreq.Port,
})

// Sensitive value (never printed in reports)
apiKey := envreq.Check(envreq.Requirement{
    Name:        "API_KEY",
    Source:      "auth",
    Description: "External API key",
    Sensitive:   true,
    Validate:    envreq.NotEmpty,
})
```

### Lifecycle

```go
func main() {
    // 1. Check all environment variables during initialization
    //    (happens automatically as packages call Check())
    
    // 2. Validate all registered variables
    envreq.MustValidate()  // Exits if required vars missing/invalid
    
    // 3. Freeze the registry before serving traffic
    envreq.Freeze()
    
    // After Freeze():
    // - Re-accessing existing vars: allowed (normal caching)
    // - New OPTIONAL vars: warning logged
    // - New REQUIRED vars: immediate panic with full report
    
    startServer()
}
```

### Validators

Built-in validators:

| Validator | Description |
|-----------|-------------|
| `envreq.URL` | Valid URL with scheme and host |
| `envreq.Duration` | Go duration string (e.g., "30s", "5m") |
| `envreq.Port` | Valid port number (1-65535) |
| `envreq.NotEmpty` | Non-empty, non-whitespace value |
| `envreq.Base64` | Valid base64 encoding |
| `envreq.OneOf("a", "b")` | Value must be one of the options |

### Reporting

```go
// Get all results
results := envreq.CheckAll()

// Print safe report (values redacted for sensitive vars)
missing := envreq.Report(os.Stderr, results)
if missing > 0 {
    os.Exit(2)
}
```

Example output:
```
ENV                  SOURCE       REQUIRED SENSITIVE STATUS   DETAILS
-------------------- ------------ -------- --------- -------- --------------------
DATABASE_URL         database     yes      no        ok       Database connection
API_KEY              auth         yes      yes       ok       API key
DEBUG_MODE           app          no       no        ok       Enable debug logging
MISSING_VAR          config       yes      no        missing  Required config value
```

### Caching

After a variable is checked, its value is cached:

```go
// Check (loads and caches)
envreq.Check(envreq.Requirement{Name: "APP_NAME", Source: "app"})

// Retrieve cached value later
name, ok := envreq.Value("APP_NAME")
```

## API Reference

### Types

```go
type Requirement struct {
    Name        string             // Environment variable name
    Source      string             // Owning package/component
    Description string             // Human-readable description
    Optional    bool               // If true, missing is not an error
    Default     string             // Default value if not set
    Validate    func(string) error // Optional validator function
    Sensitive   bool               // If true, value is never displayed
}

type Result struct {
    Requirement
    Present bool   // Whether env or default was available
    Value   string // Loaded value (redacted in reports if Sensitive)
    Err     error  // Validation error if any
}
```

### Functions

```go
// Check declares and loads an environment variable
func Check(r Requirement) Result

// Value retrieves a cached value by name
func Value(name string) (string, bool)

// CheckAll returns all registered results
func CheckAll() []Result

// Report writes a safe report to the writer
func Report(w io.Writer, results []Result) (missing int)

// MustValidate validates and exits if required vars are missing
func MustValidate()

// Freeze locks the registry (new required vars will panic)
func Freeze()

// Reset clears all registrations (for testing)
func Reset()
```

## Best Practices

1. **Declare early**: Check environment variables during package initialization
2. **Validate once**: Call `MustValidate()` in main before starting
3. **Freeze before serving**: Call `Freeze()` before accepting traffic
4. **Mark secrets**: Always set `Sensitive: true` for API keys, passwords, etc.
5. **Use validators**: Validate format early to catch misconfigurations

## Testing

```go
func TestMyFunction(t *testing.T) {
    // Reset registry for isolated tests
    envreq.Reset()
    
    // Set test environment
    t.Setenv("MY_VAR", "test-value")
    
    // Test your code
    result := envreq.Check(envreq.Requirement{
        Name:   "MY_VAR",
        Source: "test",
    })
    
    if result.Value != "test-value" {
        t.Error("unexpected value")
    }
}
```

## License

MIT

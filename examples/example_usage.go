// Example: Migration from os.Getenv to envreq.Check
//
// This shows how to gradually migrate existing code to use the centralized
// environment variable registry with validation.

package main

import (
    "fmt"
    "log"
    "os"
    "time"

    "github.com/bbmumford/envreq"
)

// Before: Using os.Getenv directly
func oldWay() {
    apiKey := os.Getenv("STRIPE_API_KEY")
    if apiKey == "" {
        log.Fatal("STRIPE_API_KEY is required")
    }

    timeout := os.Getenv("AUTH_TIMEOUT")
    if timeout == "" {
        timeout = "30s"
    }

    baseURL := os.Getenv("AUTH_SERVICE_URL")
    if baseURL == "" {
        log.Fatal("AUTH_SERVICE_URL is required")
    }

    // Use variables...
    _ = apiKey
    _ = timeout
    _ = baseURL
}

// After: Using envreq.Check (drop-in replacement)
func newWay() {
    // Drop-in replacement for os.Getenv with automatic registration
    apiKey := envreq.Check(envreq.Requirement{
        Name:        "STRIPE_API_KEY",
        Source:      "payments",
        Description: "Stripe secret API key",
        Sensitive:   true, // Never shows in reports
        Validate:    envreq.NotEmpty,
    }).Value

    timeout := envreq.Check(envreq.Requirement{
        Name:        "AUTH_TIMEOUT",
        Source:      "authclient",
        Description: "Authentication request timeout",
        Optional:    true,
        Default:     "30s",
        Validate:    envreq.Duration,
    }).Value

    baseURL := envreq.Check(envreq.Requirement{
        Name:        "AUTH_SERVICE_URL",
        Source:      "authclient",
        Description: "Base URL for authentication service",
        Validate:    envreq.URL,
    }).Value

    // Use variables same as before...
    _ = apiKey
    _ = timeout
    _ = baseURL
}

// If you need to check presence or validation status
func advancedUsage() {
    result := envreq.Check(envreq.Requirement{
        Name:     "DATABASE_URL",
        Source:   "database",
        Validate: envreq.URL,
    })

    if !result.Present {
        log.Printf("DATABASE_URL not set, using default connection")
        return
    }

    if result.Err != nil {
        log.Fatalf("DATABASE_URL validation failed: %v", result.Err)
    }

    dbURL := result.Value
    _ = dbURL
}

// Main function showing validation integration
func main() {
    // Early startup - packages call envreq.Check during init/setup
    newWay()
    advancedUsage()

    // Additional app-specific checks
    appEnv := envreq.Check(envreq.Requirement{
        Name:        "APP_ENV",
        Source:      "app",
        Description: "Application environment",
        Default:     "development",
        Validate:    envreq.OneOf("production", "development", "test"),
    }).Value

    port := envreq.Check(envreq.Requirement{
        Name:        "PORT",
        Source:      "server",
        Description: "HTTP server port",
        Default:     "8080",
        Validate:    envreq.Port,
    }).Value

    // Validate all environment variables at once
    // This will print a report and exit(2) if any required vars are missing
    envreq.MustValidate()

    // Optional: prevent new registrations after validation
    envreq.Freeze()

    fmt.Printf("Starting server on port %s in %s environment\n", port, appEnv)

    // Start your server...
    time.Sleep(100 * time.Millisecond) // Simulate server startup
}

// Package-level initialization example
func init() {
    // Packages can declare their env needs during init
    envreq.Check(envreq.Requirement{
        Name:        "LOG_LEVEL",
        Source:      "logging",
        Description: "Logging level",
        Optional:    true,
        Default:     "info",
        Validate:    envreq.OneOf("debug", "info", "warn", "error"),
    })
}

// Testing example
func ExampleTesting() {
    // In tests, use t.Setenv and call MustValidate in test setup
    os.Setenv("TEST_VAR", "test-value")

    // Reset registry for clean test
    envreq.Reset()

    // Use Check in code under test
    result := envreq.Check(envreq.Requirement{
        Name:   "TEST_VAR",
        Source: "test",
    })

    fmt.Printf("Got: %s\n", result.Value)
    // Output: Got: test-value
}

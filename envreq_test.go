package envreq_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/bbmumford/envreq"
)

func TestCheck(t *testing.T) {
	// Clean slate
	envreq.Reset()

	// Set test env vars
	t.Setenv("TEST_URL", "https://api.example.com")
	t.Setenv("TEST_TIMEOUT", "30s")
	t.Setenv("TEST_SECRET", "sk_test_1234567890")

	// Test required env var
	result := envreq.Check(envreq.Requirement{
		Name:        "TEST_URL",
		Source:      "client",
		Description: "API base URL",
		Validate:    envreq.URL,
	})

	if !result.Present {
		t.Error("Expected TEST_URL to be present")
	}
	if result.Value != "https://api.example.com" {
		t.Errorf("Expected value 'https://api.example.com', got '%s'", result.Value)
	}
	if result.Err != nil {
		t.Errorf("Unexpected validation error: %v", result.Err)
	}

	// Test optional env var with default
	result2 := envreq.Check(envreq.Requirement{
		Name:     "TEST_MISSING",
		Source:   "client",
		Optional: true,
		Default:  "default-value",
	})

	if !result2.Present {
		t.Error("Expected default to make TEST_MISSING present")
	}
	if result2.Value != "default-value" {
		t.Errorf("Expected default value, got '%s'", result2.Value)
	}

	// Test sensitive env var
	result3 := envreq.Check(envreq.Requirement{
		Name:        "TEST_SECRET",
		Source:      "payments",
		Description: "API secret key",
		Sensitive:   true,
		Validate:    envreq.NotEmpty,
	})

	if !result3.Present {
		t.Error("Expected TEST_SECRET to be present")
	}
	if !result3.Sensitive {
		t.Error("Expected TEST_SECRET to be marked sensitive")
	}
}

func TestValue(t *testing.T) {
	envreq.Reset()
	t.Setenv("TEST_VAL", "test-value")

	// Check first
	envreq.Check(envreq.Requirement{
		Name:   "TEST_VAL",
		Source: "test",
	})

	// Then retrieve
	val, ok := envreq.Value("TEST_VAL")
	if !ok {
		t.Error("Expected TEST_VAL to be cached")
	}
	if val != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", val)
	}

	// Non-existent
	val2, ok2 := envreq.Value("NOT_EXISTS")
	if ok2 {
		t.Error("Expected NOT_EXISTS to not be found")
	}
	if val2 != "" {
		t.Error("Expected empty string for non-existent var")
	}
}

func TestValidators(t *testing.T) {
	tests := []struct {
		name      string
		validator func(string) error
		value     string
		wantError bool
	}{
		{"valid URL", envreq.URL, "https://example.com", false},
		{"invalid URL", envreq.URL, "not-a-url", true},
		{"empty URL", envreq.URL, "", true},
		{"valid duration", envreq.Duration, "30s", false},
		{"invalid duration", envreq.Duration, "30x", true},
		{"not empty valid", envreq.NotEmpty, "value", false},
		{"not empty invalid", envreq.NotEmpty, "", true},
		{"not empty whitespace", envreq.NotEmpty, "   ", true},
		{"valid port", envreq.Port, "8080", false},
		{"invalid port", envreq.Port, "99999", true},
		{"valid base64", envreq.Base64, "dGVzdA==", false},
		{"invalid base64", envreq.Base64, "test@#$", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validator(tt.value)
			if (err != nil) != tt.wantError {
				t.Errorf("validator() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestOneOf(t *testing.T) {
	validator := envreq.OneOf("production", "development", "test")

	if err := validator("production"); err != nil {
		t.Errorf("Expected 'production' to be valid: %v", err)
	}

	if err := validator("invalid"); err == nil {
		t.Error("Expected 'invalid' to fail validation")
	}
}

func TestFreeze(t *testing.T) {
	envreq.Reset()

	// Should work before freeze
	envreq.Check(envreq.Requirement{Name: "TEST1", Source: "test"})

	// Freeze
	envreq.Freeze()

	// Should panic after freeze for new required var
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic after freeze for required var")
		}
		envreq.Reset() // Clean up for other tests
	}()

	envreq.Check(envreq.Requirement{Name: "TEST2", Source: "test"})
}

func TestReport(t *testing.T) {
	envreq.Reset()
	t.Setenv("PRESENT_VAR", "value")
	t.Setenv("INVALID_VAR", "not-a-url")

	// Set up some test requirements
	envreq.Check(envreq.Requirement{
		Name:        "PRESENT_VAR",
		Source:      "test",
		Description: "A present variable",
	})

	envreq.Check(envreq.Requirement{
		Name:        "MISSING_VAR",
		Source:      "test",
		Description: "A missing required variable",
	})

	envreq.Check(envreq.Requirement{
		Name:        "INVALID_VAR",
		Source:      "test",
		Description: "Invalid URL",
		Validate:    envreq.URL,
	})

	results := envreq.CheckAll()
	var buf bytes.Buffer
	missing := envreq.Report(&buf, results)

	output := buf.String()

	// Check that report contains expected headers
	if !strings.Contains(output, "ENV") || !strings.Contains(output, "SOURCE") {
		t.Error("Report missing expected headers")
	}

	// Should have 2 missing (MISSING_VAR and INVALID_VAR)
	if missing != 2 {
		t.Errorf("Expected 2 missing variables, got %d", missing)
	}

	// Test debug mode
	os.Setenv("ENVREQ_SHOW_VALUES", "1")
	defer os.Unsetenv("ENVREQ_SHOW_VALUES")

	var debugBuf bytes.Buffer
	envreq.Report(&debugBuf, results)
	// Just ensure it doesn't crash in debug mode
}

package envreq

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// URL validates that the value is a valid URL.
func URL(v string) error {
	if v == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	parsed, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme == "" {
		return fmt.Errorf("URL must have a scheme (http/https)")
	}

	if parsed.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}

// Duration validates that the value is a valid Go duration string.
func Duration(v string) error {
	if v == "" {
		return fmt.Errorf("duration cannot be empty")
	}

	_, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	return nil
}

// OneOf returns a validator that checks the value is one of the given options.
func OneOf(options ...string) func(string) error {
	return func(v string) error {
		for _, option := range options {
			if v == option {
				return nil
			}
		}
		return fmt.Errorf("must be one of: %s", strings.Join(options, ", "))
	}
}

// NotEmpty validates that the value is not empty or only whitespace.
func NotEmpty(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("value cannot be empty")
	}
	return nil
}

// Port validates that the value is a valid port number (1-65535).
func Port(v string) error {
	if v == "" {
		return fmt.Errorf("port cannot be empty")
	}

	// Simple validation without importing strconv
	if len(v) == 0 || len(v) > 5 {
		return fmt.Errorf("invalid port number")
	}

	// Check all digits
	for _, r := range v {
		if r < '0' || r > '9' {
			return fmt.Errorf("port must be numeric")
		}
	}

	// Basic range check (rough)
	if v == "0" || (len(v) == 5 && v > "65535") {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	return nil
}

// Base64 validates that the value is valid base64 encoding.
func Base64(v string) error {
	if v == "" {
		return fmt.Errorf("base64 value cannot be empty")
	}

	// Basic base64 character validation
	for _, r := range v {
		if !((r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '+' || r == '/' || r == '=') {
			return fmt.Errorf("invalid base64 character")
		}
	}

	// Check padding
	padding := strings.Count(v, "=")
	if padding > 2 {
		return fmt.Errorf("invalid base64 padding")
	}

	return nil
}

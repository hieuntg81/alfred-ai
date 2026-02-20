package tool

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// RequireField returns an error if the string value is empty.
func RequireField(name, value string) error {
	if value == "" {
		return fmt.Errorf("'%s' is required", name)
	}
	return nil
}

// RequireFields validates multiple required string fields at once.
// keys and values must have the same length.
func RequireFields(kvs ...string) error {
	if len(kvs)%2 != 0 {
		return fmt.Errorf("RequireFields: odd number of arguments")
	}
	for i := 0; i < len(kvs); i += 2 {
		if kvs[i+1] == "" {
			return fmt.Errorf("'%s' is required", kvs[i])
		}
	}
	return nil
}

// ValidateRange checks that value is within [min, max]. Returns nil on success.
func ValidateRange(name string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be %d-%d", name, min, max)
	}
	return nil
}

// ValidatePositive checks that value is > 0.
func ValidatePositive(name string, value int) error {
	if value <= 0 {
		return fmt.Errorf("'%s' is required and must be > 0", name)
	}
	return nil
}

// ValidateEnum checks that value is one of the allowed values.
// An empty value is allowed (treated as "not set").
func ValidateEnum(name, value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q (want: %s)", name, value, joinComma(allowed))
}

// ValidateAll returns the first non-nil error from the given list.
// Useful for combining multiple validation checks:
//
//	if err := ValidateAll(RequireField("url", p.URL), ValidateURL("url", p.URL)); err != nil { ... }
func ValidateAll(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidateURL checks that value is a valid absolute HTTP(S) URL.
// An empty value is allowed (use RequireField to enforce presence).
func ValidateURL(name, value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %s", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid %s: scheme must be http or https", name)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid %s: missing host", name)
	}
	return nil
}

// ValidateMaxLength checks that value does not exceed max bytes.
// An empty value always passes.
func ValidateMaxLength(name, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s exceeds maximum length of %d", name, max)
	}
	return nil
}

// ValidateJSON checks that value is syntactically valid JSON.
// An empty value is allowed (use RequireField to enforce presence).
func ValidateJSON(name, value string) error {
	if value == "" {
		return nil
	}
	if !json.Valid([]byte(value)) {
		return fmt.Errorf("invalid %s: not valid JSON", name)
	}
	return nil
}

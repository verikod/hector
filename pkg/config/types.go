package config

import (
	"fmt"
	"time"
)

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool {
	return &b
}

// IntPtr returns a pointer to the given int value.
func IntPtr(i int) *int {
	return &i
}

// BoolValue returns the value of the bool pointer, or the default if nil.
func BoolValue(b *bool, defaultValue bool) bool {
	if b == nil {
		return defaultValue
	}
	return *b
}

// IntValue returns the value of the int pointer, or the default if nil.
func IntValue(i *int, defaultValue int) int {
	if i == nil {
		return defaultValue
	}
	return *i
}

// Duration is a time.Duration that supports YAML parsing.
//
// Supports formats like: "1s", "5m", "2h", "100ms", "1h30m"
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		// Try as integer (nanoseconds)
		var ns int64
		if err := unmarshal(&ns); err != nil {
			return fmt.Errorf("duration must be a string (e.g., '1s') or integer (nanoseconds)")
		}
		*d = Duration(ns)
		return nil
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Duration returns the time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String returns the string representation.
func (d Duration) String() string {
	return time.Duration(d).String()
}

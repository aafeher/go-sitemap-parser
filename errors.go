package sitemap

import "fmt"

// ConfigError is returned when a configuration setter receives an invalid value.
// Callers can inspect the Field to determine which configuration setting caused the error.
//
// Example usage:
//
//	var cfgErr *sitemap.ConfigError
//	if errors.As(err, &cfgErr) {
//	    fmt.Println("bad config field:", cfgErr.Field)
//	}
type ConfigError struct {
	// Field is the configuration field name (e.g. "maxDepth", "follow", "rules").
	Field string
	// Err is the underlying validation error.
	Err error
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config %q: %s", e.Field, e.Err)
}

// Unwrap returns the underlying error, enabling errors.Is / errors.As traversal.
func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NetworkError is returned when an HTTP fetch fails.
// Callers can inspect URL to determine which resource could not be retrieved.
//
// Example usage:
//
//	var netErr *sitemap.NetworkError
//	if errors.As(err, &netErr) {
//	    fmt.Println("failed to fetch:", netErr.URL)
//	}
type NetworkError struct {
	// URL is the URL that was being fetched when the error occurred.
	URL string
	// Err is the underlying network or HTTP error.
	Err error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("fetch %q: %s", e.URL, e.Err)
}

// Unwrap returns the underlying error, enabling errors.Is / errors.As traversal.
func (e *NetworkError) Unwrap() error {
	return e.Err
}

// ParseError is returned when XML or gzip parsing of a sitemap document fails.
// Callers can inspect URL to determine which sitemap could not be parsed.
//
// Example usage:
//
//	var parseErr *sitemap.ParseError
//	if errors.As(err, &parseErr) {
//	    fmt.Println("failed to parse sitemap:", parseErr.URL)
//	}
type ParseError struct {
	// URL is the sitemap URL that was being parsed when the error occurred.
	// May be empty when the error is not tied to a specific URL (e.g. max depth reached).
	URL string
	// Err is the underlying parse error.
	Err error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse %q: %s", e.URL, e.Err)
}

// Unwrap returns the underlying error, enabling errors.Is / errors.As traversal.
func (e *ParseError) Unwrap() error {
	return e.Err
}

// ValidationError is returned when a URL or field value fails validation.
// Callers can inspect URL to determine which value was rejected.
//
// Example usage:
//
//	var valErr *sitemap.ValidationError
//	if errors.As(err, &valErr) {
//	    fmt.Println("invalid URL:", valErr.URL)
//	}
type ValidationError struct {
	// URL is the URL value being validated.
	// May be empty for field-level errors where no specific URL is available.
	URL string
	// Err is the underlying validation error.
	Err error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validate %q: %s", e.URL, e.Err)
}

// Unwrap returns the underlying error, enabling errors.Is / errors.As traversal.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

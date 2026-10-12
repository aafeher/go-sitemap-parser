package sitemap

import (
	"errors"
	"fmt"
)

// The errors below stand for the limits of a Parse or ParseContext call. The error that is
// recorded when a limit is reached wraps the one of that limit, so errors.Is tells which
// limit it was without the message of the error being read:
//
//	for _, err := range s.GetErrors() {
//	    if errors.Is(err, sitemap.ErrMaxURLs) {
//	        fmt.Println("the URLs are not all there:", err)
//	    }
//	}
//
// The message of the recorded error tells the limit and its value, as in
// `parse "https://example.com/sitemap.xml": limit of 100000 URLs reached`.
var (
	// ErrMaxResponseSize is wrapped by the error of a document that is larger than the limit
	// set with SetMaxResponseSize: the *NetworkError of a response that exceeds it, and the
	// *ParseError of gzip content that exceeds it once it is decompressed.
	ErrMaxResponseSize = errors.New("sitemap: maximum response size exceeded")

	// ErrMaxDepth is wrapped by the *ParseError of a sitemap index whose sitemaps were not
	// followed because the limit set with SetMaxDepth is reached.
	ErrMaxDepth = errors.New("sitemap: maximum depth reached")

	// ErrMaxSitemaps is wrapped by the *ParseError which tells that the limit set with
	// SetMaxSitemaps kept the call from fetching or from listing a sitemap.
	ErrMaxSitemaps = errors.New("sitemap: maximum number of sitemaps reached")

	// ErrMaxURLs is wrapped by the *ParseError which tells that the limit set with SetMaxURLs
	// kept the call from collecting a URL.
	ErrMaxURLs = errors.New("sitemap: maximum number of URLs reached")

	// ErrMaxValidationErrors is wrapped by the *ParseError which tells that the limit set with
	// SetMaxValidationErrors kept the call from recording a *ValidationError.
	ErrMaxValidationErrors = errors.New("sitemap: maximum number of validation errors reached")
)

// limitError is the error of a limit that was reached. Its message tells the limit and the
// value the limit has, and errors.Is matches it with the error that stands for the limit: one
// of the Err variables above.
type limitError struct {
	limit   error
	message string
}

// limitErrorf returns the error of a limit that was reached, with the message format and args
// make up.
func limitErrorf(limit error, format string, args ...any) error {
	return &limitError{limit: limit, message: fmt.Sprintf(format, args...)}
}

func (e *limitError) Error() string {
	return e.message
}

// Unwrap returns the error that stands for the limit.
func (e *limitError) Unwrap() error {
	return e.limit
}

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

// Error returns the message of the error, which names the configuration field and tells what
// is wrong with the value it was given: `config "maxDepth": must be greater than 0, got 0`.
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
// A response that is larger than the limit set with SetMaxResponseSize is such a failure: Err
// wraps ErrMaxResponseSize then, which errors.Is matches.
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

// Error returns the message of the error, which names the URL and tells why it could not be
// fetched: `fetch "https://example.com/sitemap.xml": received HTTP status 404`.
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
// It is also the error of a parse that was not carried through for another reason: the
// sitemaps a sitemap index lists were not followed because the depth limit is reached,
// sitemaps, URLs or validation errors were left out because the limit on the sitemaps, on the
// URLs or on the validation errors of a call is reached, or the call was cut short because
// its context is done. In the latter case Err is the error of the context, so errors.Is
// matches context.Canceled and context.DeadlineExceeded. When a limit is reached, Err wraps
// the error that stands for the limit, which errors.Is matches as well: ErrMaxDepth,
// ErrMaxSitemaps, ErrMaxURLs or ErrMaxValidationErrors, and ErrMaxResponseSize for gzip
// content that is too large.
//
// The error holds no text longer than 4096 bytes, like a *ValidationError.
//
// Example usage:
//
//	var parseErr *sitemap.ParseError
//	if errors.As(err, &parseErr) {
//	    fmt.Println("failed to parse sitemap:", parseErr.URL)
//	}
type ParseError struct {
	// URL is the sitemap URL that was being parsed when the error occurred. For a sitemap
	// reached through a redirect it is the URL the sitemap was served from.
	// When the depth limit is reached, it is the sitemap index whose sitemaps were not
	// followed. When the limit on the sitemaps, on the URLs or on the validation errors of a
	// call is reached, or the call was cut short, it is the URL the call was made for.
	URL string
	// Err is the underlying parse error.
	Err error
}

// Error returns the message of the error, which names the URL and tells why the parse failed
// or was not carried through: `parse "https://example.com/sitemap.xml": sitemap content is empty`.
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
// How many entries of a document are not valid is up to the document, so a call records no
// more errors of this type than the limit set with SetMaxValidationErrors allows.
//
// What a document holds is as long as its author likes as well, and the error quotes it. It
// therefore holds no text longer than 4096 bytes, twice the length a URL of a sitemap may
// have. A URL that is longer is cut to its first 4096 bytes and ends in "…" to tell that it
// was; the URL a call was made for is named whole. An Err whose message is longer is replaced
// by an error with the message cut the same way, which errors.Is and errors.As find nothing
// beyond.
//
// Example usage:
//
//	var valErr *sitemap.ValidationError
//	if errors.As(err, &valErr) {
//	    fmt.Println("invalid URL:", valErr.URL)
//	}
type ValidationError struct {
	// URL is the URL value being validated. When it is not a URL that was rejected, it is the
	// URL the rejected value belongs to: the page for a value of a <url> entry and for the
	// date of an entry of a feed, the sitemap a sitemap index lists for the date the index
	// gives for it, and the sitemap an entry stands in for an entry without a location. For
	// a sitemap reached through a redirect the latter is the URL the sitemap was served from.
	// May be empty for field-level errors where no specific URL is available.
	URL string
	// Err is the underlying validation error.
	Err error
}

// Error returns the message of the error, which names the URL and tells what is not valid:
// `validate "ftp://example.com/page": unsupported scheme "ftp"`.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validate %q: %s", e.URL, e.Err)
}

// Unwrap returns the underlying error, enabling errors.Is / errors.As traversal.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

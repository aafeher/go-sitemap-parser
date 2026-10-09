package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	sitemap "github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how to use typed errors returned by go-sitemap-parser.
//
// All errors stored in GetErrors() and returned by Parse() / ParseContext()
// implement the standard error interface and can be inspected with errors.As:
//
//   - *sitemap.ConfigError    — a configuration setter received an invalid value
//   - *sitemap.NetworkError   — an HTTP fetch failed
//   - *sitemap.ParseError     — a sitemap document could not be parsed, or the
//     parse was not carried through: the depth limit was reached, or the call
//     was cut short by its context
//   - *sitemap.ValidationError — a URL or field value failed validation
//
// Each typed error exposes a URL / Field for context and an Err for the root
// cause, so that errors.Is can still match on well-known sentinel errors.
//
// Parse() / ParseContext() fail when the document they are called for cannot
// be fetched or parsed, and return the error that GetErrors() holds about it.
// What goes wrong with a sitemap that document lists, or with one of its
// entries, is in GetErrors() only.
func main() {
	// ── 1. ConfigError ───────────────────────────────────────────────────────
	fmt.Println("=== ConfigError ===")
	s := sitemap.New().SetMaxDepth(-1) // invalid: must be > 0
	for _, err := range s.GetErrors() {
		var cfgErr *sitemap.ConfigError
		if errors.As(err, &cfgErr) {
			fmt.Printf("  field: %q\n", cfgErr.Field)
			fmt.Printf("  cause: %s\n", cfgErr.Err)
		}
	}

	// ── 2. NetworkError ───────────────────────────────────────────────────────
	fmt.Println("\n=== NetworkError ===")
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()

	s = sitemap.New()
	if _, err := s.Parse(notFound.URL+"/sitemap.xml", nil); err != nil {
		var netErr *sitemap.NetworkError
		if errors.As(err, &netErr) {
			fmt.Printf("  url:   %s\n", netErr.URL)
			fmt.Printf("  cause: %s\n", netErr.Err)
		}
	}
	for _, err := range s.GetErrors() {
		var netErr *sitemap.NetworkError
		if errors.As(err, &netErr) {
			fmt.Printf("  [errs] fetch failed: %s\n", netErr.URL)
		}
	}

	// ── 3. ParseError ─────────────────────────────────────────────────────────
	fmt.Println("\n=== ParseError ===")
	badXML := "\n" // no root XML element → unrecognised format
	s = sitemap.New()
	// The document Parse is called for cannot be parsed, so the call fails.
	if _, err := s.Parse("https://example.com/sitemap.xml", &badXML); err != nil {
		var parseErr *sitemap.ParseError
		if errors.As(err, &parseErr) {
			fmt.Printf("  url:   %s\n", parseErr.URL)
			fmt.Printf("  cause: %s\n", parseErr.Err)
		}
	}
	// The error returned is the one in the error list.
	for _, err := range s.GetErrors() {
		var parseErr *sitemap.ParseError
		if errors.As(err, &parseErr) {
			fmt.Printf("  [errs] parse failed: %s\n", parseErr.URL)
		}
	}

	// A call that is cut short by its context fails with a *ParseError as well. It
	// names the URL the call was made for and wraps the error of the context.
	fmt.Println("\n=== ParseError of a call that was cut short ===")
	index := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-1.xml</loc></sitemap>
  <sitemap><loc>https://example.com/sitemap-2.xml</loc></sitemap>
</sitemapindex>`
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the sitemaps of the index are fetched
	s = sitemap.New()
	if _, err := s.ParseContext(ctx, "https://example.com/sitemap.xml", &index); err != nil {
		var parseErr *sitemap.ParseError
		if errors.As(err, &parseErr) {
			fmt.Printf("  url:       %s\n", parseErr.URL)
			fmt.Printf("  cause:     %s\n", parseErr.Err)
			fmt.Printf("  cancelled: %t\n", errors.Is(err, context.Canceled))
		}
	}
	// It is recorded once, not once for every sitemap that was not fetched.
	fmt.Printf("  [errs] %d error(s) recorded\n", s.GetErrorsCount())

	// ── 4. ValidationError ────────────────────────────────────────────────────
	fmt.Println("\n=== ValidationError ===")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>/relative-page</loc></url>
</urlset>`)
	}))
	defer server.Close()

	s = sitemap.New().SetStrict(true)
	if _, err := s.Parse(server.URL+"/sitemap.xml", nil); err != nil {
		log.Printf("parse error: %v", err)
	}
	for _, e := range s.GetErrors() {
		var valErr *sitemap.ValidationError
		if errors.As(e, &valErr) {
			fmt.Printf("  url:   %q\n", valErr.URL)
			fmt.Printf("  cause: %s\n", valErr.Err)
		}
	}
}

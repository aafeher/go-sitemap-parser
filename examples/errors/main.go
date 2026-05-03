package main

import (
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
//   - *sitemap.ParseError     — XML or gzip parsing of a sitemap document failed
//   - *sitemap.ValidationError — a URL or field value failed validation
//
// Each typed error exposes a URL / Field for context and an Err for the root
// cause, so that errors.Is can still match on well-known sentinel errors.
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
	if _, err := s.Parse("https://example.com/sitemap.xml", &badXML); err == nil {
		for _, e := range s.GetErrors() {
			var parseErr *sitemap.ParseError
			if errors.As(e, &parseErr) {
				fmt.Printf("  url:   %s\n", parseErr.URL)
				fmt.Printf("  cause: %s\n", parseErr.Err)
			}
		}
	}

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

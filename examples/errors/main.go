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
//     parse was not carried through: a limit was reached, or the call was cut
//     short by its context
//   - *sitemap.ValidationError — a URL or field value failed validation
//
// Each typed error exposes a URL / Field for context and an Err for the root
// cause, so that errors.Is can still match on well-known sentinel errors.
//
// The error of a limit that was reached wraps the error that stands for the
// limit: sitemap.ErrMaxResponseSize, sitemap.ErrMaxDepth,
// sitemap.ErrMaxSitemaps, sitemap.ErrMaxURLs or
// sitemap.ErrMaxValidationErrors. errors.Is tells which limit it was, with no
// need to read the message.
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

	// Nothing is parsed while the error is outstanding. The error of the call
	// wraps it, so the setting to correct can be told from that error as well.
	_, err := s.Parse("https://example.com/sitemap.xml", nil)
	fmt.Printf("  Parse: %v\n", err)
	var blockedBy *sitemap.ConfigError
	if errors.As(err, &blockedBy) {
		fmt.Printf("  blocked by: %q\n", blockedBy.Field)
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

	// ── 5. Limits ─────────────────────────────────────────────────────────────
	// A limit that is reached does not fail the call. What it left out is told
	// by an error in GetErrors(), and errors.Is tells which limit that error is
	// about.
	fmt.Println("\n=== Errors of the limits ===")
	pages := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><lastmod>2024-01-15</lastmod></url>
  <url><lastmod>2024-01-16</lastmod></url>
  <url><loc>https://example.com/page-1</loc></url>
  <url><loc>https://example.com/page-2</loc></url>
  <url><loc>https://example.com/page-3</loc></url>
</urlset>`
	// The document has an entry without a location and a page more than the
	// limits allow.
	s = sitemap.New().SetMaxURLs(2).SetMaxValidationErrors(1)
	if _, err := s.Parse("https://example.com/sitemap.xml", &pages); err != nil {
		log.Printf("parse error: %v", err)
	}
	fmt.Printf("  %d URL(s), %d error(s)\n", s.GetURLCount(), s.GetErrorsCount())
	for _, e := range s.GetErrors() {
		if limit := limitOf(e); limit != "" {
			fmt.Printf("  %s: %v\n", limit, e)
		}
	}
}

// limitOf returns the name of the limit err is about, and "" if it is about
// none.
func limitOf(err error) string {
	switch {
	case errors.Is(err, sitemap.ErrMaxResponseSize):
		return "ErrMaxResponseSize"
	case errors.Is(err, sitemap.ErrMaxDepth):
		return "ErrMaxDepth"
	case errors.Is(err, sitemap.ErrMaxSitemaps):
		return "ErrMaxSitemaps"
	case errors.Is(err, sitemap.ErrMaxURLs):
		return "ErrMaxURLs"
	case errors.Is(err, sitemap.ErrMaxValidationErrors):
		return "ErrMaxValidationErrors"
	}
	return ""
}

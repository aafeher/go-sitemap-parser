package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how the two modes treat a sitemap with mistakes in it.
//
// A value that cannot be parsed costs only itself, never the whole sitemap.
// Tolerant mode (the default) leaves such a <lastmod> or <priority> unset and
// keeps the entry; strict mode skips the entry. Either way the value is
// reported via GetErrors() as a *ValidationError for the page it belongs to.
//
// An entry without a location names no page. It is skipped and reported in
// both modes; the error names the sitemap, the entry having no URL of its own.
//
// Tolerant mode also reads past the XML mistakes that can be read past, such
// as an unescaped "&" in a URL. Strict mode requires well-formed XML and
// rejects such a document as a whole. Parse() fails then, as the document it
// was called for could not be parsed: it returns the *ParseError that
// GetErrors() holds about the document.
//
// The sitemap content is passed in directly, so the example runs without
// network access.
func main() {
	// Two of the four entries hold a value that cannot be parsed: a <lastmod>
	// without the "T" separator and a time zone, and a <priority> written with
	// a decimal comma.
	invalidValues := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
    <lastmod>2024-01-15T10:30:00+01:00</lastmod>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://example.com/about</loc>
    <lastmod>2024-01-15 10:30:00</lastmod>
  </url>
  <url>
    <loc>https://example.com/contact</loc>
    <priority>0,5</priority>
  </url>
  <url>
    <loc>https://example.com/blog</loc>
    <lastmod>2024-01-15</lastmod>
    <priority>0.8</priority>
  </url>
</urlset>`

	// The second entry has no <loc> at all, the third one an empty one.
	missingLocation := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><lastmod>2024-01-15</lastmod></url>
  <url><loc></loc></url>
</urlset>`

	// The "&" of the query string is not escaped as "&amp;", which makes the
	// document malformed XML.
	malformedXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/search?q=go&page=2</loc></url>
  <url><loc>https://example.com/blog</loc></url>
</urlset>`

	fmt.Println("=== Invalid values, tolerant mode ===")
	parse(sitemap.New(), invalidValues)

	fmt.Println("\n=== Invalid values, strict mode ===")
	parse(sitemap.New().SetStrict(true), invalidValues)

	fmt.Println("\n=== Missing location, tolerant mode ===")
	parse(sitemap.New(), missingLocation)

	fmt.Println("\n=== Missing location, strict mode ===")
	parse(sitemap.New().SetStrict(true), missingLocation)

	fmt.Println("\n=== Malformed XML, tolerant mode ===")
	parse(sitemap.New(), malformedXML)

	fmt.Println("\n=== Malformed XML, strict mode ===")
	parse(sitemap.New().SetStrict(true), malformedXML)
}

// parse parses content with s and prints the URLs and the errors it yields.
func parse(s *sitemap.S, content string) {
	sm, err := s.Parse("https://example.com/sitemap.xml", &content)
	// A document that cannot be parsed fails the call. That error is in
	// GetErrors() as well, and is printed with the others below.
	var parseErr *sitemap.ParseError
	if err != nil && !errors.As(err, &parseErr) {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", u.Loc)
		// LastMod and Priority are nil when the element is absent, and also
		// when its value could not be parsed.
		if u.LastMod != nil {
			fmt.Printf("    LastMod: %s\n", u.LastMod.Format("2006-01-02T15:04:05Z07:00"))
		}
		if u.Priority != nil {
			fmt.Printf("    Priority: %.1f\n", *u.Priority)
		}
	}
	for _, e := range sm.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

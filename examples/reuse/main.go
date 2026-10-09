package main

import (
	"fmt"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how a single parser instance is reused for several Parse
// calls.
//
// Every Parse / ParseContext call starts from a clean state: the URLs and
// errors collected by the previous call are discarded first, so GetURLs() and
// GetErrors() always describe the most recent call only.
//
// Configuration errors are the exception. A *ConfigError recorded by a Set*
// method belongs to the instance rather than to a single call: it stays in
// GetErrors() and blocks parsing until the same setter is called again with a
// valid value.
//
// The sitemap content is passed in directly, so the example runs without
// network access.
func main() {
	withInvalidEntry := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/page-1</loc></url>
  <url><loc>ftp://example.com/page-2</loc></url>
</urlset>`

	valid := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.org/page-1</loc></url>
  <url><loc>https://example.org/page-2</loc></url>
  <url><loc>https://example.org/page-3</loc></url>
</urlset>`

	s := sitemap.New()

	// The first document contains an entry with an unsupported scheme. The
	// entry is skipped and reported; the call itself succeeds.
	fmt.Println("=== First call ===")
	report(s, "https://example.com/sitemap.xml", withInvalidEntry)

	// The same instance parses the next document. Neither the URL nor the
	// error of the first call is carried over.
	fmt.Println("\n=== Second call on the same instance ===")
	report(s, "https://example.org/sitemap.xml", valid)

	// An invalid setting is recorded as a *ConfigError. It blocks parsing, and
	// the results of the previous call are discarded all the same.
	fmt.Println("\n=== Invalid setting ===")
	s.SetMaxDepth(0)
	report(s, "https://example.org/sitemap.xml", valid)

	// Calling the same setter with a valid value clears the error, and the
	// instance parses again.
	fmt.Println("\n=== Setting corrected ===")
	s.SetMaxDepth(5)
	report(s, "https://example.org/sitemap.xml", valid)
}

// report parses content with s and prints what the call collected.
func report(s *sitemap.S, url, content string) {
	if _, err := s.Parse(url, &content); err != nil {
		fmt.Printf("  parse error: %v\n", err)
	}

	fmt.Printf("  %d URLs, %d errors\n", s.GetURLCount(), s.GetErrorsCount())
	for _, e := range s.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

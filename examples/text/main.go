package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a plain text sitemap.
//
// A text sitemap lists one URL on a line. A line is an entry when it begins
// with "http://" or "https://"; whitespace around it is ignored. Empty lines,
// lines that begin with "#" and every other line are skipped. Nothing but the
// location can be given in a text sitemap, so LastMod, ChangeFreq and Priority
// are nil.
//
// The sitemap is passed in directly, so the example runs without network
// access. To have it fetched, pass nil in place of the content.
func main() {
	// The third URL is indented, and the last line is no URL.
	content := `# Pages of example.com
https://example.com/
https://example.com/about

  https://example.com/contact
not a URL
`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/sitemap.txt", &content)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d URLs from the text sitemap\n", sm.GetURLCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

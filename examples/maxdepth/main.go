package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how to limit the sitemap index recursion depth via
// SetMaxDepth. A sitemap index may reference other sitemap indexes, which
// may in turn reference further indexes. SetMaxDepth caps how many levels
// deep the parser will follow before stopping. The default is 10.
//
// When the depth limit is reached, a *ParseError is recorded in GetErrors()
// and the parser stops following that branch. URLs already collected up to
// that depth remain available via GetURLs().
func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// Limit recursion to a single level: the parser will parse the root
	// sitemap but will not follow any sitemap index entries it finds there.
	s := sitemap.New().SetMaxDepth(1)

	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("parse error: %v", err)
	}

	// Report any depth-limit or other errors encountered.
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, e := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, e)
		}
	}

	fmt.Printf("Sitemap %s contains %d URLs (parsed with maxDepth=%d).\n",
		url, sm.GetURLCount(), s.GetMaxDepth())
}

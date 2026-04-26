package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// create new instance with strict mode enabled
	// In strict mode, all <loc> URLs must be absolute HTTP(S), on the same host
	// and protocol as the sitemap file, and no longer than 2,048 characters.
	s := sitemap.New().SetStrict(true).SetFetchTimeout(5).SetMultiThread(false)
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	// Print the errors (in strict mode, non-compliant URLs are reported here)
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, err := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, err)
		}
	}

	// GetURLCount()
	count := sm.GetURLCount()
	fmt.Printf("Sitemaps of %s contains %d valid URLs.\n\n", url, count)

	// GetURLs()
	for i, u := range sm.GetURLs() {
		fmt.Printf("%d. url -> Loc: %s\n", i, u.Loc)
	}
}

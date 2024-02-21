package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

// main is the entry point of the program.
// It retrieves a sitemap URL, parses it using the sitemap package,
// and prints the URLs along with additional information.
func main() {
	url := "https://www.sitemaps.org/robots.txt"

	s := sitemap.New()
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	urls := sm.GetURLs()

	for i, u := range urls {
		fmt.Printf("%d. url -> Loc: %s", i, u.Loc)
		if u.ChangeFreq != nil {
			fmt.Printf(", ChangeFreq: %v", u.ChangeFreq)
		}
		if u.Priority != nil {
			fmt.Printf(", Priority: %.1f", *u.Priority)
		}
		if u.LastMod != nil {
			fmt.Printf(", LastMod: %s", u.LastMod.String())
		}
		fmt.Println()
	}
}

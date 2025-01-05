package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// create new instance, overwrite default configuration and call Parse() with url
	s := sitemap.New().SetUserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:123.0) Gecko/20100101 Firefox/123.0").SetFetchTimeout(5).SetMultiThread(false)
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	// Print the errors
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, err := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, err)
		}
	}

	// GetURLCount()
	count := sm.GetURLCount()

	fmt.Printf("Sitemaps of %s contains %d URLs.\n\n", url, count)

	// GetURLs()
	urlsAll := sm.GetURLs()

	for i, u := range urlsAll {
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
	fmt.Println()

	// GetRandomURLs(n int)
	urlsRandom := sm.GetRandomURLs(7)

	for i, u := range urlsRandom {
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

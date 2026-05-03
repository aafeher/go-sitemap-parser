package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
)

func main() {
	s := sitemap.New()

	// Parse an RSS 2.0 feed as a sitemap
	s, err := s.Parse("https://raw.githubusercontent.com/aafeher/go-sitemap-parser/main/test/rss.xml", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Parsed %d URLs from RSS feed\n", s.GetURLCount())
	for _, u := range s.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

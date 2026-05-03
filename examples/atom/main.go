package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
)

func main() {
	s := sitemap.New()

	// Parse an Atom 1.0 feed as a sitemap
	s, err := s.Parse("https://raw.githubusercontent.com/aafeher/go-sitemap-parser/main/test/atom.xml", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Parsed %d URLs from Atom feed\n", s.GetURLCount())
	for _, u := range s.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

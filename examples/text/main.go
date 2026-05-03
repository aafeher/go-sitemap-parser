package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
)

func main() {
	s := sitemap.New()

	// Parse a plain text sitemap
	s, err := s.Parse("https://raw.githubusercontent.com/aafeher/go-sitemap-parser/main/test/sitemap.txt", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Parsed %d URLs from text sitemap\n", s.GetURLCount())
	for _, u := range s.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

// main is the entry point of the program.
// It fetches and parses a sitemap from a given URL, and prints the number of URLs in the sitemap.
func main() {
	url := "https://www.sitemaps.org/robots.txt"

	s := sitemap.New()
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	count := sm.GetURLCount()

	fmt.Printf("Sitemaps of %s contains %d URLs.", url, count)
}

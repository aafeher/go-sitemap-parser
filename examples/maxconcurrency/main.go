package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how to bound the number of sitemaps the parser fetches
// and parses at the same time via SetMaxConcurrency. This is recommended for very large
// sitemap indexes to avoid goroutine and connection blow-up, and pairs
// well with ParseContext for deadline propagation.
func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s := sitemap.New().
		SetUserAgent("go-sitemap-parser-example").
		SetMaxConcurrency(4) // at most 4 sitemaps are fetched or parsed at a time

	sm, err := s.ParseContext(ctx, url, nil)
	if err != nil {
		log.Printf("parse error: %v", err)
	}

	fmt.Printf("Sitemap %s contains %d URLs (parsed with maxConcurrency=4).\n", url, sm.GetURLCount())
}

package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// create new instance with a 10 MB response size limit
	s := sitemap.New().SetMaxResponseSize(10 * 1024 * 1024)
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	// Print the errors (including any size limit violations)
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, err := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, err)
		}
	}

	count := sm.GetURLCount()

	fmt.Printf("Sitemaps of %s contains %d URLs.\n", url, count)
}

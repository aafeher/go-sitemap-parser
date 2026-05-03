package main

import (
	"fmt"

	"github.com/aafeher/go-sitemap-parser"
)

func main() {
	// Sample XML content with hreflang (xhtml:link)
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>http://www.example.com/english/page.html</loc>
    <xhtml:link
               rel="alternate"
               hreflang="de"
               href="http://www.example.com/deutsch/page.html"/>
    <xhtml:link
               rel="alternate"
               hreflang="de-ch"
               href="http://www.example.com/schweiz-deutsch/page.html"/>
    <xhtml:link
               rel="alternate"
               hreflang="en"
               href="http://www.example.com/english/page.html"/>
  </url>
</urlset>`

	s := sitemap.New()
	_, err := s.Parse("http://www.example.com/sitemap.xml", &xmlContent)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	for _, url := range s.GetURLs() {
		fmt.Printf("URL: %s\n", url.Loc)
		if len(url.Hreflangs) > 0 {
			fmt.Println("  Alternate versions (hreflang):")
			for _, h := range url.Hreflangs {
				fmt.Printf("    - [%s] %s (rel: %s)\n", h.Hreflang, h.Href, h.Rel)
			}
		}
	}
}

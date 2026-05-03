package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a sitemap that uses the Google Image Sitemap extension.
//
// When a <url> entry contains <image:image> elements, the parser populates the
// Images field on each URL struct. Each Image exposes the Loc, Title, Caption,
// GeoLocation, and License fields defined by the extension.
//
// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/image-sitemaps
func main() {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url>
    <loc>https://example.com/page</loc>
    <image:image>
      <image:loc>https://example.com/photo1.jpg</image:loc>
      <image:title>Mountain landscape</image:title>
      <image:caption>A view from the summit</image:caption>
      <image:geo_location>Alps, Switzerland</image:geo_location>
      <image:license>https://creativecommons.org/licenses/by/4.0/</image:license>
    </image:image>
    <image:image>
      <image:loc>https://cdn.example.com/photo2.jpg</image:loc>
      <image:title>Valley view</image:title>
    </image:image>
  </url>
  <url>
    <loc>https://example.com/other-page</loc>
  </url>
</urlset>`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/sitemap.xml", &xmlContent)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		fmt.Printf("Page: %s\n", u.Loc)
		if len(u.Images) == 0 {
			fmt.Println("  (no images)")
			continue
		}
		for _, img := range u.Images {
			fmt.Printf("  Image: %s\n", img.Loc)
			if img.Title != "" {
				fmt.Printf("    Title:       %s\n", img.Title)
			}
			if img.Caption != "" {
				fmt.Printf("    Caption:     %s\n", img.Caption)
			}
			if img.GeoLocation != "" {
				fmt.Printf("    GeoLocation: %s\n", img.GeoLocation)
			}
			if img.License != "" {
				fmt.Printf("    License:     %s\n", img.License)
			}
		}
	}
}

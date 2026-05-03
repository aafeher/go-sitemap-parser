package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a sitemap that uses the Google News Sitemap extension.
//
// When a <url> entry contains a <news:news> element, the parser populates the
// News field on the URL struct. The News struct exposes Publication (Name and
// Language), PublicationDate, and Title as defined by the extension.
//
// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/news-sitemap
func main() {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
  <url>
    <loc>https://example.com/article-1</loc>
    <news:news>
      <news:publication>
        <news:name>Example News</news:name>
        <news:language>en</news:language>
      </news:publication>
      <news:publication_date>2026-05-03T10:00:00Z</news:publication_date>
      <news:title>Breaking: Example Article</news:title>
    </news:news>
  </url>
  <url>
    <loc>https://example.com/regular-page</loc>
  </url>
</urlset>`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/news-sitemap.xml", &xmlContent)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		fmt.Printf("Page: %s\n", u.Loc)
		if u.News == nil {
			fmt.Println("  (no news metadata)")
			continue
		}
		fmt.Printf("  Title:       %s\n", u.News.Title)
		fmt.Printf("  Publication: %s (%s)\n", u.News.Publication.Name, u.News.Publication.Language)
		if u.News.PublicationDate != nil {
			fmt.Printf("  Date:        %s\n", u.News.PublicationDate.Format("2006-01-02T15:04:05Z07:00"))
		}
	}
}

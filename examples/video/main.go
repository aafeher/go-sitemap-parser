package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a sitemap that uses the Google Video Sitemap extension.
//
// When a <url> entry contains <video:video> elements, the parser populates the
// Videos field on each URL struct. Each Video exposes ThumbnailLoc, Title,
// Description, ContentLoc, PlayerLoc, Duration, Rating, ViewCount,
// PublicationDate, ExpirationDate, FamilyFriendly, Restriction, Platform,
// RequiresSubscription, Uploader, Live, and Tags.
//
// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/video-sitemaps
func main() {
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
  <url>
    <loc>https://example.com/video-page</loc>
    <video:video>
      <video:thumbnail_loc>https://example.com/thumb.jpg</video:thumbnail_loc>
      <video:title>Example Video</video:title>
      <video:description>A sample video description</video:description>
      <video:content_loc>https://example.com/video.mp4</video:content_loc>
      <video:player_loc>https://example.com/player</video:player_loc>
      <video:duration>600</video:duration>
      <video:rating>4.5</video:rating>
      <video:view_count>12345</video:view_count>
      <video:publication_date>2026-05-03T10:00:00Z</video:publication_date>
      <video:family_friendly>yes</video:family_friendly>
      <video:restriction relationship="allow">HU AT DE</video:restriction>
      <video:platform relationship="allow">web mobile</video:platform>
      <video:requires_subscription>no</video:requires_subscription>
      <video:uploader info="https://example.com/channel">ExampleChannel</video:uploader>
      <video:live>no</video:live>
      <video:tag>golang</video:tag>
      <video:tag>sitemap</video:tag>
    </video:video>
  </url>
  <url>
    <loc>https://example.com/regular-page</loc>
  </url>
</urlset>`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/video-sitemap.xml", &xmlContent)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		fmt.Printf("Page: %s\n", u.Loc)
		if len(u.Videos) == 0 {
			fmt.Println("  (no videos)")
			continue
		}
		for _, v := range u.Videos {
			fmt.Printf("  Video: %s\n", v.Title)
			fmt.Printf("    Thumbnail: %s\n", v.ThumbnailLoc)
			if v.ContentLoc != "" {
				fmt.Printf("    Content:   %s\n", v.ContentLoc)
			}
			if v.Duration != nil {
				fmt.Printf("    Duration:  %ds\n", *v.Duration)
			}
			if v.Rating != nil {
				fmt.Printf("    Rating:    %.1f/5.0\n", *v.Rating)
			}
			if v.ViewCount != nil {
				fmt.Printf("    Views:     %d\n", *v.ViewCount)
			}
			if v.Restriction != nil {
				fmt.Printf("    Restrict:  [%s] %s\n", v.Restriction.Relationship, v.Restriction.Value)
			}
			if len(v.Tags) > 0 {
				fmt.Printf("    Tags:      %v\n", v.Tags)
			}
		}
	}
}

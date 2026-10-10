package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing an Atom 1.0 feed as a sitemap.
//
// The pages of a feed are what its entries link to. An <entry> may have
// several <link> elements; the page is the first one that is an alternate
// representation of the entry: a link with rel="alternate", or without a rel
// attribute, which means the same. Links of another kind, such as rel="self"
// or rel="enclosure", are not pages. The links of the feed itself are not
// taken either, and an entry without a page link is skipped without an error.
// Nothing but the location is taken from a feed, so LastMod, ChangeFreq and
// Priority are nil.
//
// The feed is passed in directly, so the example runs without network access.
// To have it fetched, pass nil in place of the content.
func main() {
	// The first entry has a link without a rel attribute, the second one an
	// enclosure in front of its page, and the last one no page at all.
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example blog</title>
  <link href="https://example.com/blog"/>
  <link rel="self" href="https://example.com/feed.xml"/>
  <updated>2024-01-16T08:00:00Z</updated>
  <entry>
    <title>First post</title>
    <link href="https://example.com/blog/first-post"/>
    <updated>2024-01-15T10:30:00Z</updated>
  </entry>
  <entry>
    <title>Second post</title>
    <link rel="enclosure" href="https://example.com/media/second-post.mp3"/>
    <link rel="alternate" href="https://example.com/blog/second-post"/>
    <updated>2024-01-16T08:00:00Z</updated>
  </entry>
  <entry>
    <title>A recording without a page of its own</title>
    <link rel="enclosure" href="https://example.com/media/recording.mp3"/>
  </entry>
</feed>`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/feed.xml", &feed)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d URLs from the Atom feed\n", sm.GetURLCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

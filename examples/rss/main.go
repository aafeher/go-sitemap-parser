package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing an RSS 2.0 feed as a sitemap.
//
// The pages of a feed are what its items link to: the <link> of every <item>
// becomes a URL. The <link> of the channel is not one of them, and an item
// without a <link> is skipped without an error, since an item is not required
// to have one. Nothing but the location is taken from a feed, so LastMod,
// ChangeFreq and Priority are nil.
//
// The feed is passed in directly, so the example runs without network access.
// To have it fetched, pass nil in place of the content.
func main() {
	// The last item has no <link>.
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example blog</title>
    <link>https://example.com/blog</link>
    <description>News from example.com</description>
    <item>
      <title>First post</title>
      <link>https://example.com/blog/first-post</link>
      <pubDate>Mon, 15 Jan 2024 10:30:00 GMT</pubDate>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.com/blog/second-post</link>
      <pubDate>Tue, 16 Jan 2024 08:00:00 GMT</pubDate>
    </item>
    <item>
      <title>A note without a page of its own</title>
    </item>
  </channel>
</rss>`

	s := sitemap.New()
	sm, err := s.Parse("https://example.com/feed.xml", &feed)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d URLs from the RSS feed\n", sm.GetURLCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
}

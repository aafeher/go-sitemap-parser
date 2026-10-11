package main

import (
	"fmt"
	"log"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing an RSS 2.0 feed as a sitemap.
//
// The pages of a feed are what its items link to: the <link> of every <item>
// becomes a URL. The <link> of the channel is not one of them, and an item
// without a <link> is skipped without an error, since an item is not required
// to have one.
//
// The <pubDate> of an item is the date its page was last modified, LastMod.
// It is read in the format RSS prescribes, the date and time of RFC 822, with
// the time zone given as an offset or by one of the names that RFC knows. An
// item without a date has none, and neither has one whose date is in another
// format: that date is reported in GetErrors(), and the page is returned all
// the same. The dates of the channel are not read. Nothing but the location
// and the date is taken from a feed, so ChangeFreq and Priority are nil.
//
// The feed is passed in directly, so the example runs without network access.
// To have it fetched, pass nil in place of the content.
func main() {
	// The second item gives its time zone by name, the third one has a date
	// that is not in the format of RSS, the fourth one has no date, and the
	// last one has no <link>.
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example blog</title>
    <link>https://example.com/blog</link>
    <description>News from example.com</description>
    <pubDate>Wed, 17 Jan 2024 09:15:00 GMT</pubDate>
    <item>
      <title>First post</title>
      <link>https://example.com/blog/first-post</link>
      <pubDate>Mon, 15 Jan 2024 10:30:00 +0100</pubDate>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.com/blog/second-post</link>
      <pubDate>Tue, 16 Jan 2024 08:00:00 EST</pubDate>
    </item>
    <item>
      <title>Third post</title>
      <link>https://example.com/blog/third-post</link>
      <pubDate>2024-01-17T09:15:00Z</pubDate>
    </item>
    <item>
      <title>About</title>
      <link>https://example.com/about</link>
    </item>
    <item>
      <title>A note without a page of its own</title>
      <pubDate>Wed, 17 Jan 2024 09:15:00 GMT</pubDate>
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
		lastMod := "no date"
		if u.LastMod != nil {
			lastMod = "last modified " + u.LastMod.UTC().Format(time.RFC3339)
		}
		fmt.Printf(" - %s (%s)\n", u.Loc, lastMod)
	}
	for _, e := range sm.GetErrors() {
		fmt.Printf("error: %v\n", e)
	}
}

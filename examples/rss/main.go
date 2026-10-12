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
// the same. An item without a <pubDate> takes its date from its <dc:date>,
// the date of Dublin Core, which is in the format of a <lastmod>. The dates of
// the channel are not read. Nothing but the location and the date is taken
// from a feed, so ChangeFreq and Priority are nil.
//
// A feed may hold elements of other formats, which are told from its own by
// their namespace: the <atom:link> of the channel and the one of the first
// item are elements of Atom. Such an element is not read, so the <atom:link>
// of the item is not taken for its <link>, wherever it stands in the item.
// The <dc:date> is the one element of another format that is.
//
// The feed is passed in directly, so the example runs without network access.
// To have it fetched, pass nil in place of the content.
func main() {
	// The first item has an <atom:link> after its <link>, the second one gives
	// its time zone by name, the third one has a date that is not in the format
	// of RSS, the fourth one has a <dc:date> in place of a <pubDate>, the fifth
	// one has no date, and the last one has no <link>.
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Example blog</title>
    <link>https://example.com/blog</link>
    <description>News from example.com</description>
    <atom:link rel="self" type="application/rss+xml" href="https://example.com/feed.xml"/>
    <pubDate>Wed, 17 Jan 2024 09:15:00 GMT</pubDate>
    <item>
      <title>First post</title>
      <link>https://example.com/blog/first-post</link>
      <atom:link rel="related" href="https://example.com/blog/first-post/comments.atom"/>
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
      <title>Fourth post</title>
      <link>https://example.com/blog/fourth-post</link>
      <dc:date>2024-01-18T07:45:00Z</dc:date>
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

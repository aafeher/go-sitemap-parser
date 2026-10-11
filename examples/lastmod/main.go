package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates the date a sitemap index gives for each of its sitemaps,
// and how to fetch only the sitemaps that were modified since an earlier
// visit.
//
// Sitemap.LastMod is the <lastmod> of the entry a sitemap has in a sitemap
// index. It is nil if the entry gives no date, and if what it gives cannot be
// parsed: that date is reported in GetErrors(), and the sitemap is listed and
// fetched all the same.
//
// The first call reads the sitemap index without fetching what it lists: no
// URL matches its follow pattern. The sitemaps that were modified since the
// earlier visit are then parsed one by one, and with them the ones no date is
// known of, which may have been. Of their pages the ones modified since then
// are printed; URL.LastMod is the <lastmod> of a page.
//
// The sitemap index and the sitemaps are served by a local test server, so the
// example runs without network access.
func main() {
	server := httptest.NewServer(http.HandlerFunc(serve))
	defer server.Close()

	lastVisit := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	index := sitemap.New().SetFollow([]string{`^$`})
	if _, err := index.Parse(server.URL+"/sitemap-index.xml", nil); err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Println("=== sitemap index ===")
	var modified []sitemap.Sitemap
	for _, listed := range index.GetSitemaps() {
		switch {
		case listed.LastMod == nil:
			fmt.Printf("%s: no date\n", strings.TrimPrefix(listed.Loc, server.URL))
			modified = append(modified, listed)
		case listed.LastMod.After(lastVisit):
			fmt.Printf("%s: last modified %s\n", strings.TrimPrefix(listed.Loc, server.URL), listed.LastMod.UTC().Format(time.RFC3339))
			modified = append(modified, listed)
		default:
			fmt.Printf("%s: last modified %s, not fetched\n", strings.TrimPrefix(listed.Loc, server.URL), listed.LastMod.UTC().Format(time.RFC3339))
		}
	}
	for _, e := range index.GetErrors() {
		fmt.Printf("error: %v\n", strings.ReplaceAll(e.Error(), server.URL, ""))
	}

	fmt.Printf("\n=== pages modified since %s ===\n", lastVisit.Format(time.DateOnly))
	s := sitemap.New()
	for _, listed := range modified {
		if _, err := s.Parse(listed.Loc, nil); err != nil {
			log.Fatalf("parse error: %v", err)
		}
		for _, u := range s.GetURLs() {
			if u.LastMod != nil && u.LastMod.After(lastVisit) {
				fmt.Printf("%s (%s)\n", strings.TrimPrefix(u.Loc, server.URL), u.LastMod.UTC().Format(time.RFC3339))
			}
		}
	}
}

// serve answers the requests of the example: with a sitemap index of four
// sitemaps, and with a sitemap of two pages for every other path.
func serve(w http.ResponseWriter, r *http.Request) {
	site := "http://" + r.Host
	w.Header().Set("Content-Type", "application/xml")

	if r.URL.Path == "/sitemap-index.xml" {
		// The third entry has a date that is in no format of the protocol, and
		// the last one has no date.
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap>
    <loc>%[1]s/sitemap-archive.xml</loc>
    <lastmod>2023-11-02</lastmod>
  </sitemap>
  <sitemap>
    <loc>%[1]s/sitemap-blog.xml</loc>
    <lastmod>2024-03-10T08:15:00+01:00</lastmod>
  </sitemap>
  <sitemap>
    <loc>%[1]s/sitemap-products.xml</loc>
    <lastmod>10/03/2024</lastmod>
  </sitemap>
  <sitemap>
    <loc>%[1]s/sitemap-pages.xml</loc>
  </sitemap>
</sitemapindex>`, site)
		return
	}

	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/sitemap-"), ".xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>%[1]s/%[2]s/old</loc>
    <lastmod>2023-11-02</lastmod>
  </url>
  <url>
    <loc>%[1]s/%[2]s/new</loc>
    <lastmod>2024-03-10T08:15:00+01:00</lastmod>
  </url>
</urlset>`, site, name)
}

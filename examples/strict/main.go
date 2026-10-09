package main

import (
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// create new instance with strict mode enabled
	// In strict mode, all <loc> URLs must be absolute HTTP(S), on the same host
	// and protocol as the sitemap file, and no longer than 2,048 characters.
	// See rules() below for what that means for an entry.
	s := sitemap.New().SetStrict(true).SetFetchTimeout(5).SetMultiThread(false)
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	// Print the errors (in strict mode, non-compliant URLs are reported here)
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, err := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, err)
		}
	}

	// GetURLCount()
	count := sm.GetURLCount()
	fmt.Printf("Sitemaps of %s contains %d valid URLs.\n\n", url, count)

	// GetURLs()
	for i, u := range sm.GetURLs() {
		fmt.Printf("%d. url -> Loc: %s\n", i, u.Loc)
	}

	fmt.Println()
	rules()
}

// rules shows what strict mode requires of the entries of a sitemap, on a
// sitemap that is passed in directly, so this part runs without network access.
//
// A URL has to be on the host and protocol of the sitemap. The name of a host
// is not case-sensitive, and a URL that names no port is on the default port
// of its protocol, so neither capital letters nor ":443" make for another
// host. A subdomain and another port do.
//
// A URL must hold no space, which it has to give as "%20", and the
// <changefreq> and <priority> of an entry must hold a value of the protocol.
//
// An entry that does not meet a requirement is skipped and reported.
func rules() {
	url := "https://example.com/sitemap.xml"
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><changefreq>weekly</changefreq><priority>1.0</priority></url>
  <url><loc>https://EXAMPLE.com/capitals</loc></url>
  <url><loc>https://example.com:443/default-port</loc></url>
  <url><loc>https://example.com/search?q=summer%20sale</loc></url>
  <url><loc>https://example.com:8443/other-port</loc></url>
  <url><loc>https://www.example.com/subdomain</loc></url>
  <url><loc>http://example.com/other-protocol</loc></url>
  <url><loc>/relative</loc></url>
  <url><loc>https://example.com/search?q=summer sale</loc></url>
  <url><loc>https://example.com/change-frequency</loc><changefreq>Weekly</changefreq></url>
  <url><loc>https://example.com/priority</loc><priority>1.5</priority></url>
</urlset>`

	s := sitemap.New().SetStrict(true)
	sm, err := s.Parse(url, &content)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Strict mode accepts %d of the 11 entries of %s:\n", sm.GetURLCount(), url)
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", u.Loc)
	}
	fmt.Printf("It rejects the other %d:\n", sm.GetErrorsCount())
	for _, e := range sm.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how a sitemap is treated that is reached through a
// redirect.
//
// Such a sitemap is located where it was finally served from, not at the URL
// that was requested. Relative URLs in it are resolved against that URL, and
// in strict mode the URLs it lists have to be on the host and protocol of
// that URL.
//
// The example runs two local test servers, so it needs no network access.
// They listen on different ports, which makes them two hosts. The old site
// redirects every request to the new one, the way http://example.com
// redirects to https://www.example.com.
func main() {
	var newSite *httptest.Server
	oldSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, newSite.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer oldSite.Close()

	// The sitemap of the new site lists a page of the new site, a page with a
	// relative URL and a page of the old site.
	newSite = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/about</loc></url>
  <url><loc>/contact</loc></url>
  <url><loc>%s/legacy</loc></url>
</urlset>`, newSite.URL, oldSite.URL)
	}))
	defer newSite.Close()

	fmt.Printf("old site: %s (redirects to the new site)\n", oldSite.URL)
	fmt.Printf("new site: %s\n", newSite.URL)

	// The sitemap is requested at the old site in both cases.
	url := oldSite.URL + "/sitemap.xml"

	// The relative URL is resolved against the new site, where the sitemap was
	// served from.
	fmt.Println("\n=== Tolerant mode ===")
	parse(sitemap.New(), url)

	// Only the page of the new site is on the host of the sitemap file. The
	// relative URL is rejected for not being absolute, the page of the old
	// site for being on another host.
	fmt.Println("\n=== Strict mode ===")
	parse(sitemap.New().SetStrict(true), url)
}

// parse parses the sitemap at url with s and prints the URLs and the errors
// it yields.
func parse(s *sitemap.S, url string) {
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", u.Loc)
	}
	for _, e := range sm.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

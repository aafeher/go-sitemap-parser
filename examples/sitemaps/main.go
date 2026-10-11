package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates GetSitemaps and GetSitemapCount, which return the sitemaps
// the documents of a Parse call list: the Sitemap lines of a robots.txt and the
// entries of the sitemap indexes the call reads.
//
// A sitemap is among them whether or not it is fetched, so the sitemaps that
// SetFollow leaves out are returned too. Each sitemap is returned once,
// however many documents list it, and by the URL it is requested by. The
// document the call is made for is not among them.
//
// No more sitemaps are returned than SetMaxSitemaps allows. If the documents
// list more, the first ones the call finds are returned and a *ParseError is
// recorded in GetErrors().
//
// The robots.txt and the sitemaps are served by a local test server, so the
// example runs without network access. The server keeps a list of the
// sitemaps it is asked for.
func main() {
	fmt.Println("=== robots.txt ===")
	parse("/robots.txt", sitemap.New())

	// The sitemaps that are not followed are listed like the others. What a
	// sitemap index lists is known only if the index is fetched.
	fmt.Println("\n=== robots.txt, only the sitemap of the blog is followed ===")
	parse("/robots.txt", sitemap.New().SetFollow([]string{`/sitemap-blog\.xml$`}))

	// No URL matches the pattern, so nothing but the sitemap index is fetched:
	// this is how to find out which sitemaps an index lists without fetching
	// them.
	fmt.Println("\n=== sitemap index, no sitemap is followed ===")
	parse("/sitemap-index.xml", sitemap.New().SetFollow([]string{`^$`}))

	// The limit is on the sitemaps a call lists as it is on the ones it
	// fetches, and the two are counted apart. The call lists the first two
	// sitemaps it finds, which are the ones the robots.txt names, and fetches
	// the first two it comes to, which are the sitemap index and the first
	// sitemap the index lists.
	fmt.Println("\n=== robots.txt, 2 sitemaps at most ===")
	parse("/robots.txt", sitemap.New().SetMaxSitemaps(2))
}

// parse parses the document at path on a local test server with s. It prints
// the number of sitemaps the call lists, the number of sitemaps the server was
// asked for and the number of URLs found, then the sitemaps listed, each with
// whether the server was asked for it, and the errors.
func parse(path string, s *sitemap.S) {
	var (
		mu      sync.Mutex
		fetched = map[string]bool{}
	)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			mu.Lock()
			fetched[server.URL+r.URL.Path] = true
			mu.Unlock()
		}

		switch r.URL.Path {
		case "/robots.txt":
			// The sitemap of the blog is named twice.
			fmt.Fprintf(w, "Sitemap: %[1]s/sitemap-index.xml\nSitemap: %[1]s/sitemap-blog.xml\nSitemap: %[1]s/sitemap-blog.xml\n", server.URL)
		case "/sitemap-index.xml":
			// The second entry is relative, which tolerant mode resolves against
			// the URL of the sitemap index.
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>%[1]s/sitemap-products-1.xml</loc></sitemap>
  <sitemap><loc>/sitemap-products-2.xml</loc></sitemap>
  <sitemap><loc>%[1]s/sitemap-blog.xml</loc></sitemap>
</sitemapindex>`, server.URL)
		default:
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s%s/page</loc></url>
</urlset>`, server.URL, r.URL.Path)
		}
	}))
	defer server.Close()

	// The sitemaps are fetched one at a time, so that they are found in the
	// same order every time.
	sm, err := s.SetMultiThread(false).Parse(server.URL+path, nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("%d sitemaps listed, %d sitemaps fetched, %d URLs found\n", sm.GetSitemapCount(), len(fetched), sm.GetURLCount())
	for _, listed := range sm.GetSitemaps() {
		note := "not fetched"
		if fetched[listed.Loc] {
			note = "fetched"
		}
		fmt.Printf("  %s (%s)\n", strings.TrimPrefix(listed.Loc, server.URL), note)
	}
	for _, e := range sm.GetErrors() {
		fmt.Printf("error: %v\n", strings.ReplaceAll(e.Error(), server.URL, ""))
	}
}

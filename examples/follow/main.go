package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how SetFollow restricts the sitemaps that are fetched.
//
// The follow rules are regular expressions. With rules set, a sitemap is
// fetched only if its URL matches one of them, whether a robots.txt names it
// or a sitemap index lists it. A sitemap index that is not followed takes the
// sitemaps it lists with it. The URL passed to Parse is always fetched.
//
// The rules are matched against the absolute URL of a sitemap and match
// anywhere in it unless they are anchored with "^" or "$".
//
// The robots.txt and the sitemaps are served by a local test server, so the
// example runs without network access. The server keeps a list of the
// sitemaps it is asked for.
func main() {
	fmt.Println("=== No follow rules ===")
	parse(sitemap.New())

	// Of the sitemaps the robots.txt names only the sitemap index is followed,
	// and of the sitemaps the index lists only the ones of the products.
	fmt.Println("\n=== Sitemap index and product sitemaps only ===")
	parse(sitemap.New().SetFollow([]string{
		`/sitemap-index\.xml$`,
		`/sitemap-products-\d+\.xml$`,
	}))

	// The sitemap index is not followed, so the sitemaps it lists are not
	// fetched either, although one of the rules matches them.
	fmt.Println("\n=== Sitemap index left out ===")
	parse(sitemap.New().SetFollow([]string{
		`/sitemap-blog\.xml$`,
		`/sitemap-products-\d+\.xml$`,
	}))
}

// parse parses the robots.txt of a local test server with s and prints the
// sitemaps the server was asked for and the number of URLs found in them.
func parse(s *sitemap.S) {
	var (
		mu      sync.Mutex
		fetched []string
	)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			mu.Lock()
			fetched = append(fetched, r.URL.Path)
			mu.Unlock()
		}

		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprintf(w, "Sitemap: %[1]s/sitemap-index.xml\nSitemap: %[1]s/sitemap-blog.xml\nSitemap: %[1]s/sitemap-drafts.xml\n", server.URL)
		case "/sitemap-index.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>%[1]s/sitemap-products-1.xml</loc></sitemap>
  <sitemap><loc>%[1]s/sitemap-products-2.xml</loc></sitemap>
  <sitemap><loc>%[1]s/sitemap-archive.xml</loc></sitemap>
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

	// The sitemaps are fetched one at a time, so that the server is asked for
	// them in the same order every time.
	sm, err := s.SetMultiThread(false).Parse(server.URL+"/robots.txt", nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("%d sitemaps fetched, %d URLs found\n", len(fetched), sm.GetURLCount())
	for _, path := range fetched {
		fmt.Printf("  %s\n", path)
	}
}

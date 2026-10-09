package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"

	"github.com/aafeher/go-sitemap-parser"
)

const (
	// sections is the number of sitemap indexes the sitemap index of the
	// example lists, and the number of sitemaps each of those lists.
	sections = 10
	// pages is the number of pages a sitemap of the example lists.
	pages = 3
)

// main demonstrates how SetMaxSitemaps limits the number of sitemaps a Parse
// call fetches.
//
// A sitemap index may list sitemap indexes, which may list further ones, so
// how many sitemaps a call ends up fetching is up to the server. SetMaxSitemaps
// puts a limit on it, on all levels together. The default is 50,000, which is
// as many sitemaps as a sitemap index may list; 0 lifts the limit.
//
// Once the limit is reached, the sitemaps that are left are not fetched and a
// *ParseError that names the URL the call was made for is recorded in
// GetErrors(). Reaching the limit does not fail Parse(), and the URLs of the
// sitemaps fetched until then remain available via GetURLs().
//
// The sitemap index of the example lists 10 sitemap indexes of 10 sitemaps
// each, which makes 110 sitemaps. They are served by a local test server, so
// the example runs without network access.
func main() {
	fmt.Println("=== Default limit (50,000 sitemaps) ===")
	parse(sitemap.New())

	// With multi-threading off the sitemaps are fetched in the order they are
	// listed, so it is the first 25 that are fetched.
	fmt.Println("\n=== 25 sitemaps at most, multi-threading off ===")
	parse(sitemap.New().SetMaxSitemaps(25).SetMultiThread(false))

	// With multi-threading on, which 25 sitemaps are fetched, and with that the
	// number of URLs, depends on how fast the server answers.
	fmt.Println("\n=== 25 sitemaps at most, multi-threading on ===")
	parse(sitemap.New().SetMaxSitemaps(25))
}

// parse parses the sitemap index of a local test server with s. It prints the
// number of sitemaps the server was asked for, the number of URLs found and
// the errors.
func parse(s *sitemap.S) {
	var requested atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		path := strings.TrimSuffix(r.URL.Path, ".xml")
		switch {
		case path == "/index":
			// The sitemap index the call is made for.
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= sections; i++ {
				fmt.Fprintf(w, `<sitemap><loc>%s/section-%d.xml</loc></sitemap>`, server.URL, i)
			}
			fmt.Fprint(w, `</sitemapindex>`)
		case strings.Count(path, "/") == 1:
			// The sitemap index of a section.
			requested.Add(1)
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= sections; i++ {
				fmt.Fprintf(w, `<sitemap><loc>%s%s/sitemap-%d.xml</loc></sitemap>`, server.URL, path, i)
			}
			fmt.Fprint(w, `</sitemapindex>`)
		default:
			// A sitemap of a section.
			requested.Add(1)
			fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= pages; i++ {
				fmt.Fprintf(w, `<url><loc>%s%s/page-%d</loc></url>`, server.URL, path, i)
			}
			fmt.Fprint(w, `</urlset>`)
		}
	}))
	defer server.Close()

	sm, err := s.Parse(server.URL+"/index.xml", nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("sitemaps fetched: %d (maxSitemaps=%d)\n", requested.Load(), s.GetMaxSitemaps())
	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	for i, e := range sm.GetErrors() {
		fmt.Printf("%d: %v\n", i+1, e)
	}
}

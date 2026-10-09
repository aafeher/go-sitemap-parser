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
	// sitemaps is the number of sitemaps the sitemap index of the example
	// lists.
	sitemaps = 4
	// pages is the number of pages a sitemap of the example lists.
	pages = 250
)

// main demonstrates how SetMaxURLs limits the number of URLs a Parse call
// collects.
//
// The URLs of a call are kept in memory, and how many there are is up to the
// server. SetMaxURLs puts a limit on it, for all the sitemaps of the call
// together. The default is 10,000,000; 0 lifts the limit.
//
// Once the limit is reached, the call stops collecting: what is left of the
// sitemap that reached the limit is left out, the sitemaps that are left are
// not fetched, and a *ParseError that names the URL the call was made for is
// recorded in GetErrors(). Reaching the limit does not fail Parse(), and the
// URLs collected until then remain available via GetURLs().
//
// The sitemap index of the example lists 4 sitemaps of 250 pages each, which
// makes 1,000 URLs. They are served by a local test server, so the example
// runs without network access.
func main() {
	fmt.Println("=== Default limit (10,000,000 URLs) ===")
	parse(sitemap.New())

	// With multi-threading off the sitemaps are fetched in the order they are
	// listed, so it is the first 600 URLs that are collected: those of the
	// first two sitemaps, and 100 of the third one. The fourth one is not
	// fetched.
	fmt.Println("\n=== 600 URLs at most, multi-threading off ===")
	parse(sitemap.New().SetMaxURLs(600).SetMultiThread(false))

	// With multi-threading on, which 600 URLs are collected depends on how fast
	// the server answers, and the sitemaps may all have been asked for by the
	// time the limit is reached.
	fmt.Println("\n=== 600 URLs at most, multi-threading on ===")
	parse(sitemap.New().SetMaxURLs(600))
}

// parse parses the sitemap index of a local test server with s. It prints the
// number of sitemaps the server was asked for, the number of URLs found and
// the errors.
func parse(s *sitemap.S) {
	var requested atomic.Int64

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, ".xml")
		if path == "/index" {
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= sitemaps; i++ {
				fmt.Fprintf(w, `<sitemap><loc>%s/sitemap-%d.xml</loc></sitemap>`, server.URL, i)
			}
			fmt.Fprint(w, `</sitemapindex>`)
			return
		}

		requested.Add(1)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		for i := 1; i <= pages; i++ {
			fmt.Fprintf(w, `<url><loc>%s%s/page-%d</loc></url>`, server.URL, path, i)
		}
		fmt.Fprint(w, `</urlset>`)
	}))
	defer server.Close()

	sm, err := s.Parse(server.URL+"/index.xml", nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("sitemaps fetched: %d\n", requested.Load())
	fmt.Printf("%d URLs (maxURLs=%d), %d errors\n", sm.GetURLCount(), s.GetMaxURLs(), sm.GetErrorsCount())
	for i, e := range sm.GetErrors() {
		fmt.Printf("%d: %v\n", i+1, e)
	}
	if urls := sm.GetURLs(); len(urls) > 0 {
		last := urls[len(urls)-1].Loc
		fmt.Printf("last URL collected: %s\n", strings.TrimPrefix(last, server.URL))
	}
}

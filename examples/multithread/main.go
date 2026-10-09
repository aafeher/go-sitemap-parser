package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// sitemaps is the number of sitemaps the robots.txt of the example lists.
const sitemaps = 4

// main demonstrates what SetMultiThread changes about the way sitemaps are
// fetched.
//
// With multi-threading on (the default) the sitemaps a robots.txt or a sitemap
// index lists are fetched concurrently, as many at a time as SetMaxConcurrency
// allows. With multi-threading off they are fetched one at a time, in the
// order they are listed: that takes longer, but the server never gets more
// than one request at once.
//
// The robots.txt and its sitemaps are served by a local test server, so the
// example runs without network access. The server takes a moment to answer a
// request for a sitemap and keeps count of how many it is answering at once.
func main() {
	fmt.Println("=== Multi-threading on (default) ===")
	parse(sitemap.New())

	fmt.Println("\n=== Multi-threading on, two fetches at most ===")
	parse(sitemap.New().SetMaxConcurrency(2))

	fmt.Println("\n=== Multi-threading off ===")
	parse(sitemap.New().SetMultiThread(false))
}

// parse parses the robots.txt of a local test server with s. It prints the
// number of URLs found, the highest number of requests the server was
// answering at once and the time the call took.
func parse(s *sitemap.S) {
	var (
		mu          sync.Mutex
		inFlight    int
		maxInFlight int
	)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			for i := 1; i <= sitemaps; i++ {
				fmt.Fprintf(w, "Sitemap: %s/sitemap-%d.xml\n", server.URL, i)
			}
			return
		}

		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()

		// Answering a request for a sitemap takes a moment.
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s%s/page</loc></url>
</urlset>`, server.URL, r.URL.Path)
	}))
	defer server.Close()

	start := time.Now()
	sm, err := s.Parse(server.URL+"/robots.txt", nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}
	elapsed := time.Since(start)

	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	fmt.Printf("requests in flight at once: %d\n", maxInFlight)
	fmt.Printf("took about %s\n", elapsed.Round(100*time.Millisecond))
}

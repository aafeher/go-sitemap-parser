package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a robots.txt.
//
// Parse takes the document for a robots.txt when the path of the URL ends in a
// segment that reads "robots.txt". The letter case makes no difference, and
// neither does a query or a fragment that follows the path. Every other URL
// is taken for that of a sitemap.
//
// Of a robots.txt only the "Sitemap:" lines are read. The name of the
// directive is not case-sensitive, a line may be indented, and a "#" begins a
// comment. The sitemaps the lines name are fetched and parsed like the
// sitemaps of a sitemap index, and they may be on another host than the
// robots.txt. Tolerant mode resolves a relative URL against the URL of the
// robots.txt; strict mode requires an absolute one and reports the others.
//
// The robots.txt and the sitemaps are served by a local test server, so the
// example runs without network access.
func main() {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server does not tell the letter cases of a path apart.
		if strings.EqualFold(r.URL.Path, "/robots.txt") {
			fmt.Fprintf(w, `# robots.txt of the example
User-agent: *
Disallow: /private/

Sitemap: %[1]s/sitemap-pages.xml
  sitemap: /sitemap-blog.xml   # a relative URL
# Sitemap: %[1]s/sitemap-drafts.xml
`, server.URL)
			return
		}

		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s%s/page</loc></url>
</urlset>`, server.URL, strings.TrimSuffix(r.URL.Path, ".xml"))
	}))
	defer server.Close()

	// Each of these URLs is that of the robots.txt.
	for _, path := range []string{"/robots.txt", "/robots.txt?v=2", "/ROBOTS.TXT"} {
		fmt.Printf("=== %s ===\n", path)
		parse(sitemap.New(), server.URL, path)
	}

	fmt.Println("=== /robots.txt, strict mode ===")
	parse(sitemap.New().SetStrict(true), server.URL, "/robots.txt")
}

// parse parses the document at path on the server with s. It prints the pages
// found, without the address of the server, which changes from run to run,
// and the errors.
func parse(s *sitemap.S, serverURL, path string) {
	// The sitemaps are fetched one at a time, so that the pages come in the
	// order the robots.txt names their sitemaps.
	sm, err := s.SetMultiThread(false).Parse(serverURL+path, nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("%d URLs found\n", sm.GetURLCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", strings.TrimPrefix(u.Loc, serverURL))
	}
	for _, e := range sm.GetErrors() {
		fmt.Printf("  error: %v\n", e)
	}
}

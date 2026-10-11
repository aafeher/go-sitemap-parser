package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/aafeher/go-sitemap-parser"
)

const (
	// sitemaps is the number of sitemaps the sitemap index of the example
	// lists that can be fetched.
	sitemaps = 4
	// pages is the number of pages a sitemap of the example lists.
	pages = 200
	// invalid is the number of entries of a sitemap of the example that are
	// not valid.
	invalid = 50
)

// main demonstrates how SetMaxValidationErrors limits the number of validation
// errors a Parse call records.
//
// What is wrong with an entry is recorded as a *ValidationError in
// GetErrors(). An entry that is not valid does not count towards SetMaxURLs,
// so how many of these errors there are is up to the server.
// SetMaxValidationErrors puts a limit on it, for all the sitemaps of the call
// together. The default is 100,000; 0 lifts the limit.
//
// Once the limit is reached, the call records no more validation errors, and
// a *ParseError that names the URL the call was made for is recorded in
// GetErrors(). It wraps sitemap.ErrMaxValidationErrors, so errors.Is tells it
// from the other errors. Nothing else changes: the sitemaps are read to their
// end, the URLs are those of a call without the limit, and the errors of the
// other types, such as the one of a sitemap that cannot be fetched, are
// recorded whatever the limit is.
//
// The sitemap index of the example lists 4 sitemaps of 200 pages and 50
// entries that are not valid each, and a sitemap that is not found. They are
// served by a local test server, so the example runs without network access.
func main() {
	fmt.Println("=== Default limit (100,000 validation errors) ===")
	parse(sitemap.New())

	// The first 20 validation errors are recorded, and one error tells that
	// the others were left out. The URLs are all there, and so is the error of
	// the sitemap that is not found.
	fmt.Println("\n=== 20 validation errors at most ===")
	parse(sitemap.New().SetMaxValidationErrors(20))

	fmt.Println("\n=== No limit ===")
	parse(sitemap.New().SetMaxValidationErrors(0))
}

// parse parses the sitemap index of a local test server with s. It prints the
// number of URLs found and what the errors are.
func parse(s *sitemap.S) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, ".xml")
		switch path {
		case "/index":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= sitemaps; i++ {
				fmt.Fprintf(w, `<sitemap><loc>%s/sitemap-%d.xml</loc></sitemap>`, server.URL, i)
			}
			fmt.Fprintf(w, `<sitemap><loc>%s/missing.xml</loc></sitemap>`, server.URL)
			fmt.Fprint(w, `</sitemapindex>`)
		case "/missing":
			http.NotFound(w, r)
		default:
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
			for i := 1; i <= pages; i++ {
				fmt.Fprintf(w, `<url><loc>%s%s/page-%d</loc></url>`, server.URL, path, i)
			}
			for i := 1; i <= invalid; i++ {
				// An entry without a location.
				fmt.Fprint(w, `<url><lastmod>2024-01-15</lastmod></url>`)
			}
			fmt.Fprint(w, `</urlset>`)
		}
	}))
	defer server.Close()

	sm, err := s.Parse(server.URL+"/index.xml", nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("%d URLs (maxValidationErrors=%d), %d errors\n", sm.GetURLCount(), s.GetMaxValidationErrors(), sm.GetErrorsCount())

	validation := 0
	for _, e := range sm.GetErrors() {
		var validationErr *sitemap.ValidationError
		switch {
		case errors.As(e, &validationErr):
			validation++
		case errors.Is(e, sitemap.ErrMaxValidationErrors):
			fmt.Printf("limit reached: %v\n", strings.ReplaceAll(e.Error(), server.URL, ""))
		default:
			fmt.Printf("other error: %v\n", strings.ReplaceAll(e.Error(), server.URL, ""))
		}
	}
	fmt.Printf("validation errors: %d\n", validation)
}

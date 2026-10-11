package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how the URLs on an internationalised host are returned.
//
// The name of such a host can be written in two forms: in Unicode
// ("bücher.example") and in Punycode ("xn--bcher-kva.example"), the ASCII form
// it is looked up by. A sitemap may use either, and may mix them.
//
// Tolerant mode (the default) returns every URL with the Punycode form of its
// host, whichever form the sitemap writes. That is the name a request for the
// URL goes to, so the URLs of one host come back with one name, and can be
// compared and requested as they are. A host name in ASCII is returned the
// way it is written.
//
// Strict mode returns a URL the way the sitemap gives it. It requires the URL
// to be on the host of the sitemap, and takes both forms of the name for the
// same host. Another host is rejected in whichever form it is written.
//
// The sitemap is passed in directly, so the example runs without network
// access.
func main() {
	// The host of the first three entries is that of the sitemap: in Unicode,
	// in Unicode with capitals, and in Punycode. The last entry is on another
	// internationalised host.
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://bücher.example/</loc></url>
  <url><loc>https://BÜCHER.example/about</loc></url>
  <url><loc>https://xn--bcher-kva.example/contact</loc></url>
  <url><loc>https://münchen.example/</loc></url>
</urlset>`

	fmt.Println("=== Tolerant mode ===")
	parse(sitemap.New(), "https://bücher.example/sitemap.xml", content)

	fmt.Println("\n=== Strict mode ===")
	parse(sitemap.New().SetStrict(true), "https://bücher.example/sitemap.xml", content)

	// The sitemap itself may be named by the Punycode form of its host as well,
	// which is the name a request for it goes to.
	fmt.Println("\n=== Strict mode, sitemap named by the Punycode form ===")
	parse(sitemap.New().SetStrict(true), "https://xn--bcher-kva.example/sitemap.xml", content)
}

// parse parses content as the sitemap at url with s and prints the URLs and
// the errors it yields.
func parse(s *sitemap.S, url string, content string) {
	sm, err := s.Parse(url, &content)
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

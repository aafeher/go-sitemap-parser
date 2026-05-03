package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how to supply a custom *http.Client to the parser.
//
// Use SetHTTPClient when you need control over the transport layer that goes
// beyond what SetFetchTimeout and SetUserAgent provide: custom TLS settings,
// proxies, authentication headers via a custom RoundTripper, connection
// pooling tuning, etc.
//
// When a custom client is set, SetFetchTimeout has no effect — the client's
// own Timeout field controls the request deadline.
func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// Example: custom client with a longer timeout and a tailored TLS config.
	customClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	s := sitemap.New().SetHTTPClient(customClient)

	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Found %d URLs\n", sm.GetURLCount())
	for _, u := range sm.GetURLs() {
		fmt.Println(u.Loc)
	}
}

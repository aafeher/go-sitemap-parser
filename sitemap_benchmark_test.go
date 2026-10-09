package sitemap

import (
	"fmt"
	"testing"
)

func Benchmark_New(b *testing.B) {
	b.Run("New", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = New()
		}
	})
}

func Benchmark_Parse(b *testing.B) {
	server := testServer()
	defer server.Close()

	b.Run("invalid url", func(b *testing.B) {
		url := "invalid_url"

		for i := 0; i < b.N; i++ {
			s := New()
			_, err := s.Parse(url, nil)
			if err != nil {
				if err.Error() != "Get \"invalid_url\": unsupported protocol scheme \"\"" {
					b.Error(err)
				}
			}
		}
	})

	b.Run("testServer index page", func(b *testing.B) {
		url := server.URL

		for i := 0; i < b.N; i++ {
			s := New()
			_, err := s.Parse(url, nil)
			if err != nil {
				if err.Error() != "received HTTP status 404" {
					b.Error(err)
				}
			}
		}
	})

	b.Run("robots.txt with sitemapindex.xml", func(b *testing.B) {
		url := server.URL + "/robots-with-sitemapindex/robots.txt"

		for i := 0; i < b.N; i++ {
			s := New()
			_, err := s.Parse(url, nil)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

// Benchmark_Parse_SitemapIndex parses a sitemap index of 16 sitemaps of 5,000
// URLs each, with multi-threading on and off. What it measures is mostly the
// decoding of the sitemaps, the server being local.
func Benchmark_Parse_SitemapIndex(b *testing.B) {
	const sitemaps, pages = 16, 5000

	server := sitemapIndexServer(b, sitemaps, pages, nil)

	for _, multiThread := range []bool{true, false} {
		b.Run(fmt.Sprintf("multiThread=%v", multiThread), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s, err := New().SetMultiThread(multiThread).Parse(server.URL+"/index.xml", nil)
				if err != nil {
					b.Fatal(err)
				}
				if s.GetURLCount() != sitemaps*pages {
					b.Fatalf("expected %d URLs, got %d", sitemaps*pages, s.GetURLCount())
				}
			}
		})
	}
}

package sitemap

import (
	"errors"
	"fmt"
	"testing"
)

// benchmarkInstance holds the instance Benchmark_New creates. An instance that
// nothing uses is not created at all: the compiler drops the call, and the
// benchmark measures an empty loop.
var benchmarkInstance *S

// Benchmark_New measures what creating an instance costs.
func Benchmark_New(b *testing.B) {
	b.Run("New", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchmarkInstance = New()
		}
	})
}

// Benchmark_Parse measures a Parse call that is turned down before anything is
// requested, one whose request fails, and one that follows a robots.txt to the
// sitemaps of a sitemap index.
//
// Each case checks that the call ends the way the case is named for, so that
// it does not measure something else unnoticed. An error is told by its type
// and by the URL it names, not by its message: the wording of a message may
// change, and it is the tests that pin it.
func Benchmark_Parse(b *testing.B) {
	server := testServer()
	defer server.Close()

	// A URL without a scheme is rejected, and nothing is requested.
	b.Run("invalid url", func(b *testing.B) {
		url := "invalid_url"

		var validationErr *ValidationError
		for i := 0; i < b.N; i++ {
			_, err := New().Parse(url, nil)
			if !errors.As(err, &validationErr) || validationErr.URL != url {
				b.Fatalf("expected a *ValidationError for %q, got %T: %v", url, err, err)
			}
		}
	})

	// The test server has no document at its root and answers with 404.
	b.Run("testServer index page", func(b *testing.B) {
		url := server.URL

		var networkErr *NetworkError
		for i := 0; i < b.N; i++ {
			_, err := New().Parse(url, nil)
			if !errors.As(err, &networkErr) || networkErr.URL != url {
				b.Fatalf("expected a *NetworkError for %q, got %T: %v", url, err, err)
			}
		}
	})

	// The robots.txt names a sitemap index of three sitemaps, which hold six
	// URLs together.
	b.Run("robots.txt with sitemapindex.xml", func(b *testing.B) {
		const pages = 6
		url := server.URL + "/robots-with-sitemapindex/robots.txt"

		for i := 0; i < b.N; i++ {
			s, err := New().Parse(url, nil)
			if err != nil {
				b.Fatal(err)
			}
			if s.GetURLCount() != pages {
				b.Fatalf("expected %d URLs, got %d", pages, s.GetURLCount())
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

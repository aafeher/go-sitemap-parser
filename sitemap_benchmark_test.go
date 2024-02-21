package sitemap

import "testing"

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

package sitemap

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestS_setConfigDefaults(t *testing.T) {
	tests := []struct {
		name string
		s    *S
		want config
	}{
		{
			name: "default config",
			s:    &S{},
			want: config{
				userAgent:       "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
				fetchTimeout:    3,
				maxResponseSize: 50 * 1024 * 1024,
				maxDepth:        10,
				multiThread:     true,
				follow:          []string{},
				rules:           []string{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.s.setConfigDefaults()
			if !configsEqual(test.s.cfg, test.want) {
				t.Errorf("expected %v, got %v", test.want, test.s.cfg)
			}
		})
	}
}

func TestS_SetUserAgent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{
			name:      "Empty User Agent",
			userAgent: "",
			want:      "",
		},
		{
			name:      "Normal User Agent",
			userAgent: "Mozilla/5.0 Firefox/61.0",
			want:      "Mozilla/5.0 Firefox/61.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.SetUserAgent(test.userAgent)
			if s.cfg.userAgent != test.want {
				t.Errorf("expected %q, got %q", test.want, s.cfg.userAgent)
			}
		})
	}
}

func TestS_SetFetchTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout uint16
	}{
		{
			name:    "PositiveTimeout",
			timeout: 5,
		},
		{
			name:    "ZeroTimeout",
			timeout: 0,
		},
		{
			name:    "LargeTimeout",
			timeout: 600,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.SetFetchTimeout(test.timeout)
			if s.cfg.fetchTimeout != test.timeout {
				t.Errorf("expected %v, got %v", test.timeout, s.cfg.fetchTimeout)
			}
		})
	}
}

func TestS_SetMultiThread(t *testing.T) {
	tests := []struct {
		name        string
		multiThread bool
	}{
		{
			name:        "MultiThread",
			multiThread: true,
		},
		{
			name:        "Sequential",
			multiThread: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.SetMultiThread(test.multiThread)
			if s.cfg.multiThread != test.multiThread {
				t.Errorf("expected %v, got %v", test.multiThread, s.cfg.multiThread)
			}
		})
	}
}

func TestS_SetMaxResponseSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
	}{
		{
			name: "SmallLimit",
			size: 1024,
		},
		{
			name: "LargeLimit",
			size: 100 * 1024 * 1024,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.SetMaxResponseSize(test.size)
			if s.cfg.maxResponseSize != test.size {
				t.Errorf("expected %v, got %v", test.size, s.cfg.maxResponseSize)
			}
		})
	}
}

func TestS_SetMaxDepth(t *testing.T) {
	tests := []struct {
		name  string
		depth int
	}{
		{
			name:  "ShallowDepth",
			depth: 1,
		},
		{
			name:  "DeepDepth",
			depth: 50,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.SetMaxDepth(test.depth)
			if s.cfg.maxDepth != test.depth {
				t.Errorf("expected %v, got %v", test.depth, s.cfg.maxDepth)
			}
		})
	}
}

func TestS_SetFollow(t *testing.T) {
	t.Run("single call", func(t *testing.T) {
		s := New()
		s.SetFollow([]string{`alpha`, `beta`})
		if len(s.cfg.followRegexes) != 2 {
			t.Errorf("expected 2 regexes, got %d", len(s.cfg.followRegexes))
		}
	})

	t.Run("multiple calls replaces regexes", func(t *testing.T) {
		s := New()
		s.SetFollow([]string{`alpha`, `beta`})
		s.SetFollow([]string{`gamma`})
		if len(s.cfg.followRegexes) != 1 {
			t.Errorf("expected 1 regex, got %d", len(s.cfg.followRegexes))
		}
		if s.cfg.followRegexes[0].String() != "gamma" {
			t.Errorf("expected regex 'gamma', got %q", s.cfg.followRegexes[0].String())
		}
	})

	t.Run("invalid regex appends error", func(t *testing.T) {
		s := New()
		s.SetFollow([]string{`(`})
		if len(s.cfg.followRegexes) != 0 {
			t.Errorf("expected 0 regexes, got %d", len(s.cfg.followRegexes))
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})
}

func TestS_SetRules(t *testing.T) {
	t.Run("single call", func(t *testing.T) {
		s := New()
		s.SetRules([]string{`page`, `post`})
		if len(s.cfg.rulesRegexes) != 2 {
			t.Errorf("expected 2 regexes, got %d", len(s.cfg.rulesRegexes))
		}
	})

	t.Run("multiple calls replaces regexes", func(t *testing.T) {
		s := New()
		s.SetRules([]string{`page`, `post`})
		s.SetRules([]string{`article`})
		if len(s.cfg.rulesRegexes) != 1 {
			t.Errorf("expected 1 regex, got %d", len(s.cfg.rulesRegexes))
		}
		if s.cfg.rulesRegexes[0].String() != "article" {
			t.Errorf("expected regex 'article', got %q", s.cfg.rulesRegexes[0].String())
		}
	})

	t.Run("invalid regex appends error", func(t *testing.T) {
		s := New()
		s.SetRules([]string{`*a`})
		if len(s.cfg.rulesRegexes) != 0 {
			t.Errorf("expected 0 regexes, got %d", len(s.cfg.rulesRegexes))
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})
}

func TestS_SetStrict(t *testing.T) {
	t.Run("default is false", func(t *testing.T) {
		s := New()
		if s.cfg.strict {
			t.Error("expected strict to be false by default")
		}
	})

	t.Run("set to true", func(t *testing.T) {
		s := New()
		result := s.SetStrict(true)
		if !s.cfg.strict {
			t.Error("expected strict to be true")
		}
		if result != s {
			t.Error("expected method chaining to return same instance")
		}
	})

	t.Run("set to false", func(t *testing.T) {
		s := New()
		s.SetStrict(true)
		s.SetStrict(false)
		if s.cfg.strict {
			t.Error("expected strict to be false")
		}
	})
}

func TestS_resolveAndValidateLoc(t *testing.T) {
	baseURL := "https://example.com/sitemaps/index.xml"

	t.Run("tolerant absolute URL", func(t *testing.T) {
		s := New()
		resolved, err := s.resolveAndValidateLoc("https://example.com/page1", baseURL)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resolved != "https://example.com/page1" {
			t.Errorf("expected https://example.com/page1, got %s", resolved)
		}
	})

	t.Run("tolerant relative URL with leading slash", func(t *testing.T) {
		s := New()
		resolved, err := s.resolveAndValidateLoc("/products/page1.html", baseURL)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resolved != "https://example.com/products/page1.html" {
			t.Errorf("expected https://example.com/products/page1.html, got %s", resolved)
		}
	})

	t.Run("tolerant relative URL without leading slash", func(t *testing.T) {
		s := New()
		resolved, err := s.resolveAndValidateLoc("page2.html", baseURL)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resolved != "https://example.com/sitemaps/page2.html" {
			t.Errorf("expected https://example.com/sitemaps/page2.html, got %s", resolved)
		}
	})

	t.Run("tolerant ftp URL rejected", func(t *testing.T) {
		s := New()
		_, err := s.resolveAndValidateLoc("ftp://example.com/file", baseURL)
		if err == nil {
			t.Error("expected error for ftp URL in tolerant mode")
		}
	})

	t.Run("tolerant unparseable loc", func(t *testing.T) {
		s := New()
		_, err := s.resolveAndValidateLoc("%%", baseURL)
		if err == nil {
			t.Error("expected error for unparseable URL")
		}
	})

	t.Run("tolerant unparseable base URL", func(t *testing.T) {
		s := New()
		_, err := s.resolveAndValidateLoc("/page", "%%")
		if err == nil {
			t.Error("expected error for unparseable base URL")
		}
	})

	t.Run("strict valid absolute URL", func(t *testing.T) {
		s := New().SetStrict(true)
		resolved, err := s.resolveAndValidateLoc("https://example.com/page1", baseURL)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resolved != "https://example.com/page1" {
			t.Errorf("expected https://example.com/page1, got %s", resolved)
		}
	})

	t.Run("strict rejects relative URL", func(t *testing.T) {
		s := New().SetStrict(true)
		_, err := s.resolveAndValidateLoc("/products/page1.html", baseURL)
		if err == nil {
			t.Error("expected error for relative URL in strict mode")
		}
	})

	t.Run("strict rejects ftp scheme", func(t *testing.T) {
		s := New().SetStrict(true)
		_, err := s.resolveAndValidateLoc("ftp://example.com/file", baseURL)
		if err == nil {
			t.Error("expected error for ftp URL in strict mode")
		}
	})

	t.Run("strict rejects different host", func(t *testing.T) {
		s := New().SetStrict(true)
		_, err := s.resolveAndValidateLoc("https://other.com/page", baseURL)
		if err == nil {
			t.Error("expected error for different host in strict mode")
		}
	})

	t.Run("strict rejects different protocol", func(t *testing.T) {
		s := New().SetStrict(true)
		_, err := s.resolveAndValidateLoc("http://example.com/page", baseURL)
		if err == nil {
			t.Error("expected error for different protocol in strict mode")
		}
	})

	t.Run("strict rejects URL exceeding 2048 chars", func(t *testing.T) {
		s := New().SetStrict(true)
		longPath := strings.Repeat("a", 2049-len("https://example.com/"))
		longURL := "https://example.com/" + longPath
		_, err := s.resolveAndValidateLoc(longURL, baseURL)
		if err == nil {
			t.Error("expected error for URL exceeding 2048 characters")
		}
	})

	t.Run("strict accepts URL at exactly 2048 chars", func(t *testing.T) {
		s := New().SetStrict(true)
		longPath := strings.Repeat("a", 2048-len("https://example.com/"))
		longURL := "https://example.com/" + longPath
		_, err := s.resolveAndValidateLoc(longURL, baseURL)
		if err != nil {
			t.Errorf("unexpected error for URL at exactly 2048 characters: %v", err)
		}
	})

	t.Run("strict rejects missing host", func(t *testing.T) {
		s := New().SetStrict(true)
		_, err := s.resolveAndValidateLoc("https:///path", baseURL)
		if err == nil {
			t.Error("expected error for URL with missing host in strict mode")
		}
	})
}

func TestS_Parse_TolerantRelativeURLs(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("tolerant resolves relative loc in urlset", func(t *testing.T) {
		s := New()
		content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>/page-01</loc></url>
    <url><loc>/page-02</loc></url>
</urlset>`
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Fatalf("expected 2 URLs, got %d", s.GetURLCount())
		}
		for _, u := range s.GetURLs() {
			if !strings.HasPrefix(u.Loc, server.URL) {
				t.Errorf("expected resolved URL starting with %s, got %s", server.URL, u.Loc)
			}
		}
	})

	t.Run("tolerant resolves relative loc in sitemapindex", func(t *testing.T) {
		s := New().SetMultiThread(false)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>/sitemap-02.xml</loc></sitemap>
</sitemapindex>`)
		sitemapURL := fmt.Sprintf("%s/sitemapindex.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Fatalf("expected 2 URLs from resolved sitemap index, got %d", s.GetURLCount())
		}
	})
}

func TestS_Parse_StrictMode(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("strict rejects relative loc in urlset", func(t *testing.T) {
		s := New().SetStrict(true)
		content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>/page-01</loc></url>
    <url><loc>/page-02</loc></url>
</urlset>`
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 0 {
			t.Errorf("expected 0 URLs in strict mode, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 2 {
			t.Errorf("expected 2 errors in strict mode, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict rejects cross-host loc", func(t *testing.T) {
		s := New().SetStrict(true)
		content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://other-domain.com/page-01</loc></url>
</urlset>`
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 0 {
			t.Errorf("expected 0 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 1 {
			t.Errorf("expected 1 error, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict rejects relative loc in sitemapindex", func(t *testing.T) {
		s := New().SetStrict(true).SetMultiThread(false)
		content := `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>/sub-sitemap.xml</loc></sitemap>
</sitemapindex>`
		sitemapURL := fmt.Sprintf("%s/sitemapindex.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 0 {
			t.Errorf("expected 0 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 1 {
			t.Errorf("expected 1 error, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict accepts same-host absolute URLs", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc></url>
    <url><loc>%s/page-02</loc></url>
</urlset>`, server.URL, server.URL)
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Errorf("expected 2 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d", s.GetErrorsCount())
		}
	})
}

func TestS_Parse(t *testing.T) {
	server := testServer()
	defer server.Close()

	timeLocationUTC, err := time.LoadLocation("UTC")
	if err != nil {
		t.Errorf("%v", err)
	}

	timeLocationCET, err := time.LoadLocation("CET")
	if err != nil {
		t.Errorf("%v", err)
	}

	tests := []struct {
		name                 string
		url                  string
		multiThread          bool
		follow               []string
		rules                []string
		content              *string
		err                  *string
		mainURLContent       *string
		robotsTxtSitemapURLs []string
		sitemapLocations     []string
		urls                 []URL
		errs                 []error
	}{
		{
			name:                 "unparseable url",
			url:                  "%%",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL: parse \"%%\": invalid URL escape \"%%\""),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL: %w", &url.Error{Op: "parse", URL: "%%", Err: url.EscapeError("%%")}),
			},
		},
		{
			name:                 "invalid url",
			url:                  "invalid_url",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL scheme %q: only http and https are supported", ""),
			},
		},
		{
			name:                 "empty url",
			url:                  "",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL scheme %q: only http and https are supported", ""),
			},
		},
		{
			name:                 "relative url",
			url:                  "/just/a/path",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL scheme %q: only http and https are supported", ""),
			},
		},
		{
			name:                 "missing host",
			url:                  "http://",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL: missing host"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL: missing host"),
			},
		},
		{
			name:                 "ftp url",
			url:                  "ftp://example.com/sitemap.xml",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("invalid URL scheme \"ftp\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				fmt.Errorf("invalid URL scheme %q: only http and https are supported", "ftp"),
			},
		},
		{
			name:                 "testServer index page",
			url:                  server.URL,
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("received HTTP status 404"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("received HTTP status 404")},
		},
		{
			name:                 "page not found",
			url:                  fmt.Sprintf("%s/404", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("received HTTP status 404"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("received HTTP status 404")},
		},

		// robots.txt
		{
			name:                 "robots.txt empty file",
			url:                  fmt.Sprintf("%s/robots-empty/robots.txt", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
		},
		{
			name:                 "robots.txt empty content",
			url:                  fmt.Sprintf("%s/robots-empty/robots.txt", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              pointerOfString(""),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
		},
		{
			name:                 "robots.txt without sitemap",
			url:                  fmt.Sprintf("%s/robots-without-sitemap/robots.txt", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString("User-agent: *\nDisallow: /\n\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
		},
		{
			name:                 "robots.txt with sitemapindex",
			url:                  fmt.Sprintf("%s/robots-with-sitemapindex/robots.txt", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("User-agent: *\nDisallow: /\n\nSitemap: %s/sitemapindex-1.xml\n\n", server.URL)),
			robotsTxtSitemapURLs: []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)},
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-01.xml", server.URL),
				fmt.Sprintf("%s/sitemap-02.xml", server.URL),
				fmt.Sprintf("%s/sitemap-03.xml", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
		{
			name:           "robots.txt with two sitemapindex",
			url:            fmt.Sprintf("%s/robots-with-sitemapindex-2/robots.txt", server.URL),
			multiThread:    false,
			follow:         []string{},
			rules:          []string{},
			mainURLContent: pointerOfString(fmt.Sprintf("User-agent: *\nDisallow: /\n\nSitemap: %s/sitemapindex-1.xml\nSitemap: %s/sitemapindex-2.xml\n\n", server.URL, server.URL)),
			robotsTxtSitemapURLs: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
				fmt.Sprintf("%s/sitemapindex-2.xml", server.URL),
			},
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-01.xml", server.URL),
				fmt.Sprintf("%s/sitemap-02.xml", server.URL),
				fmt.Sprintf("%s/sitemap-03.xml", server.URL),
				fmt.Sprintf("%s/sitemapindex-2.xml", server.URL),
				fmt.Sprintf("%s/sitemap-04.xml", server.URL),
				fmt.Sprintf("%s/sitemap-05.xml", server.URL),
				fmt.Sprintf("%s/sitemap-06.xml", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-07", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqNever),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-08", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-09", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-10", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-11", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-12", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
		{
			name:                 "robots.txt with invalid sitemap",
			url:                  fmt.Sprintf("%s/robots-with-invalid-sitemap/robots.txt", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("User-agent: *\nDisallow: /\n\nSitemap: %s/invalid.xml\n\n", server.URL)),
			robotsTxtSitemapURLs: []string{fmt.Sprintf("%s/invalid.xml", server.URL)},
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("received HTTP status 404")},
		},
		{
			name:                 "robots.txt with sitemapindex.xml.gz",
			url:                  fmt.Sprintf("%s/robots-with-sitemapindex-gz/robots.txt", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("User-agent: *\nDisallow: /\n\nSitemap: %s/sitemapindex-1.xml.gz\n\n", server.URL)),
			robotsTxtSitemapURLs: []string{fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL)},
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-01.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-03.xml.gz", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},

		// sitemapindex.xml.gz
		{
			name:                 "sitemapindex.xml.gz corrupted file",
			url:                  fmt.Sprintf("%s/sitemapindex-empty-corrupted.xml.gz", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString("error: gzip: invalid checksum\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("EOF"), errors.New("EOF")},
		},
		{
			name:                 "sitemapindex.xml.gz empty file",
			url:                  fmt.Sprintf("%s/sitemapindex-empty.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("sitemapindex is empty"), errors.New("sitemap is empty")},
		},
		{
			name:                 "sitemapindex.xml.gz",
			url:                  fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-01.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-02.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-03.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>", server.URL, server.URL, server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-01.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-03.xml.gz", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},

		// sitemap.xml.gz
		{
			name:                 "sitemap.xml.gz empty file",
			url:                  fmt.Sprintf("%s/sitemap-empty.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("sitemapindex is empty"), errors.New("sitemap is empty")},
		},
		{
			name:                 "sitemap.xml.gz",
			url:                  fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url>\n        <loc>%s/page-02</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>hourly</changefreq>\n        <priority>0.5</priority>\n    </url>\n    <url>\n        <loc>%s/page-03</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>daily</changefreq>\n        <priority>0.5</priority>\n    </url>\n</urlset>\n", server.URL, server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},

		// sitemapindex
		{
			name:                 "sitemapindex.xml empty file",
			url:                  fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("EOF"), errors.New("EOF")},
		},
		{
			name:                 "sitemapindex.xml empty content",
			url:                  fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              pointerOfString("\n"),
			mainURLContent:       pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("EOF"), errors.New("EOF")},
		},
		{
			name:                 "sitemapindex.xml",
			url:                  fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-01.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-02.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-03.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>", server.URL, server.URL, server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-01.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
				fmt.Sprintf("%s/sitemap-03.xml.gz", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
		{
			name:                 "sitemapindex.xml with invalid sitemap",
			url:                  fmt.Sprintf("%s/sitemapindex-with-invalid-sitemap.xml", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              nil,
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/invalid.xml</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>\n", server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-with-invalid-sitemap.xml", server.URL),
				fmt.Sprintf("%s/invalid.xml", server.URL),
			},
			urls: nil,
			errs: []error{errors.New("received HTTP status 404")},
		},
		{
			name:                 "sitemapindex with follow and rules",
			url:                  fmt.Sprintf("%s/sitemapindex-follow-1.xml", server.URL),
			multiThread:          false,
			follow:               []string{`alpha`},
			rules:                []string{`page`},
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-follow-alpha-01.xml</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-follow-alpha-02.xml</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-follow-beta-01.xml</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>\n", server.URL, server.URL, server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-follow-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-follow-alpha-01.xml", server.URL),
				fmt.Sprintf("%s/sitemap-follow-alpha-02.xml", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-alpha-01", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-alpha-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
		{
			name:                 "sitemapindex with rules error",
			url:                  "",
			multiThread:          false,
			follow:               []string{},
			rules:                []string{`*a`},
			err:                  pointerOfString("errors occurred before parsing, see GetErrors() for details"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				&syntax.Error{
					Code: syntax.ErrorCode("missing argument to repetition operator"),
					Expr: "*",
				},
			},
		},
		{
			name:                 "sitemapindex with follow error",
			url:                  "",
			multiThread:          false,
			follow:               []string{`(`},
			rules:                []string{},
			err:                  pointerOfString("errors occurred before parsing, see GetErrors() for details"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				&syntax.Error{
					Code: syntax.ErrorCode("missing closing )"),
					Expr: "(",
				},
			},
		},

		// sitemap
		{
			name:                 "sitemap.xml empty file",
			url:                  fmt.Sprintf("%s/sitemap-empty.xml", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("EOF"), errors.New("EOF")},
		},
		{
			name:                 "sitemap.xml empty content",
			url:                  fmt.Sprintf("%s/sitemap-empty.xml", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              pointerOfString("\n"),
			mainURLContent:       pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New("EOF"), errors.New("EOF")},
		},
		{
			name:                 "sitemap.xml",
			url:                  fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			mainURLContent:       pointerOfString(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url>\n        <loc>%s/page-02</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>hourly</changefreq>\n        <priority>0.5</priority>\n    </url>\n    <url>\n        <loc>%s/page-03</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>daily</changefreq>\n        <priority>0.5</priority>\n    </url>\n</urlset>\n", server.URL, server.URL)),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			sitemap, err := s.SetMultiThread(test.multiThread).SetFollow(test.follow).SetRules(test.rules).Parse(test.url, test.content)
			if err != nil {
				if err.Error() != *test.err {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if sitemap == nil {
				t.Fatal("Expected not nil object, but got nil")
			}

			if err == nil {
				if sitemap.mainURL != test.url {
					t.Fatalf("Expected URL to be %s, but got %s", test.url, sitemap.mainURL)
				}
			}

			if !reflect.DeepEqual(sitemap.mainURLContent, *test.mainURLContent) {
				t.Error("mainURLContent is not equal to expected value")
			}
			if !reflect.DeepEqual(sitemap.robotsTxtSitemapURLs, test.robotsTxtSitemapURLs) {
				t.Error("robotsTxtSitemapURLs is not equal to expected value")
			}
			if !compareSitemapLocationsArray(sitemap.sitemapLocations, test.sitemapLocations) {
				t.Error("sitemapLocations is not equal to expected value")
			}
			if !compareURLsArray(sitemap.urls, test.urls) {
				t.Error("urls is not equal to expected value")
			}
			if !reflect.DeepEqual(sitemap.errs, test.errs) {
				t.Error("errs is not equal to expected value")
			}
		})
	}
}

func TestS_Parse_Reuse(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := New().SetMultiThread(false)

	// First parse: sitemap with 2 URLs
	content1 := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url><loc>%s/page-01</loc></url>\n    <url><loc>%s/page-02</loc></url>\n</urlset>", server.URL, server.URL)
	_, err := s.Parse(fmt.Sprintf("%s/sitemap-02.xml", server.URL), &content1)
	if err != nil {
		t.Fatalf("first Parse failed: %v", err)
	}
	if s.GetURLCount() != 2 {
		t.Fatalf("after first parse: expected 2 URLs, got %d", s.GetURLCount())
	}

	// Second parse: sitemap with 1 URL
	content2 := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url><loc>%s/page-03</loc></url>\n</urlset>", server.URL)
	_, err = s.Parse(fmt.Sprintf("%s/sitemap-03.xml", server.URL), &content2)
	if err != nil {
		t.Fatalf("second Parse failed: %v", err)
	}
	if s.GetURLCount() != 1 {
		t.Errorf("after second parse: expected 1 URL, got %d", s.GetURLCount())
	}
	if s.GetErrorsCount() != 0 {
		t.Errorf("after second parse: expected 0 errors, got %d", s.GetErrorsCount())
	}
}

func TestS_Parse_ConcurrentSafety(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := New()

	content := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url><loc>%s/page-01</loc></url>\n    <url><loc>%s/page-02</loc></url>\n</urlset>", server.URL, server.URL)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := content
			_, _ = s.Parse(fmt.Sprintf("%s/sitemap.xml", server.URL), &c)
		}()
	}
	wg.Wait()
}

func TestS_GetErrorsCount(t *testing.T) {
	tests := []struct {
		name          string
		errorsOccured int
		s             *S
		want          int64
	}{
		{
			name:          "No errors",
			errorsOccured: 0,
			s:             New(),
			want:          0,
		},
		{
			name:          "One error",
			errorsOccured: 1,
			s: func(s *S) *S {
				s.errs = append(s.errs, errors.New("Dummy error"))
				return s
			}(New()),
			want: 1,
		},
		{
			name:          "Multiple errors",
			errorsOccured: 3,
			s: func(s *S) *S {
				for i := 0; i < 3; i++ {
					s.errs = append(s.errs, errors.New(fmt.Sprintf("Dummy error %d", i)))
				}
				return s
			}(New()),
			want: 3,
		},
		{
			name:          "Nil receiver",
			errorsOccured: 0,
			s:             nil,
			want:          0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.GetErrorsCount()
			if got != test.want {
				t.Errorf("expected %v, got %v", test.want, got)
			}
		})

	}
}

func TestS_GetErrors(t *testing.T) {
	tests := []struct {
		name string
		s    *S
		want []error
	}{
		{
			name: "No error",
			s:    New(),
			want: []error{},
		},
		{
			name: "Multiple errors",
			s:    &S{errs: []error{fmt.Errorf("error1"), fmt.Errorf("error2")}},
			want: []error{fmt.Errorf("error1"), fmt.Errorf("error2")},
		},
		{
			name: "Nil receiver",
			s:    nil,
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.GetErrors()

			if len(got) != len(test.want) {
				t.Fatalf("unexpected length of errors. want: %d, got: %d", len(test.want), len(got))
			}

			for i, err := range got {
				if err.Error() != test.want[i].Error() {
					t.Errorf("unexpected error message. want: %s, got: %s", test.want[i].Error(), err.Error())
				}
			}
		})
	}
}

func TestS_GetURLs(t *testing.T) {
	tests := []struct {
		name string
		s    *S
		want []URL
	}{
		{
			name: "nil receiver",
			s:    nil,
			want: []URL{},
		},
		{
			name: "No URLs",
			s:    &S{},
			want: []URL{},
		},
		{
			name: "Single URL",
			s:    &S{urls: []URL{{Loc: "http://www.sitemaps.org/1"}}},
			want: []URL{{Loc: "http://www.sitemaps.org/1"}},
		},
		{
			name: "Multiple URLs",
			s: &S{urls: []URL{
				{Loc: "http://www.sitemaps.org/1"},
				{Loc: "http://www.sitemaps.org/2"},
			}},
			want: []URL{
				{Loc: "http://www.sitemaps.org/1"},
				{Loc: "http://www.sitemaps.org/2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetURLs(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetURLs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestS_GetURLCount(t *testing.T) {
	tests := []struct {
		name     string
		s        *S
		expected int64
	}{
		{
			name:     "nil S",
			s:        nil,
			expected: 0,
		},
		{
			name:     "Empty URL slice in S",
			s:        &S{urls: []URL{}},
			expected: 0,
		},
		{
			name:     "One URL in S",
			s:        &S{urls: []URL{{}}},
			expected: 1,
		},
		{
			name:     "Multiple URLs in S",
			s:        &S{urls: []URL{{}, {}, {}, {}}},
			expected: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.GetURLCount()
			if got != test.expected {
				t.Errorf("Expected: %v, but got: %v", test.expected, got)
			}
		})
	}
}

func TestS_GetRandomURLs(t *testing.T) {
	tests := []struct {
		name    string
		s       *S
		n       int
		wantLen int
	}{
		{
			name:    "nil receiver",
			s:       nil,
			n:       5,
			wantLen: 0,
		},
		{
			name: "empty URL list",
			s: &S{
				urls: []URL{},
			},
			n:       5,
			wantLen: 0,
		},
		{
			name: "non-empty URL list, n is greater than len(urls)",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       5,
			wantLen: 3,
		},
		{
			name: "non-empty URL list, n is less than len(urls)",
			s: &S{
				urls: []URL{{}, {}, {}, {}},
			},
			n:       2,
			wantLen: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.GetRandomURLs(test.n)

			if len(got) != test.wantLen {
				t.Errorf("GetRandomURLs() = %v, wantLen %v", len(got), test.wantLen)
			}
		})
	}

	t.Run("does not modify original urls", func(t *testing.T) {
		urls := []URL{
			{Loc: "http://example.com/1"},
			{Loc: "http://example.com/2"},
			{Loc: "http://example.com/3"},
			{Loc: "http://example.com/4"},
		}
		s := &S{urls: urls}
		originalLen := len(s.urls)
		originalLocs := make([]string, len(s.urls))
		for i, u := range s.urls {
			originalLocs[i] = u.Loc
		}

		_ = s.GetRandomURLs(2)

		if len(s.urls) != originalLen {
			t.Errorf("expected urls length %d, got %d", originalLen, len(s.urls))
		}
		for i, u := range s.urls {
			if u.Loc != originalLocs[i] {
				t.Errorf("urls[%d].Loc = %q, want %q", i, u.Loc, originalLocs[i])
			}
		}
	})
}

func TestS_setContent(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name           string
		setup          func() *S
		attrURLContent *string
		wantURLContent string
		wantErr        error
	}{
		{
			name: "setContent_with_urlContent",
			setup: func() *S {
				s := New()
				s.mainURL = fmt.Sprintf("%s/example", server.URL)
				return s
			},
			attrURLContent: pointerOfString("URL Content"),
			wantURLContent: "URL Content",
			wantErr:        nil,
		},
		{
			name: "setContent_without_urlContent",
			setup: func() *S {
				s := New()
				s.mainURL = fmt.Sprintf("%s/example", server.URL)
				return s
			},
			attrURLContent: nil,
			wantURLContent: "example content\n",
			wantErr:        nil,
		},
		{
			name: "setContent_without_urlContent_with_invalid_mainURL",
			setup: func() *S {
				s := New()
				s.mainURL = fmt.Sprintf("%s/404", server.URL)
				return s
			},
			attrURLContent: nil,
			wantURLContent: "",
			wantErr:        fmt.Errorf("received HTTP status 404"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.setup()
			retURLContent, err := s.setContent(test.attrURLContent)
			if retURLContent != test.wantURLContent {
				t.Errorf("unexpected urlContent: got %v, want %v", retURLContent, test.wantURLContent)
			}
			if err != nil && test.wantErr != nil {
				if err.Error() != test.wantErr.Error() {
					t.Errorf("unexpected err: got %v, want %v", err, test.wantErr)
				}
			} else if err != nil && test.wantErr == nil {
				t.Errorf("unexpected err: got %v, want %v", err, test.wantErr)
			} else if err == nil && test.wantErr != nil {
				t.Errorf("unexpected err: got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestS_parseRobotsTXT(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output int
	}{
		{
			name:   "empty robots.txt",
			input:  "",
			output: 0,
		},
		{
			name:   "robots.txt without Sitemap",
			input:  "User-agent: *\nDisallow: /",
			output: 0,
		},
		{
			name:   "robots.txt with a Sitemap",
			input:  "Sitemap: https://example.com\nUser-agent: *",
			output: 1,
		},
		{
			name:   "robots.txt with multiple Sitemap",
			input:  "Sitemap: https://example.com\nSitemap: https://example.com",
			output: 2,
		},
		{
			name:   "robots.txt with CRLF line endings",
			input:  "User-agent: *\r\nDisallow: /\r\nSitemap: https://example.com\r\n",
			output: 1,
		},
		{
			name:   "robots.txt with lowercase sitemap directive",
			input:  "sitemap: https://example.com/lower",
			output: 1,
		},
		{
			name:   "robots.txt with mixed case sitemap directive",
			input:  "SITEMAP: https://example.com/upper\nSiteMap: https://example.com/mixed",
			output: 2,
		},
		{
			name:   "robots.txt with empty sitemap value",
			input:  "Sitemap: ",
			output: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.parseRobotsTXT(test.input)

			if len(s.robotsTxtSitemapURLs) != test.output {
				t.Errorf("Input %s: expected %d, got %d", test.input, test.output, len(s.robotsTxtSitemapURLs))
			}
			for i, u := range s.robotsTxtSitemapURLs {
				if strings.ContainsRune(u, '\r') {
					t.Errorf("robotsTxtSitemapURLs[%d] contains \\r: %q", i, u)
				}
			}
		})
	}
}

func TestS_fetch(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := S{cfg: config{fetchTimeout: 3, maxResponseSize: 50 * 1024 * 1024}}
	type fields struct {
		cfg config
	}
	tests := []struct {
		name    string
		fields  fields
		url     string
		wantErr bool
	}{
		{
			name:    "Empty URL",
			fields:  fields{s.cfg},
			url:     "",
			wantErr: true,
		},
		{
			name:    "Invalid URL",
			fields:  fields{s.cfg},
			url:     "https:bad_domain",
			wantErr: true,
		},
		{
			name:    "404 HTTP response",
			fields:  fields{s.cfg},
			url:     fmt.Sprintf("%s/404", server.URL),
			wantErr: true,
		},
		{
			name:    "Expected HTTP Response",
			fields:  fields{s.cfg},
			url:     fmt.Sprintf("%s/sitemap-01.xml", server.URL),
			wantErr: false,
		},
		{
			name:    "Timeout URL",
			fields:  fields{config{fetchTimeout: 0, maxResponseSize: 50 * 1024 * 1024}},
			url:     fmt.Sprintf("%s/sitemap-01.xml", server.URL),
			wantErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := &S{
				cfg: test.fields.cfg,
			}
			_, err := s.fetch(test.url)
			if (err != nil) != test.wantErr {
				t.Errorf("fetch() error = %v, wantErr %v", err, test.wantErr)
				return
			}
		})
	}
}

func TestS_fetch_ResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("A"), 1024))
	}))
	defer server.Close()

	t.Run("within limit", func(t *testing.T) {
		s := New().SetMaxResponseSize(2048)
		_, err := s.fetch(server.URL)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		s := New().SetMaxResponseSize(512)
		_, err := s.fetch(server.URL)
		if err == nil {
			t.Error("expected error for oversized response, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "response size exceeds limit") {
			t.Errorf("expected size limit error, got: %v", err)
		}
	})
}

func TestS_fetch_NewRequestError(t *testing.T) {
	e := New()

	_, err := e.fetch("://invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL but got none")
	}
}

func TestS_fetch_IOCopyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 1000; i++ {
			_, err := w.Write([]byte("Some content that will be interrupted"))
			if err != nil {
				return
			}
		}
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, _ := hijacker.Hijack()
			err := conn.Close()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	e := New()
	e.SetFetchTimeout(1)

	_, err := e.fetch(server.URL)
	if err == nil {
		t.Error("expected io.Copy error but got none")
	}
}

func TestS_checkAndUnzipContent(t *testing.T) {
	// Preparing gzipped data
	//gzipPrefix := []byte("\x1f\x8b\x08")
	buffer := new(bytes.Buffer)
	writer := gzip.NewWriter(buffer)
	_, err := writer.Write([]byte("test content"))
	if err != nil {
		return
	}
	err = writer.Close()
	if err != nil {
		return
	}

	gzippedContent := buffer.Bytes()

	tests := []struct {
		name    string
		content []byte
		want    []byte
	}{
		{
			name:    "Uncompressed data",
			content: []byte("plain content"),
			want:    []byte("plain content"),
		},
		{
			name:    "Gzipped data",
			content: gzippedContent,
			want:    []byte("test content"),
		},
		{
			name:    "Invalid data",
			content: []byte("\x1f\x8b\x08" + "invalid"), // gzip prefix + invalid content
			want:    []byte("\x1f\x8b\x08" + "invalid"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S{
				errs: []error{},
			}

			got := s.checkAndUnzipContent(tt.content)

			if !bytes.Equal(got, tt.want) {
				t.Errorf("checkAndUnzipContent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestS_parseAndFetchUrlsMultiThread(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name      string
		locations []string
		urlsCount int64
		errsCount int64
	}{
		{
			name: "emptyStrings",
			locations: []string{
				"",
				"",
			},
			urlsCount: 0,
			errsCount: 2,
		},
		{
			name: "invalidURLs",
			locations: []string{
				"invalid_url",
				"http://[::1]",
			},
			urlsCount: 0,
			errsCount: 2,
		},
		{
			name: "mainURLs",
			locations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-04.xml", server.URL),
			},
			urlsCount: 7,
			errsCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := &S{cfg: config{userAgent: "test-agent", fetchTimeout: 3, maxResponseSize: 50 * 1024 * 1024, maxDepth: 10}, errs: []error{}}
			s.parseAndFetchUrlsMultiThread(test.locations, 0)

			if len(s.urls) != int(test.urlsCount) {
				t.Errorf("expected %d, got %d", test.urlsCount, len(s.urls))
			}

			if len(s.errs) != int(test.errsCount) {
				t.Errorf("expected %d, got %d", test.errsCount, len(s.errs))
			}
		})
	}
}

func TestS_parseAndFetchUrlsSequential(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name      string
		locations []string
		urlsCount int64
		errsCount int64
	}{
		{
			name: "emptyStrings",
			locations: []string{
				"",
				"",
			},
			urlsCount: 0,
			errsCount: 2,
		},
		{
			name: "invalidURLs",
			locations: []string{
				"invalid_url",
				"http://[::1]",
			},
			urlsCount: 0,
			errsCount: 2,
		},
		{
			name: "mainURLs",
			locations: []string{
				fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-04.xml", server.URL),
			},
			urlsCount: 7,
			errsCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := &S{cfg: config{userAgent: "test-agent", fetchTimeout: 3, maxResponseSize: 50 * 1024 * 1024, maxDepth: 10}, errs: []error{}}
			s.parseAndFetchUrlsSequential(test.locations, 0)

			if len(s.urls) != int(test.urlsCount) {
				t.Errorf("expected %d, got %d", test.urlsCount, len(s.urls))
			}

			if len(s.errs) != int(test.errsCount) {
				t.Errorf("expected %d, got %d", test.errsCount, len(s.errs))
			}
		})
	}
}

func TestS_parseAndFetchUrlsMultiThread_MaxDepth(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := New().SetMaxDepth(0)
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsMultiThread(locations, 0)

	if len(s.urls) != 0 {
		t.Errorf("expected 0 URLs at depth limit, got %d", len(s.urls))
	}
	if s.GetErrorsCount() != 1 {
		t.Errorf("expected 1 depth error, got %d", s.GetErrorsCount())
	}
	if !strings.Contains(s.GetErrors()[0].Error(), "max recursion depth") {
		t.Errorf("expected max recursion depth error, got: %v", s.GetErrors()[0])
	}
}

func TestS_parseAndFetchUrlsSequential_MaxDepth(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := New().SetMaxDepth(0).SetMultiThread(false)
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsSequential(locations, 0)

	if len(s.urls) != 0 {
		t.Errorf("expected 0 URLs at depth limit, got %d", len(s.urls))
	}
	if s.GetErrorsCount() != 1 {
		t.Errorf("expected 1 depth error, got %d", s.GetErrorsCount())
	}
	if !strings.Contains(s.GetErrors()[0].Error(), "max recursion depth") {
		t.Errorf("expected max recursion depth error, got: %v", s.GetErrors()[0])
	}
}

func TestS_parse(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name                       string
		url                        string
		content                    string
		sitemapLocationsAddedCount int64
		urlsCount                  int64
		errsCount                  int64
	}{
		{
			name:                       "SitemapIndex",
			url:                        fmt.Sprintf("%s/sitemapindex-1.xml", server.URL),
			content:                    fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-01.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-02.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-03.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>", server.URL, server.URL, server.URL),
			sitemapLocationsAddedCount: 3,
			urlsCount:                  0,
			errsCount:                  0,
		},
		{
			name:                       "URLSet",
			url:                        fmt.Sprintf("%s/sitemap-02.xml", server.URL),
			content:                    fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url>\n        <loc>%s/page-02</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>hourly</changefreq>\n        <priority>0.5</priority>\n    </url>\n    <url>\n        <loc>%s/page-03</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>daily</changefreq>\n        <priority>0.5</priority>\n    </url>\n</urlset>\n", server.URL, server.URL),
			sitemapLocationsAddedCount: 0,
			urlsCount:                  2,
			errsCount:                  0,
		},
		{
			name:                       "invalid content",
			url:                        fmt.Sprintf("%s/invalid.xml", server.URL),
			content:                    "invalid content",
			sitemapLocationsAddedCount: 0,
			urlsCount:                  0,
			errsCount:                  2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			sitemapLocationsAdded := s.parse(test.url, test.content)

			if len(sitemapLocationsAdded) != int(test.sitemapLocationsAddedCount) {
				t.Errorf("expected %d, got %d", test.sitemapLocationsAddedCount, len(sitemapLocationsAdded))
			}

			if len(s.urls) != int(test.urlsCount) {
				t.Errorf("expected %d, got %d", test.urlsCount, len(s.urls))
			}

			if len(s.errs) != int(test.errsCount) {
				t.Errorf("expected %d, got %d", test.errsCount, len(s.errs))
			}
		})
	}
}

func TestS_parseSitemapIndex(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name         string
		data         string
		sitemapIndex sitemapIndex
		err          error
	}{
		{
			name: "empty content",
			data: "",
			err:  errors.New("sitemapindex is empty"),
		},
		{
			name: "invalid content",
			data: "invalid content",
			err:  errors.New("EOF"),
		},
		{
			name: "SitemapIndex",
			data: fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-01.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-02.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-03.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>", server.URL, server.URL, server.URL),
			err:  nil,
		},
		{
			name: "URLSet",
			data: fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url>\n        <loc>%s/page-02</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>hourly</changefreq>\n        <priority>0.5</priority>\n    </url>\n    <url>\n        <loc>%s/page-03</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>daily</changefreq>\n        <priority>0.5</priority>\n    </url>\n</urlset>\n", server.URL, server.URL),
			err:  xml.UnmarshalError("expected element type <sitemapindex> but have <urlset>"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			_, err := s.parseSitemapIndex(test.data)

			if test.err != nil {
				if err == nil {
					t.Errorf("expected %v, got %v", test.err, err)
				} else if err.Error() != test.err.Error() {
					t.Errorf("expected %v, got %v", test.err, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected %v, got %v", test.err, err)
				}
			}
		})
	}
}

func TestS_parseURLSet(t *testing.T) {
	server := testServer()
	defer server.Close()

	tests := []struct {
		name string
		data string
		err  error
	}{
		{
			name: "empty content",
			data: "",
			err:  errors.New("sitemap is empty"),
		},
		{
			name: "invalid content",
			data: "invalid content",
			err:  errors.New("EOF"),
		},
		{
			name: "Sitemap",
			data: fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <sitemap>\n        <loc>%s/sitemap-01.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-02.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n    <sitemap>\n        <loc>%s/sitemap-03.xml.gz</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n    </sitemap>\n</sitemapindex>", server.URL, server.URL, server.URL),
			err:  xml.UnmarshalError("expected element type <urlset> but have <sitemapindex>"),
		},
		{
			name: "URLSet",
			data: fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n    <url>\n        <loc>%s/page-02</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>hourly</changefreq>\n        <priority>0.5</priority>\n    </url>\n    <url>\n        <loc>%s/page-03</loc>\n        <lastmod>2024-02-12T12:34:56+01:00</lastmod>\n        <changefreq>daily</changefreq>\n        <priority>0.5</priority>\n    </url>\n</urlset>\n", server.URL, server.URL),
			err:  nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			_, err := s.parseURLSet(test.data)

			if test.err != nil {
				if err == nil {
					t.Errorf("expected %v, got %v", test.err, err)
				} else if err.Error() != test.err.Error() {
					t.Errorf("expected %v, got %v", test.err, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected %v, got %v", test.err, err)
				}
			}
		})
	}
}

func TestUnzip(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		output   []byte
		hasError bool
	}{
		{
			name:     "Valid content",
			input:    gzipByte("hello world"),
			output:   []byte("hello world"),
			hasError: false,
		},
		{
			name:     "Invalid gzip content",
			input:    []byte("\x1f\x8b\x08" + "invalid"),
			output:   []byte("\x1f\x8b\x08" + "invalid"),
			hasError: true,
		},
		{
			name:     "Invalid content",
			input:    []byte("invalid"),
			output:   []byte("invalid"),
			hasError: true,
		},
		{
			name:     "Empty content",
			input:    []byte(""),
			output:   nil,
			hasError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uncompressed, err := unzip(test.input)

			if (err != nil) != test.hasError {
				t.Errorf("expected %v, got %v", test.hasError, err)
			}

			if !bytes.Equal(uncompressed, test.output) {
				t.Errorf("expected %v, got %v", test.output, uncompressed)
			}

		})
	}
}

func TestLastModTime_UnmarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		xmlInput string
		want     time.Time
		wantErr  bool
	}{
		{
			name:     "Year only",
			xmlInput: "<lastmod>2023</lastmod>",
			want:     time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "Year-Month",
			xmlInput: "<lastmod>2023-06</lastmod>",
			want:     time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "Year-Month-Day",
			xmlInput: "<lastmod>2023-06-15</lastmod>",
			want:     time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "ISO8601 with timezone offset",
			xmlInput: "<lastmod>2023-06-15T10:30:00-07:00</lastmod>",
			want:     time.Date(2023, 6, 15, 10, 30, 0, 0, time.FixedZone("", -7*60*60)),
			wantErr:  false,
		},
		{
			name:     "ISO8601 with Z timezone",
			xmlInput: "<lastmod>2023-06-15T10:30:00Z</lastmod>",
			want:     time.Date(2023, 6, 15, 10, 30, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "ISO8601 with microseconds",
			xmlInput: "<lastmod>2023-06-15T10:30:05.123456Z</lastmod>",
			want:     time.Date(2023, 6, 15, 10, 30, 5, 123456000, time.UTC),
			wantErr:  false,
		},
		{
			name:     "RFC3339",
			xmlInput: "<lastmod>2023-06-15T10:30:05+02:00</lastmod>",
			want:     time.Date(2023, 6, 15, 10, 30, 5, 0, time.FixedZone("", 2*60*60)),
			wantErr:  false,
		},
		{
			name:     "With whitespace",
			xmlInput: "<lastmod> 2023-06-15 </lastmod>",
			want:     time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
			wantErr:  false,
		},
		{
			name:     "Invalid format",
			xmlInput: "<lastmod>invalid-date</lastmod>",
			want:     time.Time{},
			wantErr:  true,
		},
		{
			name:     "Empty input",
			xmlInput: "<lastmod>",
			want:     time.Time{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := xml.NewDecoder(strings.NewReader(tt.xmlInput))

			token, err := decoder.Token()
			if err != nil {
				t.Fatalf("Failed to read XML token: %v\n", err)
			}
			startElement := token.(xml.StartElement)

			var got lastModTime
			err = got.UnmarshalXML(decoder, startElement)

			if (err != nil) != tt.wantErr {
				t.Errorf("lastModTime.UnmarshalXML() error = %v, expected error: %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				gotTime := got.Time
				if !gotTime.Equal(tt.want) {
					t.Errorf("lastModTime.UnmarshalXML() = %v, expected value: %v", gotTime, tt.want)
				}
			}
		})
	}
}

//func Test_zip(t *testing.T) {
//	tests := []struct {
//		name     string
//		input    []byte
//		output   []byte
//		hasError bool
//	}{
//		{
//			name:     "Valid content",
//			input:    []byte("hello world"),
//			output:   gzipByte("hello world"),
//			hasError: false,
//		},
//		{
//			name:     "Empty content",
//			input:    []byte(""),
//			output:   gzipByte(""),
//			hasError: false,
//		},
//		{
//			name:     "Nil content",
//			input:    nil,
//			output:   gzipByte(""),
//			hasError: false,
//		},
//	}
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			compressed, err := zip(test.input, nil)
//
//			if (err != nil) != test.hasError {
//				t.Errorf("expected %v, got %v", test.hasError, err)
//			}
//
//			if !bytes.Equal(compressed, test.output) {
//				t.Errorf("expected %v, got %v", test.output, compressed)
//			}
//
//		})
//	}
//}

func configsEqual(c1, c2 config) bool {
	return c1.fetchTimeout == c2.fetchTimeout &&
		c1.userAgent == c2.userAgent &&
		c1.maxResponseSize == c2.maxResponseSize &&
		c1.maxDepth == c2.maxDepth &&
		c1.multiThread == c2.multiThread &&
		reflect.DeepEqual(c1.follow, c2.follow) &&
		reflect.DeepEqual(c1.rules, c2.rules)
}

func pointerOfString(str string) *string {
	return &str
}

func pointerOfFloat32(number float32) *float32 {
	return &number
}

func pointerOfTime(t time.Time) *time.Time {
	return &t
}

func pointerOfLastModTime(lmt lastModTime) *lastModTime {
	return &lmt
}

func pointerOfURLChangeFreq(changeFreq URLChangeFreq) *URLChangeFreq {
	return &changeFreq
}

func compareSitemapLocationsArray(sitemapSitemapLocations []string, testSitemapLocations []string) bool {
	if len(sitemapSitemapLocations) != len(testSitemapLocations) {
		return false
	}

	sort.Slice(sitemapSitemapLocations, func(i, j int) bool {
		return sitemapSitemapLocations[i] < sitemapSitemapLocations[j]
	})

	sort.Slice(testSitemapLocations, func(i, j int) bool {
		return testSitemapLocations[i] < testSitemapLocations[j]
	})

	return reflect.DeepEqual(sitemapSitemapLocations, testSitemapLocations)
}

func compareURLsArray(sitemapURLs []URL, testCaseURLs []URL) bool {
	if len(sitemapURLs) != len(testCaseURLs) {
		return false
	}

	sort.Slice(sitemapURLs, func(i, j int) bool {
		return sitemapURLs[i].Loc < sitemapURLs[j].Loc
	})

	for i, sitemapURL := range sitemapURLs {
		if sitemapURL.Loc != testCaseURLs[i].Loc {
			return false
		}
		if sitemapURL.LastMod.Unix() != testCaseURLs[i].LastMod.Unix() {
			return false
		}
		if *sitemapURL.ChangeFreq != *testCaseURLs[i].ChangeFreq {
			return false
		}
		if *sitemapURL.Priority != *testCaseURLs[i].Priority {
			return false
		}
	}
	return true
}

func gzipByte(s string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(s)); err != nil {
		panic(err)
	}
	if err := gz.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

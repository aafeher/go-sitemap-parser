package sitemap

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
				maxConcurrency:  defaultMaxConcurrency,
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

	t.Run("ZeroValue", func(t *testing.T) {
		s := New()
		defaultSize := s.cfg.maxResponseSize
		s.SetMaxResponseSize(0)
		if s.cfg.maxResponseSize != defaultSize {
			t.Errorf("expected default %v to be preserved, got %v", defaultSize, s.cfg.maxResponseSize)
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})

	t.Run("NegativeValue", func(t *testing.T) {
		s := New()
		defaultSize := s.cfg.maxResponseSize
		s.SetMaxResponseSize(-1)
		if s.cfg.maxResponseSize != defaultSize {
			t.Errorf("expected default %v to be preserved, got %v", defaultSize, s.cfg.maxResponseSize)
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})
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

	t.Run("ZeroValue", func(t *testing.T) {
		s := New()
		defaultDepth := s.cfg.maxDepth
		s.SetMaxDepth(0)
		if s.cfg.maxDepth != defaultDepth {
			t.Errorf("expected default %v to be preserved, got %v", defaultDepth, s.cfg.maxDepth)
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})

	t.Run("NegativeValue", func(t *testing.T) {
		s := New()
		defaultDepth := s.cfg.maxDepth
		s.SetMaxDepth(-5)
		if s.cfg.maxDepth != defaultDepth {
			t.Errorf("expected default %v to be preserved, got %v", defaultDepth, s.cfg.maxDepth)
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})
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

	t.Run("pattern at max length is accepted", func(t *testing.T) {
		s := New()
		pattern := strings.Repeat("a", maxRegexPatternLength)
		s.SetFollow([]string{pattern})
		if len(s.cfg.followRegexes) != 1 {
			t.Errorf("expected 1 regex, got %d", len(s.cfg.followRegexes))
		}
		if len(s.errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(s.errs))
		}
	})

	t.Run("pattern exceeding max length is rejected", func(t *testing.T) {
		s := New()
		pattern := strings.Repeat("a", maxRegexPatternLength+1)
		s.SetFollow([]string{pattern})
		if len(s.cfg.followRegexes) != 0 {
			t.Errorf("expected 0 regexes, got %d", len(s.cfg.followRegexes))
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})

	t.Run("valid and oversized patterns: only valid compiled", func(t *testing.T) {
		s := New()
		long := strings.Repeat("a", maxRegexPatternLength+1)
		s.SetFollow([]string{`alpha`, long, `beta`})
		if len(s.cfg.followRegexes) != 2 {
			t.Errorf("expected 2 regexes, got %d", len(s.cfg.followRegexes))
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

	t.Run("pattern at max length is accepted", func(t *testing.T) {
		s := New()
		pattern := strings.Repeat("a", maxRegexPatternLength)
		s.SetRules([]string{pattern})
		if len(s.cfg.rulesRegexes) != 1 {
			t.Errorf("expected 1 regex, got %d", len(s.cfg.rulesRegexes))
		}
		if len(s.errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(s.errs))
		}
	})

	t.Run("pattern exceeding max length is rejected", func(t *testing.T) {
		s := New()
		pattern := strings.Repeat("a", maxRegexPatternLength+1)
		s.SetRules([]string{pattern})
		if len(s.cfg.rulesRegexes) != 0 {
			t.Errorf("expected 0 regexes, got %d", len(s.cfg.rulesRegexes))
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})

	t.Run("valid and oversized patterns: only valid compiled", func(t *testing.T) {
		s := New()
		long := strings.Repeat("a", maxRegexPatternLength+1)
		s.SetRules([]string{`page`, long, `post`})
		if len(s.cfg.rulesRegexes) != 2 {
			t.Errorf("expected 2 regexes, got %d", len(s.cfg.rulesRegexes))
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

func TestS_SetHTTPClient(t *testing.T) {
	t.Run("default is nil", func(t *testing.T) {
		s := New()
		if s.cfg.httpClient != nil {
			t.Error("expected httpClient to be nil by default")
		}
	})

	t.Run("stores custom client", func(t *testing.T) {
		s := New()
		custom := &http.Client{}
		result := s.SetHTTPClient(custom)
		if s.cfg.httpClient != custom {
			t.Error("expected custom client to be stored in config")
		}
		if result != s {
			t.Error("expected method chaining to return same instance")
		}
	})

	t.Run("nil resets to default", func(t *testing.T) {
		s := New()
		s.SetHTTPClient(&http.Client{})
		s.SetHTTPClient(nil)
		if s.cfg.httpClient != nil {
			t.Error("expected httpClient to be nil after reset")
		}
	})

	t.Run("custom client is used for fetching", func(t *testing.T) {
		sitemap := `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>http://example.com/page</loc></url></urlset>`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sitemap)
		}))
		defer server.Close()

		called := false
		transport := &recordingTransport{
			delegate: http.DefaultTransport,
			called:   &called,
		}
		customClient := &http.Client{Transport: transport}

		s := New()
		s.SetHTTPClient(customClient)
		_, err := s.Parse(server.URL+"/sitemap.xml", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("expected custom HTTP client to be used for fetching")
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
	})

	t.Run("fetchTimeout ignored when custom client set", func(t *testing.T) {
		// The custom client has a 1ms timeout; if fetchTimeout were applied instead,
		// the server sleep would not cause a timeout error.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></urlset>`)
		}))
		defer server.Close()

		customClient := &http.Client{Timeout: 1 * time.Millisecond}

		s := New().SetFetchTimeout(60).SetHTTPClient(customClient)
		_, err := s.Parse(server.URL+"/sitemap.xml", nil)
		if err == nil {
			t.Error("expected timeout error from custom client, got nil")
		}
	})
}

// recordingTransport is an http.RoundTripper that records whether it was called
// and delegates all requests to the underlying transport.
type recordingTransport struct {
	delegate http.RoundTripper
	called   *bool
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*rt.called = true
	return rt.delegate.RoundTrip(req)
}

func TestImage_validateAndFilterImages(t *testing.T) {
	t.Run("empty input returns empty", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterImages(nil)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})

	t.Run("tolerant: valid image kept", func(t *testing.T) {
		s := New()
		imgs := []Image{{Loc: "https://example.com/photo.jpg", Title: "T"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 1 {
			t.Errorf("expected 1 image, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})

	t.Run("tolerant: empty loc silently dropped", func(t *testing.T) {
		s := New()
		imgs := []Image{{Loc: ""}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors for empty loc in tolerant mode, got %d", len(errs))
		}
	})

	t.Run("tolerant: loc exceeding max length rejected with error", func(t *testing.T) {
		s := New()
		imgs := []Image{{Loc: "http://example.com/" + strings.Repeat("a", maxLocLength)}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})

	t.Run("tolerant: non-HTTP scheme accepted", func(t *testing.T) {
		s := New()
		imgs := []Image{{Loc: "ftp://example.com/photo.jpg"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 1 {
			t.Errorf("expected 1 image in tolerant mode, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", len(errs))
		}
	})

	t.Run("tolerant: multiple images, one empty loc dropped", func(t *testing.T) {
		s := New()
		imgs := []Image{
			{Loc: "https://example.com/a.jpg"},
			{Loc: ""},
			{Loc: "https://example.com/b.jpg"},
		}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 2 {
			t.Errorf("expected 2 images, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})

	t.Run("strict: valid HTTP image kept", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "http://example.com/photo.jpg"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 1 {
			t.Errorf("expected 1 image, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})

	t.Run("strict: valid HTTPS image kept", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "https://cdn.example.com/photo.jpg"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 1 {
			t.Errorf("expected 1 image, got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})

	t.Run("strict: empty loc produces error and is dropped", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: ""}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})

	t.Run("strict: non-HTTP scheme rejected", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "ftp://example.com/photo.jpg"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})

	t.Run("strict: loc exceeding max length rejected", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "https://example.com/" + strings.Repeat("a", maxLocLength)}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})

	t.Run("strict: unparseable URL rejected with error", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "http://example.com/path%zzinvalid"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 0 {
			t.Errorf("expected 0 images, got %d", len(got))
		}
		if len(errs) != 1 {
			t.Errorf("expected 1 error for unparseable URL, got %d", len(errs))
		}
	})

	t.Run("strict: CDN host (different from page host) accepted", func(t *testing.T) {
		s := New().SetStrict(true)
		imgs := []Image{{Loc: "https://cdn.other-host.com/photo.jpg"}}
		got, errs := s.validateAndFilterImages(imgs)
		if len(got) != 1 {
			t.Errorf("expected 1 image (CDN host allowed in strict mode), got %d", len(got))
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d", len(errs))
		}
	})
}

func TestImage_parseURLSet_WithImages(t *testing.T) {
	t.Run("URL with two images, first has all fields", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
    <url>
        <loc>https://example.com/page</loc>
        <image:image>
            <image:loc>https://example.com/photo1.jpg</image:loc>
            <image:title>First photo</image:title>
            <image:caption>A caption</image:caption>
            <image:geo_location>Budapest, Hungary</image:geo_location>
            <image:license>https://creativecommons.org/licenses/by/4.0/</image:license>
        </image:image>
        <image:image>
            <image:loc>https://example.com/photo2.jpg</image:loc>
        </image:image>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL) != 1 {
			t.Fatalf("expected 1 URL, got %d", len(urlSet.URL))
		}
		u := urlSet.URL[0]
		if len(u.Images) != 2 {
			t.Fatalf("expected 2 images, got %d", len(u.Images))
		}
		img := u.Images[0]
		if img.Loc != "https://example.com/photo1.jpg" {
			t.Errorf("expected photo1.jpg loc, got %q", img.Loc)
		}
		if img.Title != "First photo" {
			t.Errorf("expected title %q, got %q", "First photo", img.Title)
		}
		if img.Caption != "A caption" {
			t.Errorf("expected caption %q, got %q", "A caption", img.Caption)
		}
		if img.GeoLocation != "Budapest, Hungary" {
			t.Errorf("expected geo_location %q, got %q", "Budapest, Hungary", img.GeoLocation)
		}
		if img.License != "https://creativecommons.org/licenses/by/4.0/" {
			t.Errorf("expected license %q, got %q", "https://creativecommons.org/licenses/by/4.0/", img.License)
		}
		if u.Images[1].Loc != "https://example.com/photo2.jpg" {
			t.Errorf("expected photo2.jpg, got %q", u.Images[1].Loc)
		}
		if u.Images[1].Title != "" || u.Images[1].Caption != "" {
			t.Errorf("expected empty optional fields on second image")
		}
	})

	t.Run("URL without images has nil Images slice", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page</loc></url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL[0].Images) != 0 {
			t.Errorf("expected 0 images, got %d", len(urlSet.URL[0].Images))
		}
	})

	t.Run("image element without namespace is ignored", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url>
        <loc>https://example.com/page</loc>
        <image><loc>https://example.com/photo.jpg</loc></image>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL[0].Images) != 0 {
			t.Errorf("expected 0 images (no namespace), got %d", len(urlSet.URL[0].Images))
		}
	})

	t.Run("multiple URLs with mixed image presence", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
    <url>
        <loc>https://example.com/with-image</loc>
        <image:image><image:loc>https://example.com/photo.jpg</image:loc></image:image>
    </url>
    <url>
        <loc>https://example.com/without-image</loc>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL) != 2 {
			t.Fatalf("expected 2 URLs, got %d", len(urlSet.URL))
		}
		if len(urlSet.URL[0].Images) != 1 {
			t.Errorf("expected 1 image on first URL, got %d", len(urlSet.URL[0].Images))
		}
		if len(urlSet.URL[1].Images) != 0 {
			t.Errorf("expected 0 images on second URL, got %d", len(urlSet.URL[1].Images))
		}
	})
}

func TestImage_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with images parses correctly", func(t *testing.T) {
		s := New()
		_, err := s.Parse(server.URL+"/sitemap-image-01.xml", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Fatalf("expected 2 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}

		urls := s.GetURLs()
		var pageWithImages, pageWithout URL
		for _, u := range urls {
			if strings.HasSuffix(u.Loc, "/page-with-images") {
				pageWithImages = u
			} else {
				pageWithout = u
			}
		}

		if len(pageWithImages.Images) != 2 {
			t.Fatalf("expected 2 images on page-with-images, got %d", len(pageWithImages.Images))
		}
		img := pageWithImages.Images[0]
		if !strings.HasSuffix(img.Loc, "/photo1.jpg") {
			t.Errorf("unexpected image loc: %q", img.Loc)
		}
		if img.Title != "First photo" {
			t.Errorf("expected title %q, got %q", "First photo", img.Title)
		}
		if img.Caption != "A caption" {
			t.Errorf("expected caption %q, got %q", "A caption", img.Caption)
		}
		if img.GeoLocation != "Budapest, Hungary" {
			t.Errorf("expected geo_location %q, got %q", "Budapest, Hungary", img.GeoLocation)
		}
		if img.License != "https://creativecommons.org/licenses/by/4.0/" {
			t.Errorf("expected license URL, got %q", img.License)
		}
		if !strings.HasSuffix(pageWithImages.Images[1].Loc, "/photo2.jpg") {
			t.Errorf("unexpected second image loc: %q", pageWithImages.Images[1].Loc)
		}
		if len(pageWithout.Images) != 0 {
			t.Errorf("expected 0 images on page-without-images, got %d", len(pageWithout.Images))
		}
	})

	t.Run("tolerant: image with empty loc dropped silently", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
    <url>
        <loc>%s/page</loc>
        <image:image><image:loc></image:loc></image:image>
        <image:image><image:loc>%s/photo.jpg</image:loc></image:image>
    </url>
</urlset>`, server.URL, server.URL)
		sitemapURL := server.URL + "/sitemap.xml"
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		urls := s.GetURLs()
		if len(urls) != 1 {
			t.Fatalf("expected 1 URL, got %d", len(urls))
		}
		if len(urls[0].Images) != 1 {
			t.Errorf("expected 1 valid image (empty loc dropped), got %d", len(urls[0].Images))
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict: image with empty loc produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
    <url>
        <loc>%s/page</loc>
        <image:image><image:loc></image:loc></image:image>
    </url>
</urlset>`, server.URL)
		sitemapURL := server.URL + "/sitemap.xml"
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 1 {
			t.Errorf("expected 1 error for empty image loc in strict mode, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict: image with invalid scheme produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
    <url>
        <loc>%s/page</loc>
        <image:image><image:loc>ftp://example.com/photo.jpg</image:loc></image:image>
    </url>
</urlset>`, server.URL)
		sitemapURL := server.URL + "/sitemap.xml"
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 1 {
			t.Errorf("expected 1 error for ftp image loc in strict mode, got %d", s.GetErrorsCount())
		}
		if len(s.GetURLs()[0].Images) != 0 {
			t.Errorf("expected image to be dropped, got %d", len(s.GetURLs()[0].Images))
		}
	})
}

func TestNews_validateNews(t *testing.T) {
	makeDate := func(s string) *lastModTime {
		lmt := &lastModTime{}
		tok := xml.NewDecoder(strings.NewReader("<d>" + s + "</d>"))
		start, _ := tok.Token()
		_ = lmt.UnmarshalXML(tok, start.(xml.StartElement))
		return lmt
	}

	t.Run("nil input returns nil", func(t *testing.T) {
		s := New()
		got, errs := s.validateNews("", nil)
		if got != nil || len(errs) != 0 {
			t.Errorf("expected nil, nil for nil input")
		}
	})

	t.Run("tolerant: valid news kept without errors", func(t *testing.T) {
		s := New()
		n := &News{
			Publication:     NewsPublication{Name: "Example", Language: "en"},
			PublicationDate: makeDate("2026-05-03"),
			Title:           "Article",
		}
		got, errs := s.validateNews("https://example.com/page", n)
		if got != n {
			t.Error("expected same news pointer")
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", len(errs))
		}
	})

	t.Run("tolerant: missing fields produce no errors", func(t *testing.T) {
		s := New()
		n := &News{}
		got, errs := s.validateNews("https://example.com/page", n)
		if got != n {
			t.Error("expected same news pointer")
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", len(errs))
		}
	})

	t.Run("strict: fully valid news kept without errors", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{
			Publication:     NewsPublication{Name: "Example", Language: "en"},
			PublicationDate: makeDate("2026-05-03T10:00:00Z"),
			Title:           "Article Title",
		}
		got, errs := s.validateNews("https://example.com/page", n)
		if got != n {
			t.Error("expected same news pointer")
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors for valid news, got %d: %v", len(errs), errs)
		}
	})

	t.Run("strict: empty title produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{
			Publication:     NewsPublication{Name: "Example", Language: "en"},
			PublicationDate: makeDate("2026-05-03"),
			Title:           "",
		}
		_, errs := s.validateNews("https://example.com/page", n)
		if len(errs) != 1 {
			t.Errorf("expected 1 error for empty title, got %d", len(errs))
		}
	})

	t.Run("strict: empty publication name produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{
			Publication:     NewsPublication{Name: "", Language: "en"},
			PublicationDate: makeDate("2026-05-03"),
			Title:           "Article",
		}
		_, errs := s.validateNews("https://example.com/page", n)
		if len(errs) != 1 {
			t.Errorf("expected 1 error for empty publication name, got %d", len(errs))
		}
	})

	t.Run("strict: empty publication language produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{
			Publication:     NewsPublication{Name: "Example", Language: ""},
			PublicationDate: makeDate("2026-05-03"),
			Title:           "Article",
		}
		_, errs := s.validateNews("https://example.com/page", n)
		if len(errs) != 1 {
			t.Errorf("expected 1 error for empty publication language, got %d", len(errs))
		}
	})

	t.Run("strict: nil publication date produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{
			Publication:     NewsPublication{Name: "Example", Language: "en"},
			PublicationDate: nil,
			Title:           "Article",
		}
		_, errs := s.validateNews("https://example.com/page", n)
		if len(errs) != 1 {
			t.Errorf("expected 1 error for nil publication_date, got %d", len(errs))
		}
	})

	t.Run("strict: all required fields missing produces four errors", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{}
		got, errs := s.validateNews("https://example.com/page", n)
		if got != n {
			t.Error("expected news entry to be kept despite errors")
		}
		if len(errs) != 4 {
			t.Errorf("expected 4 errors (title, name, language, date), got %d", len(errs))
		}
	})
}

func TestNews_parseURLSet_WithNews(t *testing.T) {
	t.Run("URL with full news entry", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
    <url>
        <loc>https://example.com/article</loc>
        <news:news>
            <news:publication>
                <news:name>Example News</news:name>
                <news:language>en</news:language>
            </news:publication>
            <news:publication_date>2026-05-03T10:00:00Z</news:publication_date>
            <news:title>Breaking: Example Article</news:title>
        </news:news>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u := urlSet.URL[0]
		if u.News == nil {
			t.Fatal("expected News to be non-nil")
		}
		if u.News.Title != "Breaking: Example Article" {
			t.Errorf("expected title %q, got %q", "Breaking: Example Article", u.News.Title)
		}
		if u.News.Publication.Name != "Example News" {
			t.Errorf("expected publication name %q, got %q", "Example News", u.News.Publication.Name)
		}
		if u.News.Publication.Language != "en" {
			t.Errorf("expected language %q, got %q", "en", u.News.Publication.Language)
		}
		if u.News.PublicationDate == nil {
			t.Error("expected PublicationDate to be non-nil")
		}
	})

	t.Run("URL without news has nil News field", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page</loc></url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if urlSet.URL[0].News != nil {
			t.Errorf("expected nil News, got non-nil")
		}
	})

	t.Run("news element without namespace is ignored", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url>
        <loc>https://example.com/page</loc>
        <news><title>Ignored</title></news>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if urlSet.URL[0].News != nil {
			t.Errorf("expected nil News (no namespace), got non-nil")
		}
	})

	t.Run("multiple URLs with mixed news presence", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
    <url>
        <loc>https://example.com/article</loc>
        <news:news>
            <news:publication><news:name>N</news:name><news:language>hu</news:language></news:publication>
            <news:publication_date>2026-05-03</news:publication_date>
            <news:title>Article</news:title>
        </news:news>
    </url>
    <url>
        <loc>https://example.com/page</loc>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if urlSet.URL[0].News == nil {
			t.Error("expected News on first URL")
		}
		if urlSet.URL[1].News != nil {
			t.Error("expected nil News on second URL")
		}
	})
}

func TestNews_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with news parses correctly", func(t *testing.T) {
		s := New()
		_, err := s.Parse(server.URL+"/sitemap-news-01.xml", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Fatalf("expected 2 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}

		urls := s.GetURLs()
		var article, plain URL
		for _, u := range urls {
			if strings.HasSuffix(u.Loc, "/article-1") {
				article = u
			} else {
				plain = u
			}
		}

		if article.News == nil {
			t.Fatal("expected News on article URL")
		}
		if article.News.Title != "Breaking: Example Article" {
			t.Errorf("unexpected title: %q", article.News.Title)
		}
		if article.News.Publication.Name != "Example News" {
			t.Errorf("unexpected publication name: %q", article.News.Publication.Name)
		}
		if article.News.Publication.Language != "en" {
			t.Errorf("unexpected language: %q", article.News.Publication.Language)
		}
		if article.News.PublicationDate == nil {
			t.Error("expected non-nil PublicationDate")
		}
		if plain.News != nil {
			t.Errorf("expected nil News on plain URL")
		}
	})

	t.Run("strict: all required fields present — no errors", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
    <url>
        <loc>%s/article</loc>
        <news:news>
            <news:publication>
                <news:name>Example</news:name>
                <news:language>en</news:language>
            </news:publication>
            <news:publication_date>2026-05-03T10:00:00Z</news:publication_date>
            <news:title>Article</news:title>
        </news:news>
    </url>
</urlset>`, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}
		if s.GetURLs()[0].News == nil {
			t.Error("expected News to be set")
		}
	})

	t.Run("strict: missing required fields produce errors, news entry kept", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
    <url>
        <loc>%s/article</loc>
        <news:news></news:news>
    </url>
</urlset>`, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 4 {
			t.Errorf("expected 4 errors (title, name, language, date), got %d", s.GetErrorsCount())
		}
		urls := s.GetURLs()
		if len(urls) != 1 {
			t.Errorf("expected URL to be kept despite errors, got %d URLs", len(urls))
		}
		if urls[0].News == nil {
			t.Error("expected News entry to be kept despite missing fields")
		}
	})

	t.Run("tolerant: missing fields produce no errors", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
    <url>
        <loc>%s/article</loc>
        <news:news><news:title>Only Title</news:title></news:news>
    </url>
</urlset>`, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", s.GetErrorsCount())
		}
		urls := s.GetURLs()
		if len(urls) != 1 || urls[0].News == nil {
			t.Error("expected URL with News in tolerant mode")
		}
		if urls[0].News.Title != "Only Title" {
			t.Errorf("expected title %q, got %q", "Only Title", urls[0].News.Title)
		}
	})
}

func pointerOfInt(i int) *int                  { return &i }
func pointerOfFloat32Video(f float32) *float32 { return &f }

// compareErrorStrings compares two error slices by their Error() strings.
// It is used in place of reflect.DeepEqual for error slices so that typed
// error wrappers (e.g. *ValidationError, *NetworkError) can be compared with
// plain fmt.Errorf expectations as long as their Error() output matches.
func compareErrorStrings(got, want []error) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Error() != want[i].Error() {
			return false
		}
	}
	return true
}

func TestVideo_validateAndFilterVideos(t *testing.T) {
	t.Run("empty input returns empty", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterVideos(nil)
		if len(got) != 0 || len(errs) != 0 {
			t.Errorf("expected empty result for nil input")
		}
	})

	t.Run("tolerant: valid video kept", func(t *testing.T) {
		s := New()
		videos := []Video{{ThumbnailLoc: "https://example.com/thumb.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4"}}
		got, errs := s.validateAndFilterVideos(videos)
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 video, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("tolerant: empty ThumbnailLoc silently dropped", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: ""}})
		if len(got) != 0 || len(errs) != 0 {
			t.Errorf("expected 0 videos, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("tolerant: ThumbnailLoc exceeding max length rejected with error", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/" + strings.Repeat("a", maxLocLength)}})
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 videos, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("tolerant: non-HTTP scheme accepted", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "ftp://example.com/thumb.jpg"}})
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 video, 0 errors in tolerant mode; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("tolerant: multiple videos one without ThumbnailLoc dropped", func(t *testing.T) {
		s := New()
		videos := []Video{
			{ThumbnailLoc: "https://example.com/a.jpg"},
			{ThumbnailLoc: ""},
			{ThumbnailLoc: "https://example.com/b.jpg"},
		}
		got, errs := s.validateAndFilterVideos(videos)
		if len(got) != 2 || len(errs) != 0 {
			t.Errorf("expected 2 videos, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: valid video kept without errors", func(t *testing.T) {
		s := New().SetStrict(true)
		dur := 300
		rat := float32(4.5)
		videos := []Video{{
			ThumbnailLoc: "https://example.com/thumb.jpg",
			Title:        "Title",
			Description:  "Description",
			ContentLoc:   "https://example.com/video.mp4",
			Duration:     &dur,
			Rating:       &rat,
			Tags:         []string{"a", "b"},
		}}
		got, errs := s.validateAndFilterVideos(videos)
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 video, 0 errors; got %d, %d: %v", len(got), len(errs), errs)
		}
	})

	t.Run("strict: empty ThumbnailLoc produces error and drops video", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: ""}})
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 videos, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: ThumbnailLoc exceeding max length rejected", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/" + strings.Repeat("a", maxLocLength)}})
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 videos, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: non-HTTP scheme rejected", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "ftp://example.com/thumb.jpg"}})
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 videos, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: unparseable ThumbnailLoc rejected", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/path%zzinvalid"}})
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 videos, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: empty title produces error, video kept", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "", Description: "D", ContentLoc: "https://example.com/v.mp4"}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: empty description produces error, video kept", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "", ContentLoc: "https://example.com/v.mp4"}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: no ContentLoc and no PlayerLoc produces error, video kept", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D"}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: PlayerLoc alone satisfies content requirement", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", PlayerLoc: "https://example.com/player"}})
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 video, 0 errors; got %d, %d: %v", len(got), len(errs), errs)
		}
	})

	t.Run("strict: Duration below 1 produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Duration: pointerOfInt(0)}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: Duration above 28800 produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Duration: pointerOfInt(maxVideoDuration + 1)}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: Rating below 0 produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Rating: pointerOfFloat32Video(-0.1)}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: Rating above 5.0 produces error", func(t *testing.T) {
		s := New().SetStrict(true)
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Rating: pointerOfFloat32Video(5.1)}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict: Tags exceeding 32 produces error, video kept", func(t *testing.T) {
		s := New().SetStrict(true)
		tags := make([]string, maxVideoTags+1)
		for i := range tags {
			tags[i] = fmt.Sprintf("tag%d", i)
		}
		got, errs := s.validateAndFilterVideos([]Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Tags: tags}})
		if len(got) != 1 || len(errs) != 1 {
			t.Errorf("expected 1 video, 1 error; got %d, %d", len(got), len(errs))
		}
	})
}

func TestHreflang_validateAndFilterHreflangs(t *testing.T) {
	t.Run("nil or empty", func(t *testing.T) {
		s := New()
		got, errs := s.validateAndFilterHreflangs(nil)
		if len(got) != 0 || len(errs) != 0 {
			t.Errorf("expected 0 links, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("tolerant mode: drop empty href", func(t *testing.T) {
		s := New()
		links := []AlternateLink{{Href: ""}, {Href: "http://example.com/"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 link, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("both modes: reject oversized href", func(t *testing.T) {
		s := New()
		links := []AlternateLink{{Href: "http://example.com/" + strings.Repeat("a", maxLocLength)}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: valid link", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "http://example.com/"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 1 || len(errs) != 0 {
			t.Errorf("expected 1 link, 0 errors; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: reject empty href", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Href: ""}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: reject invalid rel", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Rel: "canonical", Hreflang: "en", Href: "http://example.com/"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: reject empty hreflang", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Rel: "alternate", Hreflang: "", Href: "http://example.com/"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: reject invalid URL", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "http://example.com/%%invalid"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})

	t.Run("strict mode: reject unsupported scheme", func(t *testing.T) {
		s := New().SetStrict(true)
		links := []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "ftp://example.com/"}}
		got, errs := s.validateAndFilterHreflangs(links)
		if len(got) != 0 || len(errs) != 1 {
			t.Errorf("expected 0 links, 1 error; got %d, %d", len(got), len(errs))
		}
	})
}

func TestHreflang_parseURLSet_WithHreflang(t *testing.T) {
	t.Run("URL with hreflang entries", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
    <url>
        <loc>http://www.example.com/english/page.html</loc>
        <xhtml:link rel="alternate" hreflang="de" href="http://www.example.com/deutsch/page.html"/>
        <xhtml:link rel="alternate" hreflang="en" href="http://www.example.com/english/page.html"/>
    </url>
</urlset>`
		s := New()
		_, err := s.Parse("http://www.example.com/sitemap.xml", &data)
		if err != nil {
			t.Fatal(err)
		}
		urls := s.GetURLs()
		if len(urls) != 1 {
			t.Fatalf("expected 1 URL, got %d", len(urls))
		}
		if len(urls[0].Hreflangs) != 2 {
			t.Errorf("expected 2 hreflangs, got %d", len(urls[0].Hreflangs))
		}
	})
}

func TestVideo_parseURLSet_WithVideos(t *testing.T) {
	t.Run("URL with full video entry", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>https://example.com/video-page</loc>
        <video:video>
            <video:thumbnail_loc>https://example.com/thumb.jpg</video:thumbnail_loc>
            <video:title>Example Video</video:title>
            <video:description>A description</video:description>
            <video:content_loc>https://example.com/video.mp4</video:content_loc>
            <video:player_loc>https://example.com/player</video:player_loc>
            <video:duration>600</video:duration>
            <video:rating>4.5</video:rating>
            <video:view_count>1000</video:view_count>
            <video:family_friendly>yes</video:family_friendly>
            <video:restriction relationship="allow">HU AT</video:restriction>
            <video:platform relationship="allow">web mobile</video:platform>
            <video:requires_subscription>no</video:requires_subscription>
            <video:uploader info="https://example.com/uploader">Channel</video:uploader>
            <video:live>no</video:live>
            <video:tag>golang</video:tag>
            <video:tag>sitemap</video:tag>
        </video:video>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u := urlSet.URL[0]
		if len(u.Videos) != 1 {
			t.Fatalf("expected 1 video, got %d", len(u.Videos))
		}
		v := u.Videos[0]
		if v.ThumbnailLoc != "https://example.com/thumb.jpg" {
			t.Errorf("unexpected ThumbnailLoc: %q", v.ThumbnailLoc)
		}
		if v.Title != "Example Video" {
			t.Errorf("unexpected Title: %q", v.Title)
		}
		if v.Description != "A description" {
			t.Errorf("unexpected Description: %q", v.Description)
		}
		if v.ContentLoc != "https://example.com/video.mp4" {
			t.Errorf("unexpected ContentLoc: %q", v.ContentLoc)
		}
		if v.PlayerLoc != "https://example.com/player" {
			t.Errorf("unexpected PlayerLoc: %q", v.PlayerLoc)
		}
		if v.Duration == nil || *v.Duration != 600 {
			t.Errorf("unexpected Duration: %v", v.Duration)
		}
		if v.Rating == nil || *v.Rating != 4.5 {
			t.Errorf("unexpected Rating: %v", v.Rating)
		}
		if v.ViewCount == nil || *v.ViewCount != 1000 {
			t.Errorf("unexpected ViewCount: %v", v.ViewCount)
		}
		if v.FamilyFriendly != "yes" {
			t.Errorf("unexpected FamilyFriendly: %q", v.FamilyFriendly)
		}
		if v.Restriction == nil || v.Restriction.Relationship != "allow" || v.Restriction.Value != "HU AT" {
			t.Errorf("unexpected Restriction: %v", v.Restriction)
		}
		if v.Platform == nil || v.Platform.Relationship != "allow" || v.Platform.Value != "web mobile" {
			t.Errorf("unexpected Platform: %v", v.Platform)
		}
		if v.RequiresSubscription != "no" {
			t.Errorf("unexpected RequiresSubscription: %q", v.RequiresSubscription)
		}
		if v.Uploader == nil || v.Uploader.Value != "Channel" || v.Uploader.Info != "https://example.com/uploader" {
			t.Errorf("unexpected Uploader: %v", v.Uploader)
		}
		if v.Live != "no" {
			t.Errorf("unexpected Live: %q", v.Live)
		}
		if len(v.Tags) != 2 || v.Tags[0] != "golang" || v.Tags[1] != "sitemap" {
			t.Errorf("unexpected Tags: %v", v.Tags)
		}
	})

	t.Run("URL without video has nil Videos slice", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page</loc></url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL[0].Videos) != 0 {
			t.Errorf("expected 0 videos, got %d", len(urlSet.URL[0].Videos))
		}
	})

	t.Run("video element without namespace is ignored", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url>
        <loc>https://example.com/page</loc>
        <video><thumbnail_loc>https://example.com/thumb.jpg</thumbnail_loc></video>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL[0].Videos) != 0 {
			t.Errorf("expected 0 videos (no namespace), got %d", len(urlSet.URL[0].Videos))
		}
	})

	t.Run("multiple URLs with mixed video presence", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>https://example.com/with-video</loc>
        <video:video><video:thumbnail_loc>https://example.com/t.jpg</video:thumbnail_loc></video:video>
    </url>
    <url>
        <loc>https://example.com/without-video</loc>
    </url>
</urlset>`
		s := New()
		urlSet, err := s.parseURLSet(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(urlSet.URL[0].Videos) != 1 {
			t.Errorf("expected 1 video on first URL, got %d", len(urlSet.URL[0].Videos))
		}
		if len(urlSet.URL[1].Videos) != 0 {
			t.Errorf("expected 0 videos on second URL, got %d", len(urlSet.URL[1].Videos))
		}
	})
}

func TestVideo_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with video parses correctly", func(t *testing.T) {
		s := New()
		_, err := s.Parse(server.URL+"/sitemap-video-01.xml", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Fatalf("expected 2 URLs, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}

		urls := s.GetURLs()
		var videoPage, plain URL
		for _, u := range urls {
			if strings.HasSuffix(u.Loc, "/video-page") {
				videoPage = u
			} else {
				plain = u
			}
		}

		if len(videoPage.Videos) != 1 {
			t.Fatalf("expected 1 video on video-page, got %d", len(videoPage.Videos))
		}
		v := videoPage.Videos[0]
		if !strings.HasSuffix(v.ThumbnailLoc, "/thumb.jpg") {
			t.Errorf("unexpected ThumbnailLoc: %q", v.ThumbnailLoc)
		}
		if v.Title != "Example Video" {
			t.Errorf("unexpected Title: %q", v.Title)
		}
		if v.Duration == nil || *v.Duration != 600 {
			t.Errorf("unexpected Duration: %v", v.Duration)
		}
		if v.Rating == nil || *v.Rating != 4.5 {
			t.Errorf("unexpected Rating: %v", v.Rating)
		}
		if v.ViewCount == nil || *v.ViewCount != 1000 {
			t.Errorf("unexpected ViewCount: %v", v.ViewCount)
		}
		if v.Restriction == nil || v.Restriction.Relationship != "allow" {
			t.Errorf("unexpected Restriction: %v", v.Restriction)
		}
		if v.Platform == nil || v.Platform.Relationship != "allow" {
			t.Errorf("unexpected Platform: %v", v.Platform)
		}
		if v.Uploader == nil || v.Uploader.Value != "ExampleChannel" {
			t.Errorf("unexpected Uploader: %v", v.Uploader)
		}
		if len(v.Tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(v.Tags))
		}
		if len(plain.Videos) != 0 {
			t.Errorf("expected 0 videos on plain page")
		}
	})

	t.Run("strict: valid video produces no errors", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>%s/page</loc>
        <video:video>
            <video:thumbnail_loc>%s/thumb.jpg</video:thumbnail_loc>
            <video:title>Title</video:title>
            <video:description>Description</video:description>
            <video:content_loc>%s/video.mp4</video:content_loc>
        </video:video>
    </url>
</urlset>`, server.URL, server.URL, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}
		if len(s.GetURLs()[0].Videos) != 1 {
			t.Errorf("expected 1 video, got %d", len(s.GetURLs()[0].Videos))
		}
	})

	t.Run("tolerant: video with only ThumbnailLoc kept without errors", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>%s/page</loc>
        <video:video>
            <video:thumbnail_loc>%s/thumb.jpg</video:thumbnail_loc>
        </video:video>
    </url>
</urlset>`, server.URL, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", s.GetErrorsCount())
		}
		if len(s.GetURLs()[0].Videos) != 1 {
			t.Errorf("expected 1 video, got %d", len(s.GetURLs()[0].Videos))
		}
	})

	t.Run("strict: missing required fields produce errors, video kept", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>%s/page</loc>
        <video:video>
            <video:thumbnail_loc>%s/thumb.jpg</video:thumbnail_loc>
        </video:video>
    </url>
</urlset>`, server.URL, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// title, description, content_loc+player_loc = 3 errors
		if s.GetErrorsCount() != 3 {
			t.Errorf("expected 3 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}
		if len(s.GetURLs()[0].Videos) != 1 {
			t.Errorf("expected video to be kept despite errors, got %d", len(s.GetURLs()[0].Videos))
		}
	})

	t.Run("tolerant: empty ThumbnailLoc dropped silently", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
    <url>
        <loc>%s/page</loc>
        <video:video><video:thumbnail_loc></video:thumbnail_loc></video:video>
        <video:video><video:thumbnail_loc>%s/thumb.jpg</video:thumbnail_loc></video:video>
    </url>
</urlset>`, server.URL, server.URL)
		_, err := s.Parse(server.URL+"/sitemap.xml", &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d", s.GetErrorsCount())
		}
		if len(s.GetURLs()[0].Videos) != 1 {
			t.Errorf("expected 1 valid video (empty thumb dropped), got %d", len(s.GetURLs()[0].Videos))
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

	t.Run("tolerant rejects resolved URL exceeding 2048 chars", func(t *testing.T) {
		s := New()
		longPath := strings.Repeat("a", 2049-len("https://example.com/"))
		longURL := "https://example.com/" + longPath
		_, err := s.resolveAndValidateLoc(longURL, baseURL)
		if err == nil {
			t.Error("expected error for resolved URL exceeding 2048 characters in tolerant mode")
		}
	})

	t.Run("tolerant accepts resolved URL at exactly 2048 chars", func(t *testing.T) {
		s := New()
		longPath := strings.Repeat("a", 2048-len("https://example.com/"))
		longURL := "https://example.com/" + longPath
		_, err := s.resolveAndValidateLoc(longURL, baseURL)
		if err != nil {
			t.Errorf("unexpected error for resolved URL at exactly 2048 characters: %v", err)
		}
	})

	t.Run("tolerant rejects relative URL that resolves beyond 2048 chars", func(t *testing.T) {
		s := New()
		longPath := "/" + strings.Repeat("a", 2049-len("https://example.com/"))
		_, err := s.resolveAndValidateLoc(longPath, baseURL)
		if err == nil {
			t.Error("expected error for relative URL resolving to more than 2048 characters")
		}
	})
}

func TestS_Parse_Deduplication(t *testing.T) {
	var fetchCount int
	var mu sync.Mutex

	urlsetContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page-01</loc></url>
</urlset>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetchCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprint(w, urlsetContent)
	}))
	defer srv.Close()

	t.Run("duplicate sitemap URL in sitemapindex fetched only once", func(t *testing.T) {
		mu.Lock()
		fetchCount = 0
		mu.Unlock()

		sitemapURL := srv.URL + "/sitemap.xml"
		indexContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>%s</loc></sitemap>
    <sitemap><loc>%s</loc></sitemap>
    <sitemap><loc>%s</loc></sitemap>
</sitemapindex>`, sitemapURL, sitemapURL, sitemapURL)

		indexURL := srv.URL + "/sitemapindex.xml"
		s := New().SetMultiThread(false)
		_, err := s.Parse(indexURL, &indexContent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		got := fetchCount
		mu.Unlock()

		if got != 1 {
			t.Errorf("expected sitemap URL to be fetched exactly once, got %d fetches", got)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d: %v", s.GetErrorsCount(), s.GetErrors())
		}
	})

	t.Run("duplicate sitemap URL in robots.txt fetched only once", func(t *testing.T) {
		mu.Lock()
		fetchCount = 0
		mu.Unlock()

		sitemapURL := srv.URL + "/sitemap.xml"
		robotsTxt := fmt.Sprintf("User-agent: *\nSitemap: %s\nSitemap: %s\nSitemap: %s\n",
			sitemapURL, sitemapURL, sitemapURL)

		robotsURL := srv.URL + "/robots.txt"
		s := New()
		_, err := s.Parse(robotsURL, &robotsTxt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		got := fetchCount
		mu.Unlock()

		if got != 1 {
			t.Errorf("expected sitemap URL to be fetched exactly once from robots.txt, got %d fetches", got)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
	})

	t.Run("duplicate sitemap URL in sitemapindex fetched only once (multi-thread)", func(t *testing.T) {
		mu.Lock()
		fetchCount = 0
		mu.Unlock()

		sitemapURL := srv.URL + "/sitemap.xml"
		indexContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>%s</loc></sitemap>
    <sitemap><loc>%s</loc></sitemap>
    <sitemap><loc>%s</loc></sitemap>
</sitemapindex>`, sitemapURL, sitemapURL, sitemapURL)

		indexURL := srv.URL + "/sitemapindex.xml"
		s := New().SetMultiThread(true)
		_, err := s.Parse(indexURL, &indexContent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		got := fetchCount
		mu.Unlock()

		if got != 1 {
			t.Errorf("expected sitemap URL to be fetched exactly once, got %d fetches", got)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
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
		content := `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>/sitemap-02.xml</loc></sitemap>
</sitemapindex>`
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

	t.Run("strict rejects priority below 0.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>-0.1</priority></url>
</urlset>`, server.URL)
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

	t.Run("strict rejects priority above 1.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>1.1</priority></url>
</urlset>`, server.URL)
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

	t.Run("strict accepts priority at 0.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>0.0</priority></url>
</urlset>`, server.URL)
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict accepts priority at 1.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>1.0</priority></url>
</urlset>`, server.URL)
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d", s.GetErrorsCount())
		}
	})

	t.Run("strict accepts URL without priority", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc></url>
</urlset>`, server.URL)
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 1 {
			t.Errorf("expected 1 URL, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors, got %d", s.GetErrorsCount())
		}
	})

	t.Run("tolerant accepts out-of-range priority", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>-0.5</priority></url>
    <url><loc>%s/page-02</loc><priority>1.5</priority></url>
</urlset>`, server.URL, server.URL)
		sitemapURL := fmt.Sprintf("%s/sitemap.xml", server.URL)
		_, err := s.Parse(sitemapURL, &content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.GetURLCount() != 2 {
			t.Errorf("expected 2 URLs in tolerant mode, got %d", s.GetURLCount())
		}
		if s.GetErrorsCount() != 0 {
			t.Errorf("expected 0 errors in tolerant mode, got %d", s.GetErrorsCount())
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
			err:                  pointerOfString("validate \"%%\": parse \"%%\": invalid URL escape \"%%\""),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"%%\": parse \"%%\": invalid URL escape \"%%\""),
			},
		},
		{
			name:                 "invalid url",
			url:                  "invalid_url",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"invalid_url\": invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"invalid_url\": invalid URL scheme \"\": only http and https are supported"),
			},
		},
		{
			name:                 "empty url",
			url:                  "",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"\": invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"\": invalid URL scheme \"\": only http and https are supported"),
			},
		},
		{
			name:                 "relative url",
			url:                  "/just/a/path",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"/just/a/path\": invalid URL scheme \"\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"/just/a/path\": invalid URL scheme \"\": only http and https are supported"),
			},
		},
		{
			name:                 "missing host",
			url:                  "http://",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"http://\": missing host"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"http://\": missing host"),
			},
		},
		{
			name:                 "ftp url",
			url:                  "ftp://example.com/sitemap.xml",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"ftp://example.com/sitemap.xml\": invalid URL scheme \"ftp\": only http and https are supported"),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				errors.New("validate \"ftp://example.com/sitemap.xml\": invalid URL scheme \"ftp\": only http and https are supported"),
			},
		},
		{
			name:                 "testServer index page",
			url:                  server.URL,
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString(fmt.Sprintf("fetch %q: received HTTP status 404", server.URL)),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New(fmt.Sprintf("fetch %q: received HTTP status 404", server.URL))},
		},
		{
			name:                 "page not found",
			url:                  fmt.Sprintf("%s/404", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString(fmt.Sprintf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/404", server.URL))),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{errors.New(fmt.Sprintf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/404", server.URL)))},
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
			errs:                 []error{errors.New(fmt.Sprintf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/invalid.xml", server.URL)))},
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
			mainURLContent:       pointerOfString("error: gzip decompression failed: gzip: invalid checksum\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty-corrupted.xml.gz", server.URL), "")},
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
			errs:                 []error{fmt.Errorf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemapindex-empty.xml.gz", server.URL))},
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
			errs:                 []error{fmt.Errorf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemap-empty.xml.gz", server.URL))},
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
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")},
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
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")},
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
			errs: []error{errors.New(fmt.Sprintf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/invalid.xml", server.URL)))},
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
				errors.New("config \"rules\": error parsing regexp: missing argument to repetition operator: `*`"),
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
				errors.New("config \"follow\": error parsing regexp: missing closing ): `(`"),
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
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemap-empty.xml", server.URL), "")},
		},
		{
			name:        "RSS 2.0 sitemap",
			url:         "http://www.example.com/rss.xml",
			multiThread: true,
			content: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <link>http://www.example.com/rss-item-1</link>
    </item>
    <item>
      <link>http://www.example.com/rss-item-2</link>
    </item>
  </channel>
</rss>`),
			mainURLContent: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <link>http://www.example.com/rss-item-1</link>
    </item>
    <item>
      <link>http://www.example.com/rss-item-2</link>
    </item>
  </channel>
</rss>`),
			urls: []URL{
				{Loc: "http://www.example.com/rss-item-1"},
				{Loc: "http://www.example.com/rss-item-2"},
			},
		},
		{
			name:        "Atom 1.0 sitemap",
			url:         "http://www.example.com/atom.xml",
			multiThread: true,
			content: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link href="http://www.example.com/atom-entry-1"/>
  </entry>
  <entry>
    <link rel="alternate" href="http://www.example.com/atom-entry-2"/>
  </entry>
</feed>`),
			mainURLContent: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link href="http://www.example.com/atom-entry-1"/>
  </entry>
  <entry>
    <link rel="alternate" href="http://www.example.com/atom-entry-2"/>
  </entry>
</feed>`),
			urls: []URL{
				{Loc: "http://www.example.com/atom-entry-1"},
				{Loc: "http://www.example.com/atom-entry-2"},
			},
		},
		{
			name:           "Plain Text sitemap",
			url:            "http://www.example.com/sitemap.txt",
			multiThread:    true,
			content:        pointerOfString("http://www.example.com/text-url-1\n# comment\n  \nhttps://www.example.com/text-url-2"),
			mainURLContent: pointerOfString("http://www.example.com/text-url-1\n# comment\n  \nhttps://www.example.com/text-url-2"),
			urls: []URL{
				{Loc: "http://www.example.com/text-url-1"},
				{Loc: "https://www.example.com/text-url-2"},
			},
		},
		{
			name:        "RSS 2.0 with rules and invalid links",
			url:         "http://www.example.com/rss-rules.xml",
			rules:       []string{"valid"},
			multiThread: true,
			content: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item><link>http://www.example.com/valid-1</link></item>
    <item><link>http://www.example.com/wrong-1</link></item>
    <item><link>  </link></item>
  </channel>
</rss>`),
			mainURLContent: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item><link>http://www.example.com/valid-1</link></item>
    <item><link>http://www.example.com/wrong-1</link></item>
    <item><link>  </link></item>
  </channel>
</rss>`),
			urls: []URL{{Loc: "http://www.example.com/valid-1"}},
		},
		{
			name:        "Atom 1.0 with no alternate link",
			url:         "http://www.example.com/atom-no-alt.xml",
			multiThread: true,
			content: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="self" href="http://www.example.com/self"/>
  </entry>
</feed>`),
			mainURLContent: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="self" href="http://www.example.com/self"/>
  </entry>
</feed>`),
			urls: nil,
		},
		{
			name:        "RSS empty",
			url:         "http://www.example.com/rss-empty.xml",
			multiThread: true,
			content:     pointerOfString(""),
			mainURLContent: pointerOfString(""),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/rss-empty.xml\": sitemap content is empty")},
		},
		{
			name:        "Atom empty",
			url:         "http://www.example.com/atom-empty.xml",
			multiThread: true,
			content:     pointerOfString(""),
			mainURLContent: pointerOfString(""),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/atom-empty.xml\": sitemap content is empty")},
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
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemap-empty.xml", server.URL), "")},
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
				t.Logf("urls mismatch:\n  got:  %+v\n  want: %+v", sitemap.urls, test.urls)
				t.Error("urls is not equal to expected value")
			}
			if !compareErrorStrings(sitemap.errs, test.errs) {
				t.Errorf("errs mismatch:\n  got:  %v\n  want: %v", sitemap.errs, test.errs)
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
					s.errs = append(s.errs, fmt.Errorf("Dummy error %d", i))
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
			wantErr:        fmt.Errorf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/404", server.URL)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.setup()
			retURLContent, err := s.setContent(context.Background(), test.attrURLContent)
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
		{
			name:   "robots.txt with full-line comment",
			input:  "# Sitemap: https://example.com/commented\nSitemap: https://example.com/real",
			output: 1,
		},
		{
			name:   "robots.txt with inline comment after sitemap",
			input:  "Sitemap: https://example.com/real # primary sitemap",
			output: 1,
		},
		{
			name:   "robots.txt with UTF-8 BOM",
			input:  "\ufeffSitemap: https://example.com/bom",
			output: 1,
		},
		{
			name:   "robots.txt with leading whitespace before directive",
			input:  "   Sitemap: https://example.com/indented",
			output: 1,
		},
		{
			name:   "robots.txt with short non-sitemap line",
			input:  "User: x\nSitemap: https://example.com/ok",
			output: 1,
		},
		{
			name:   "robots.txt with blank lines",
			input:  "\n\nSitemap: https://example.com/ok\n\n",
			output: 1,
		},
		{
			name:   "robots.txt with only inline comment value",
			input:  "Sitemap: # only comment",
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
			_, err := s.fetch(context.Background(), test.url)
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
		_, err := s.fetch(context.Background(), server.URL)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		s := New().SetMaxResponseSize(512)
		_, err := s.fetch(context.Background(), server.URL)
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

	_, err := e.fetch(context.Background(), "://invalid-url")
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

	_, err := e.fetch(context.Background(), server.URL)
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

			got := s.checkAndUnzipContent("", tt.content)

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
			errsCount: 1, // duplicate URL is deduplicated; only one fetch attempt is made
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
			s.parseAndFetchUrlsMultiThread(context.Background(), test.locations, 0)

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
			errsCount: 1, // duplicate URL is deduplicated; only one fetch attempt is made
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
			s.parseAndFetchUrlsSequential(context.Background(), test.locations, 0)

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

	s := New().SetMaxDepth(1)
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsMultiThread(context.Background(), locations, 1)

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

	s := New().SetMaxDepth(1).SetMultiThread(false)
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsSequential(context.Background(), locations, 1)

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
			errsCount:                  1,
		},
		{
			name:                       "malformed sitemapindex XML",
			url:                        fmt.Sprintf("%s/sitemapindex.xml", server.URL),
			content:                    "<sitemapindex><broken",
			sitemapLocationsAddedCount: 0,
			urlsCount:                  0,
			errsCount:                  1,
		},
		{
			name:                       "malformed urlset XML",
			url:                        fmt.Sprintf("%s/sitemap.xml", server.URL),
			content:                    "<urlset><broken",
			sitemapLocationsAddedCount: 0,
			urlsCount:                  0,
			errsCount:                  1,
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
		{
			name:     "Empty element",
			xmlInput: "<lastmod></lastmod>",
			want:     time.Time{},
			wantErr:  false,
		},
		{
			name:     "Whitespace only",
			xmlInput: "<lastmod>   </lastmod>",
			want:     time.Time{},
			wantErr:  false,
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

func TestS_fetch_ContextCancel(t *testing.T) {
	// Server that blocks until the client gives up. We use a channel that
	// is never written to, so the handler waits for the request context to
	// be cancelled.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	s := New().SetFetchTimeout(30) // long timeout: only ctx can abort the call
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel shortly after the request starts.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := s.fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestS_ParseContext_Cancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	s := New().SetFetchTimeout(30)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := s.ParseContext(ctx, server.URL+"/sitemapindex-1.xml", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestS_fetch_NilContext(t *testing.T) {
	// Covers the `if ctx == nil { ctx = context.Background() }` branch in fetch.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	s := New()
	//nolint:staticcheck // intentionally passing nil to exercise the defensive branch
	body, err := s.fetch(nil, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("unexpected body: %q", string(body))
	}
}

func TestS_ParseContext_NilContext(t *testing.T) {
	// Covers the `if ctx == nil { ctx = context.Background() }` branch in ParseContext.
	server := testServer()
	defer server.Close()

	s := New()
	//nolint:staticcheck // intentionally passing nil to exercise the defensive branch
	if _, err := s.ParseContext(nil, server.URL+"/sitemap-01.xml", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.GetURLCount() == 0 {
		t.Error("expected URLs to be parsed, got 0")
	}
}

func TestS_ParseContext_PreCancelled_RobotsTXT(t *testing.T) {
	// Covers:
	//   - the early `ctx.Err()` check inside the robots.txt goroutine
	//   - the final `if ctxErr := ctx.Err(); ctxErr != nil { return s, ctxErr }`
	// We pre-supply the robots.txt body via urlContent so setContent does not
	// perform an HTTP fetch (which would fail before reaching the goroutine).
	robots := "Sitemap: http://127.0.0.1:1/sitemap.xml\n"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	s := New()
	_, err := s.ParseContext(ctx, "http://example.com/robots.txt", &robots)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestS_parseAndFetchUrlsMultiThread_PreCancelled(t *testing.T) {
	// Covers the loop-level `if ctx.Err() != nil { break }` branch in
	// parseAndFetchUrlsMultiThread.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	s.parseAndFetchUrlsMultiThread(ctx, []string{"http://127.0.0.1:1/a", "http://127.0.0.1:1/b"}, 0)
}

func TestS_parseAndFetchUrlsMultiThread_AcquireSlotCancel(t *testing.T) {
	// Covers the acquireSlot ctx-cancel error branch inside the goroutine
	// of parseAndFetchUrlsMultiThread. We pre-saturate the semaphore so the
	// goroutine must block, then cancel the context. The loop-level
	// ctx.Err() break is bypassed by using a context that becomes cancelled
	// only after the goroutine has been spawned.
	s := New().SetMaxConcurrency(1)
	s.sem = make(chan struct{}, 1)
	s.sem <- struct{}{} // saturate

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	s.parseAndFetchUrlsMultiThread(ctx, []string{"http://127.0.0.1:1/a"}, 0)

	if len(s.errs) == 0 {
		t.Error("expected at least one error from cancelled acquireSlot")
	}
}

func TestS_parseAndFetchUrlsSequential_PreCancelled(t *testing.T) {
	// Covers the early ctx.Err() return in parseAndFetchUrlsSequential.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	s.parseAndFetchUrlsSequential(ctx, []string{"http://127.0.0.1:1/a"}, 0)
}

func TestS_Parse_BackwardCompatible(t *testing.T) {
	// The legacy Parse signature must keep working unchanged.
	server := testServer()
	defer server.Close()

	s := New()
	if _, err := s.Parse(server.URL+"/sitemap-01.xml", nil); err != nil {
		t.Fatalf("unexpected error from Parse: %v", err)
	}
	if s.GetURLCount() == 0 {
		t.Error("expected URLs to be parsed via Parse, got 0")
	}
}

func TestS_SetMaxConcurrency(t *testing.T) {
	t.Run("default is defaultMaxConcurrency", func(t *testing.T) {
		s := New()
		if s.cfg.maxConcurrency != defaultMaxConcurrency {
			t.Errorf("expected default %d, got %d", defaultMaxConcurrency, s.cfg.maxConcurrency)
		}
	})
	t.Run("Positive", func(t *testing.T) {
		s := New().SetMaxConcurrency(4)
		if s.cfg.maxConcurrency != 4 {
			t.Errorf("expected 4, got %d", s.cfg.maxConcurrency)
		}
		if len(s.errs) != 0 {
			t.Errorf("expected no errors, got %d", len(s.errs))
		}
	})
	t.Run("Zero sets unlimited", func(t *testing.T) {
		s := New().SetMaxConcurrency(0)
		if s.cfg.maxConcurrency != 0 {
			t.Errorf("expected 0 (unlimited), got %d", s.cfg.maxConcurrency)
		}
		if len(s.errs) != 0 {
			t.Errorf("expected no errors, got %d", len(s.errs))
		}
	})
	t.Run("Negative", func(t *testing.T) {
		s := New().SetMaxConcurrency(-1)
		if s.cfg.maxConcurrency != defaultMaxConcurrency {
			t.Errorf("expected default %d to be preserved, got %d", defaultMaxConcurrency, s.cfg.maxConcurrency)
		}
		if len(s.errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(s.errs))
		}
	})
}

func TestS_acquireSlot_NilSem(t *testing.T) {
	s := New() // sem is nil by default
	if err := s.acquireSlot(context.Background()); err != nil {
		t.Errorf("expected nil error with nil sem, got %v", err)
	}
	s.releaseSlot() // must be a no-op with nil sem
}

func TestS_ParseContext_UnlimitedConcurrency(t *testing.T) {
	// SetMaxConcurrency(0) restores unlimited concurrency (sem == nil during Parse).
	server := testServer()
	defer server.Close()

	s := New().SetMaxConcurrency(0)
	if _, err := s.ParseContext(context.Background(), server.URL+"/sitemapindex-1.xml", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.GetURLCount() == 0 {
		t.Error("expected URLs, got 0")
	}
}

func TestS_acquireSlot_AcquireAndRelease(t *testing.T) {
	s := New()
	s.sem = make(chan struct{}, 2)
	if err := s.acquireSlot(context.Background()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := s.acquireSlot(context.Background()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(s.sem) != 2 {
		t.Errorf("expected sem fully occupied, got %d", len(s.sem))
	}
	s.releaseSlot()
	s.releaseSlot()
	if len(s.sem) != 0 {
		t.Errorf("expected sem empty, got %d", len(s.sem))
	}
}

func TestS_acquireSlot_CtxCancel(t *testing.T) {
	s := New()
	s.sem = make(chan struct{}, 1)
	// Saturate the semaphore so the next acquire must block.
	s.sem <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.acquireSlot(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestS_ParseContext_MaxConcurrency_Bounded(t *testing.T) {
	// Verify that parsing succeeds normally with a small concurrency cap.
	server := testServer()
	defer server.Close()

	s := New().SetMaxConcurrency(2)
	if _, err := s.ParseContext(context.Background(), server.URL+"/sitemapindex-1.xml", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.GetURLCount() == 0 {
		t.Error("expected URLs, got 0")
	}
	if cap(s.sem) != 2 {
		t.Errorf("expected sem cap 2, got %d", cap(s.sem))
	}
}

func TestS_ParseContext_MaxConcurrency_RobotsTXT_CtxCancel(t *testing.T) {
	// Pre-cancelled ctx + maxConcurrency=1 + a saturated semaphore forces
	// the robots.txt goroutine onto the acquireSlot ctx-cancel branch.
	robots := "Sitemap: http://127.0.0.1:1/sitemap.xml\n"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New().SetMaxConcurrency(1)
	if _, err := s.ParseContext(ctx, "http://example.com/robots.txt", &robots); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestS_ParseContext_RobotsTXT_Deadlock(t *testing.T) {
	// This test reproduces the deadlock scenario where a robots.txt sitemap
	// points to a sitemap index, and maxConcurrency is 1. The initial fetch
	// in the robots.txt goroutine must release its semaphore slot before
	// recursively calling parseAndFetchUrlsMultiThread, otherwise the nested
	// goroutines will block forever waiting for the single slot.
	server := testServer()
	defer server.Close()

	robots := fmt.Sprintf("Sitemap: %s/sitemapindex-1.xml\n", server.URL)
	s := New().SetMaxConcurrency(1)

	// Use a timeout to detect the deadlock.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.ParseContext(ctx, server.URL+"/robots.txt", &robots)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("deadlock detected: parsing timed out")
		}
		t.Fatalf("unexpected error: %v", err)
	}

	if s.GetURLCount() == 0 {
		t.Error("expected URLs to be parsed, but got 0")
	}
}

func configsEqual(c1, c2 config) bool {
	return c1.fetchTimeout == c2.fetchTimeout &&
		c1.userAgent == c2.userAgent &&
		c1.maxResponseSize == c2.maxResponseSize &&
		c1.maxDepth == c2.maxDepth &&
		c1.maxConcurrency == c2.maxConcurrency &&
		c1.multiThread == c2.multiThread &&
		c1.httpClient == c2.httpClient &&
		reflect.DeepEqual(c1.follow, c2.follow) &&
		reflect.DeepEqual(c1.rules, c2.rules)
}

func pointerOfString(str string) *string {
	return &str
}

func pointerOfFloat32(number float32) *float32 {
	return &number
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

	sort.Slice(testCaseURLs, func(i, j int) bool {
		return testCaseURLs[i].Loc < testCaseURLs[j].Loc
	})

	for i, sitemapURL := range sitemapURLs {
		if sitemapURL.Loc != testCaseURLs[i].Loc {
			return false
		}
		if (sitemapURL.LastMod == nil) != (testCaseURLs[i].LastMod == nil) {
			return false
		}
		if sitemapURL.LastMod != nil && sitemapURL.LastMod.Unix() != testCaseURLs[i].LastMod.Unix() {
			return false
		}
		if (sitemapURL.ChangeFreq == nil) != (testCaseURLs[i].ChangeFreq == nil) {
			return false
		}
		if sitemapURL.ChangeFreq != nil && *sitemapURL.ChangeFreq != *testCaseURLs[i].ChangeFreq {
			return false
		}
		if (sitemapURL.Priority == nil) != (testCaseURLs[i].Priority == nil) {
			return false
		}
		if sitemapURL.Priority != nil && *sitemapURL.Priority != *testCaseURLs[i].Priority {
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

func TestTypedErrors_ConfigError(t *testing.T) {
	s := New().SetMaxDepth(-1)
	errs := s.GetErrors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	var cfgErr *ConfigError
	if !errors.As(errs[0], &cfgErr) {
		t.Fatalf("expected *ConfigError, got %T", errs[0])
	}
	if cfgErr.Field != "maxDepth" {
		t.Errorf("expected field 'maxDepth', got %q", cfgErr.Field)
	}
	if cfgErr.Unwrap() == nil {
		t.Error("expected non-nil Unwrap")
	}
	if cfgErr.Err == nil {
		t.Error("expected non-nil Err")
	}
}

func TestTypedErrors_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := New().SetMultiThread(false)
	url := server.URL + "/not-found.xml"
	_, _ = s.Parse(url, nil)
	errs := s.GetErrors()
	if len(errs) == 0 {
		t.Fatal("expected at least 1 error")
	}

	var netErr *NetworkError
	if !errors.As(errs[0], &netErr) {
		t.Fatalf("expected *NetworkError, got %T", errs[0])
	}
	if netErr.URL != url {
		t.Errorf("expected URL %q, got %q", url, netErr.URL)
	}
	if netErr.Unwrap() == nil {
		t.Error("expected non-nil Unwrap")
	}
}

func TestTypedErrors_ParseError(t *testing.T) {
	s := New().SetMultiThread(false)
	content := "\n" // no XML root element → unrecognized format
	sitemapURL := "http://example.com/sitemap.xml"
	_, _ = s.Parse(sitemapURL, &content)
	errs := s.GetErrors()
	if len(errs) == 0 {
		t.Fatal("expected at least 1 error")
	}

	var parseErr *ParseError
	if !errors.As(errs[0], &parseErr) {
		t.Fatalf("expected *ParseError, got %T", errs[0])
	}
	if parseErr.URL != sitemapURL {
		t.Errorf("expected URL %q, got %q", sitemapURL, parseErr.URL)
	}
	if parseErr.Unwrap() == nil {
		t.Error("expected non-nil Unwrap")
	}
}

func TestTypedErrors_ValidationError(t *testing.T) {
	s := New().SetStrict(true)
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>/relative-path</loc></url>
</urlset>`
	sitemapURL := "http://example.com/sitemap.xml"
	_, _ = s.Parse(sitemapURL, &content)
	errs := s.GetErrors()
	if len(errs) == 0 {
		t.Fatal("expected at least 1 error")
	}

	var valErr *ValidationError
	if !errors.As(errs[0], &valErr) {
		t.Fatalf("expected *ValidationError, got %T", errs[0])
	}
	if valErr.URL == "" {
		t.Error("expected non-empty URL in ValidationError")
	}
	if valErr.Unwrap() == nil {
		t.Error("expected non-nil Unwrap")
	}
}

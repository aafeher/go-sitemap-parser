package sitemap

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
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
				maxSitemaps:     50000,
				maxURLs:         10000000,
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
	t.Run("PositiveTimeout", func(t *testing.T) {
		s := New()
		s.SetFetchTimeout(5)
		if s.cfg.fetchTimeout != 5 {
			t.Errorf("expected 5, got %v", s.cfg.fetchTimeout)
		}
		if len(s.errs) != 0 {
			t.Errorf("expected no errors, got %v", s.errs)
		}
	})

	t.Run("LargeTimeout", func(t *testing.T) {
		s := New()
		s.SetFetchTimeout(600)
		if s.cfg.fetchTimeout != 600 {
			t.Errorf("expected 600, got %v", s.cfg.fetchTimeout)
		}
	})

	t.Run("ZeroTimeout records ConfigError and keeps default", func(t *testing.T) {
		s := New()
		defaultTimeout := s.cfg.fetchTimeout
		s.SetFetchTimeout(0)
		if s.cfg.fetchTimeout != defaultTimeout {
			t.Errorf("expected fetchTimeout to remain %d, got %d", defaultTimeout, s.cfg.fetchTimeout)
		}
		if len(s.errs) != 1 {
			t.Fatalf("expected 1 error, got %d", len(s.errs))
		}
		var cfgErr *ConfigError
		if !errors.As(s.errs[0], &cfgErr) {
			t.Fatalf("expected *ConfigError, got %T", s.errs[0])
		}
		if cfgErr.Field != "fetchTimeout" {
			t.Errorf("expected field %q, got %q", "fetchTimeout", cfgErr.Field)
		}
	})
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

// configErrorFields returns the Field of every error in errs, comma-separated and
// in order. An error that is not a *ConfigError is reported by its type instead,
// so an unexpected entry shows up in the comparison.
func configErrorFields(errs []error) string {
	fields := make([]string, 0, len(errs))
	for _, err := range errs {
		var cfgErr *ConfigError
		if errors.As(err, &cfgErr) {
			fields = append(fields, cfgErr.Field)
			continue
		}
		fields = append(fields, fmt.Sprintf("%T", err))
	}
	return strings.Join(fields, ",")
}

// TestS_Setters_ConfigErrors verifies that every setter call replaces the
// configuration errors recorded by the previous call for the same setting.
func TestS_Setters_ConfigErrors(t *testing.T) {
	tests := []struct {
		field   string
		invalid func(s *S)
		valid   func(s *S)
	}{
		{"fetchTimeout", func(s *S) { s.SetFetchTimeout(0) }, func(s *S) { s.SetFetchTimeout(5) }},
		{"maxResponseSize", func(s *S) { s.SetMaxResponseSize(0) }, func(s *S) { s.SetMaxResponseSize(1024) }},
		{"maxDepth", func(s *S) { s.SetMaxDepth(0) }, func(s *S) { s.SetMaxDepth(5) }},
		{"maxConcurrency", func(s *S) { s.SetMaxConcurrency(-1) }, func(s *S) { s.SetMaxConcurrency(4) }},
		{"maxSitemaps", func(s *S) { s.SetMaxSitemaps(-1) }, func(s *S) { s.SetMaxSitemaps(100) }},
		{"maxURLs", func(s *S) { s.SetMaxURLs(-1) }, func(s *S) { s.SetMaxURLs(1000) }},
		{"follow", func(s *S) { s.SetFollow([]string{`(`}) }, func(s *S) { s.SetFollow([]string{`alpha`}) }},
		{"rules", func(s *S) { s.SetRules([]string{`(`}) }, func(s *S) { s.SetRules([]string{`alpha`}) }},
	}

	for _, test := range tests {
		t.Run(test.field+" valid value clears the error", func(t *testing.T) {
			s := New()
			test.invalid(s)
			mustEqual(t, "errors after invalid value", configErrorFields(s.errs), test.field)
			test.valid(s)
			mustEqual(t, "errors after valid value", configErrorFields(s.errs), "")
		})

		t.Run(test.field+" repeated invalid value is recorded once", func(t *testing.T) {
			s := New()
			test.invalid(s)
			test.invalid(s)
			mustEqual(t, "errors", configErrorFields(s.errs), test.field)
		})
	}

	t.Run("errors of other settings and of other kinds are kept", func(t *testing.T) {
		s := New().SetMaxDepth(0).SetFetchTimeout(0)
		s.errs = append(s.errs, errors.New("Dummy error"))
		s.SetMaxDepth(5)
		mustEqual(t, "errors", configErrorFields(s.errs), "fetchTimeout,*errors.errorString")
	})

	t.Run("a slice returned earlier by GetErrors is not modified", func(t *testing.T) {
		s := New().SetMaxDepth(0).SetFetchTimeout(0)
		before := s.GetErrors()
		s.SetMaxDepth(5)
		mustEqual(t, "errors returned before the call", configErrorFields(before), "maxDepth,fetchTimeout")
		mustEqual(t, "errors returned after the call", configErrorFields(s.GetErrors()), "fetchTimeout")
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestS_GetConfiguration_Defaults(t *testing.T) {
	s := New()
	mustEqual(t, "GetUserAgent", s.GetUserAgent(), "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)")
	mustEqual(t, "GetFetchTimeout", s.GetFetchTimeout(), 3)
	mustEqual(t, "GetMultiThread", s.GetMultiThread(), true)
	mustEqual(t, "GetMaxResponseSize", s.GetMaxResponseSize(), 50*1024*1024)
	mustEqual(t, "GetMaxDepth", s.GetMaxDepth(), 10)
	mustEqual(t, "GetMaxConcurrency", s.GetMaxConcurrency(), 16)
	mustEqual(t, "GetMaxSitemaps", s.GetMaxSitemaps(), 50000)
	mustEqual(t, "GetMaxURLs", s.GetMaxURLs(), 10000000)
	mustEqual(t, "GetFollow length", len(s.GetFollow()), 0)
	mustEqual(t, "GetRules length", len(s.GetRules()), 0)
	mustEqual(t, "GetHTTPClient is nil", s.GetHTTPClient() == nil, true)
	mustEqual(t, "GetStrict", s.GetStrict(), false)
}

func TestS_GetConfiguration_AfterSetters(t *testing.T) {
	customClient := &http.Client{}
	s := New().
		SetUserAgent("TestAgent/1.0").
		SetFetchTimeout(30).
		SetMultiThread(false).
		SetMaxResponseSize(1024).
		SetMaxDepth(5).
		SetMaxConcurrency(8).
		SetMaxSitemaps(100).
		SetMaxURLs(1000).
		SetFollow([]string{`\.xml$`}).
		SetRules([]string{`/product/`}).
		SetHTTPClient(customClient).
		SetStrict(true)

	mustEqual(t, "GetUserAgent", s.GetUserAgent(), "TestAgent/1.0")
	mustEqual(t, "GetFetchTimeout", s.GetFetchTimeout(), 30)
	mustEqual(t, "GetMultiThread", s.GetMultiThread(), false)
	mustEqual(t, "GetMaxResponseSize", s.GetMaxResponseSize(), 1024)
	mustEqual(t, "GetMaxDepth", s.GetMaxDepth(), 5)
	mustEqual(t, "GetMaxConcurrency", s.GetMaxConcurrency(), 8)
	mustEqual(t, "GetMaxSitemaps", s.GetMaxSitemaps(), 100)
	mustEqual(t, "GetMaxURLs", s.GetMaxURLs(), 1000)
	follow := s.GetFollow()
	mustEqual(t, "GetFollow length", len(follow), 1)
	if len(follow) > 0 {
		mustEqual(t, "GetFollow[0]", follow[0], `\.xml$`)
	}
	rules := s.GetRules()
	mustEqual(t, "GetRules length", len(rules), 1)
	if len(rules) > 0 {
		mustEqual(t, "GetRules[0]", rules[0], `/product/`)
	}
	mustEqual(t, "GetHTTPClient", s.GetHTTPClient(), customClient)
	mustEqual(t, "GetStrict", s.GetStrict(), true)
}

func TestS_GetConfiguration_CopySemantics(t *testing.T) {
	s := New().SetFollow([]string{`\.xml$`}).SetRules([]string{`/product/`})
	follow := s.GetFollow()
	follow[0] = "mutated"
	mustEqual(t, "GetFollow after mutation", s.GetFollow()[0], `\.xml$`)
	rules := s.GetRules()
	rules[0] = "mutated"
	mustEqual(t, "GetRules after mutation", s.GetRules()[0], `/product/`)
}

func TestImage_validateAndFilterImages(t *testing.T) {
	tests := []struct {
		name       string
		strict     bool
		images     []Image
		wantImages int
		wantErrs   int
	}{
		{"empty input returns empty", false, nil, 0, 0},
		{"tolerant: valid image kept", false, []Image{{Loc: "https://example.com/photo.jpg", Title: "T"}}, 1, 0},
		{"tolerant: empty loc silently dropped", false, []Image{{Loc: ""}}, 0, 0},
		{"tolerant: loc exceeding max length rejected with error", false, []Image{{Loc: "http://example.com/" + strings.Repeat("a", maxLocLength)}}, 0, 1},
		{"tolerant: non-HTTP scheme accepted", false, []Image{{Loc: "ftp://example.com/photo.jpg"}}, 1, 0},
		{"tolerant: multiple images, one empty loc dropped", false, []Image{{Loc: "https://example.com/a.jpg"}, {Loc: ""}, {Loc: "https://example.com/b.jpg"}}, 2, 0},
		{"strict: valid HTTP image kept", true, []Image{{Loc: "http://example.com/photo.jpg"}}, 1, 0},
		{"strict: valid HTTPS image kept", true, []Image{{Loc: "https://cdn.example.com/photo.jpg"}}, 1, 0},
		{"strict: empty loc produces error and is dropped", true, []Image{{Loc: ""}}, 0, 1},
		{"strict: non-HTTP scheme rejected", true, []Image{{Loc: "ftp://example.com/photo.jpg"}}, 0, 1},
		{"strict: loc exceeding max length rejected", true, []Image{{Loc: "https://example.com/" + strings.Repeat("a", maxLocLength)}}, 0, 1},
		{"strict: unparseable URL rejected with error", true, []Image{{Loc: "http://example.com/path%zzinvalid"}}, 0, 1},
		{"strict: CDN host (different from page host) accepted", true, []Image{{Loc: "https://cdn.other-host.com/photo.jpg"}}, 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.strict {
				s = s.SetStrict(true)
			}
			got, errs := s.validateAndFilterImages(tt.images)
			if len(got) != tt.wantImages {
				t.Errorf("expected %d images, got %d", tt.wantImages, len(got))
			}
			if len(errs) != tt.wantErrs {
				t.Errorf("expected %d errors, got %d: %v", tt.wantErrs, len(errs), errs)
			}
		})
	}
}

func assertImageFields(t *testing.T, img Image, loc, title, caption, geoLocation, license string) {
	t.Helper()
	mustEqual(t, "image.Loc", img.Loc, loc)
	mustEqual(t, "image.Title", img.Title, title)
	mustEqual(t, "image.Caption", img.Caption, caption)
	mustEqual(t, "image.GeoLocation", img.GeoLocation, geoLocation)
	mustEqual(t, "image.License", img.License, license)
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
		us := requireURLSetParse(t, New(), data)
		if len(us.URL) != 1 {
			t.Fatalf("expected 1 URL, got %d", len(us.URL))
		}
		u := us.URL[0]
		if len(u.Images) != 2 {
			t.Fatalf("expected 2 images, got %d", len(u.Images))
		}
		assertImageFields(t, u.Images[0], "https://example.com/photo1.jpg", "First photo", "A caption", "Budapest, Hungary", "https://creativecommons.org/licenses/by/4.0/")
		mustEqual(t, "image[1].Loc", u.Images[1].Loc, "https://example.com/photo2.jpg")
		if u.Images[1].Title != "" || u.Images[1].Caption != "" {
			t.Errorf("expected empty optional fields on second image")
		}
	})

	t.Run("URL without images has nil Images slice", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page</loc></url>
</urlset>`
		us := requireURLSetParse(t, New(), data)
		mustEqual(t, "image count", len(us.URL[0].Images), 0)
	})

	t.Run("image element without namespace is ignored", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url>
        <loc>https://example.com/page</loc>
        <image><loc>https://example.com/photo.jpg</loc></image>
    </url>
</urlset>`
		us := requireURLSetParse(t, New(), data)
		mustEqual(t, "image count (no namespace)", len(us.URL[0].Images), 0)
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
		us := requireURLSetParse(t, New(), data)
		if len(us.URL) != 2 {
			t.Fatalf("expected 2 URLs, got %d", len(us.URL))
		}
		mustEqual(t, "URL[0] image count", len(us.URL[0].Images), 1)
		mustEqual(t, "URL[1] image count", len(us.URL[1].Images), 0)
	})
}

func TestImage_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with images parses correctly", func(t *testing.T) {
		s := New()
		requireParse(t, s, server.URL+"/sitemap-image-01.xml", nil)
		assertCounts(t, s, 2, 0)

		var pageWithImages, pageWithout URL
		for _, u := range s.GetURLs() {
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
		mustEqual(t, "image.Title", img.Title, "First photo")
		mustEqual(t, "image.Caption", img.Caption, "A caption")
		mustEqual(t, "image.GeoLocation", img.GeoLocation, "Budapest, Hungary")
		mustEqual(t, "image.License", img.License, "https://creativecommons.org/licenses/by/4.0/")
		if !strings.HasSuffix(pageWithImages.Images[1].Loc, "/photo2.jpg") {
			t.Errorf("unexpected second image loc: %q", pageWithImages.Images[1].Loc)
		}
		mustEqual(t, "pageWithout image count", len(pageWithout.Images), 0)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		urls := s.GetURLs()
		if len(urls) != 1 {
			t.Fatalf("expected 1 URL, got %d", len(urls))
		}
		mustEqual(t, "image count (empty loc dropped)", len(urls[0].Images), 1)
		mustEqual(t, "error count", s.GetErrorsCount(), int64(0))
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "error count", s.GetErrorsCount(), int64(1))
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "error count", s.GetErrorsCount(), int64(1))
		mustEqual(t, "image count (ftp dropped)", len(s.GetURLs()[0].Images), 0)
	})
}

func TestNews_validateNews(t *testing.T) {
	makeDate := func(s string) *LastModTime {
		lmt := &LastModTime{}
		tok := xml.NewDecoder(strings.NewReader("<d>" + s + "</d>"))
		start, _ := tok.Token()
		_ = lmt.UnmarshalXML(tok, start.(xml.StartElement))
		return lmt
	}

	t.Run("nil input returns nil", func(t *testing.T) {
		s := New()
		got, errs := s.validateNews("", nil, nil)
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
		got, errs := s.validateNews("https://example.com/page", n, nil)
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
		got, errs := s.validateNews("https://example.com/page", n, nil)
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
		got, errs := s.validateNews("https://example.com/page", n, nil)
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
		_, errs := s.validateNews("https://example.com/page", n, nil)
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
		_, errs := s.validateNews("https://example.com/page", n, nil)
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
		_, errs := s.validateNews("https://example.com/page", n, nil)
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
		_, errs := s.validateNews("https://example.com/page", n, nil)
		if len(errs) != 1 {
			t.Errorf("expected 1 error for nil publication_date, got %d", len(errs))
		}
	})

	t.Run("strict: all required fields missing produces four errors", func(t *testing.T) {
		s := New().SetStrict(true)
		n := &News{}
		got, errs := s.validateNews("https://example.com/page", n, nil)
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
		urlSet, err := decodeURLSet(s, data)
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
		urlSet, err := decodeURLSet(s, data)
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
		urlSet, err := decodeURLSet(s, data)
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
		urlSet, err := decodeURLSet(s, data)
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

func TestNews_validateNews_InvalidDate(t *testing.T) {
	n := &News{
		Title:       "Title",
		Publication: NewsPublication{Name: "Name", Language: "en"},
	}

	t.Run("strict mode does not report an invalid date as missing", func(t *testing.T) {
		got, errs := New().SetStrict(true).validateNews("https://example.com/page", n, pointerOfString("yesterday"))
		if got != n {
			t.Error("expected the news entry to be kept")
		}
		mustEqual(t, "errors", len(errs), 0)
	})

	t.Run("strict mode reports an absent date as missing", func(t *testing.T) {
		_, errs := New().SetStrict(true).validateNews("https://example.com/page", n, nil)
		if len(errs) != 1 {
			t.Fatalf("expected the date to be reported as missing, got %v", errs)
		}
		mustEqual(t, "error", errs[0].Error(), `validate "https://example.com/page": strict mode: news <publication_date> is missing`)
	})

	for _, text := range []string{"", " ", "\n\t "} {
		t.Run(fmt.Sprintf("strict mode reports an empty date as empty, %q", text), func(t *testing.T) {
			got, errs := New().SetStrict(true).validateNews("https://example.com/page", n, &text)
			if got != n {
				t.Error("expected the news entry to be kept")
			}
			if len(errs) != 1 {
				t.Fatalf("expected the date to be reported as empty, got %v", errs)
			}
			mustEqual(t, "error", errs[0].Error(), `validate "https://example.com/page": strict mode: news <publication_date> is empty`)
			var validationErr *ValidationError
			if !errors.As(errs[0], &validationErr) {
				t.Errorf("expected a *ValidationError, got %T", errs[0])
			}
		})
	}

	t.Run("strict mode reports nothing about a date that is there", func(t *testing.T) {
		dated := *n
		dated.PublicationDate = &LastModTime{time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)}
		// What the element held does not matter once it yielded a date.
		for _, text := range []*string{nil, pointerOfString(""), pointerOfString("2024-01-15")} {
			_, errs := New().SetStrict(true).validateNews("https://example.com/page", &dated, text)
			mustEqual(t, "errors", len(errs), 0)
		}
	})

	t.Run("tolerant mode does not require the date", func(t *testing.T) {
		for _, text := range []*string{nil, pointerOfString(""), pointerOfString(" "), pointerOfString("yesterday")} {
			got, errs := New().validateNews("https://example.com/page", n, text)
			if got != n {
				t.Error("expected the news entry to be kept")
			}
			mustEqual(t, "errors", len(errs), 0)
		}
	})
}

func TestNews_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with news parses correctly", func(t *testing.T) {
		s := New()
		requireParse(t, s, server.URL+"/sitemap-news-01.xml", nil)
		assertCounts(t, s, 2, 0)

		var article, plain URL
		for _, u := range s.GetURLs() {
			if strings.HasSuffix(u.Loc, "/article-1") {
				article = u
			} else {
				plain = u
			}
		}

		if article.News == nil {
			t.Fatal("expected News on article URL")
		}
		mustEqual(t, "news.Title", article.News.Title, "Breaking: Example Article")
		mustEqual(t, "news.Publication.Name", article.News.Publication.Name, "Example News")
		mustEqual(t, "news.Publication.Language", article.News.Publication.Language, "en")
		mustEqual(t, "news.PublicationDate is set", article.News.PublicationDate != nil, true)
		mustEqual(t, "plain.News is nil", plain.News == nil, true)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		assertCounts(t, s, 1, 0)
		mustEqual(t, "news is set", s.GetURLs()[0].News != nil, true)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "error count", s.GetErrorsCount(), int64(4))
		urls := s.GetURLs()
		mustEqual(t, "URL count", len(urls), 1)
		mustEqual(t, "news entry kept", urls[0].News != nil, true)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "error count", s.GetErrorsCount(), int64(0))
		urls := s.GetURLs()
		if len(urls) != 1 || urls[0].News == nil {
			t.Error("expected URL with News in tolerant mode")
		} else {
			mustEqual(t, "news.Title", urls[0].News.Title, "Only Title")
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

// mustEqual is a generic test helper that fails if got != want.
func mustEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// requireParse calls Parse and fatals on error.
func requireParse(t *testing.T, s *S, url string, content *string) {
	t.Helper()
	_, err := s.Parse(url, content)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

// assertCounts verifies GetURLCount and GetErrorsCount on s.
func assertCounts(t *testing.T, s *S, wantURLs int64, wantErrs int64) {
	t.Helper()
	if s.GetURLCount() != wantURLs {
		t.Fatalf("expected %d URLs, got %d", wantURLs, s.GetURLCount())
	}
	if s.GetErrorsCount() != wantErrs {
		t.Errorf("expected %d errors, got %d: %v", wantErrs, s.GetErrorsCount(), s.GetErrors())
	}
}

// decodedURLSet holds the entries of a <urlset> for the tests that look into
// them; parseURLSet itself hands the entries on without keeping them.
type decodedURLSet struct {
	URL []urlEntry
}

// decodeURLSet collects the entries parseURLSet yields for data.
func decodeURLSet(s *S, data string) (decodedURLSet, error) {
	var us decodedURLSet
	err := s.parseURLSet(data, func(entry *urlEntry) {
		us.URL = append(us.URL, *entry)
	})
	return us, err
}

// requireURLSetParse calls parseURLSet and fatals on error.
func requireURLSetParse(t *testing.T, s *S, data string) decodedURLSet {
	t.Helper()
	result, err := decodeURLSet(s, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func assertPtrInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s: got %v, want %d", name, got, want)
	}
}

func assertPtrFloat32(t *testing.T, name string, got *float32, want float32) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s: got %v, want %f", name, got, want)
	}
}

func assertVideoRestriction(t *testing.T, r *VideoRestriction, wantRel, wantVal string) {
	t.Helper()
	if r == nil {
		t.Error("Restriction: unexpected nil")
		return
	}
	if wantRel != "" {
		mustEqual(t, "Restriction.Relationship", r.Relationship, wantRel)
	}
	if wantVal != "" {
		mustEqual(t, "Restriction.Value", r.Value, wantVal)
	}
}

func assertVideoPlatform(t *testing.T, p *VideoPlatform, wantRel, wantVal string) {
	t.Helper()
	if p == nil {
		t.Error("Platform: unexpected nil")
		return
	}
	if wantRel != "" {
		mustEqual(t, "Platform.Relationship", p.Relationship, wantRel)
	}
	if wantVal != "" {
		mustEqual(t, "Platform.Value", p.Value, wantVal)
	}
}

func assertVideoUploader(t *testing.T, u *VideoUploader, wantVal, wantInfo string) {
	t.Helper()
	if u == nil {
		t.Error("Uploader: unexpected nil")
		return
	}
	if wantVal != "" {
		mustEqual(t, "Uploader.Value", u.Value, wantVal)
	}
	if wantInfo != "" {
		mustEqual(t, "Uploader.Info", u.Info, wantInfo)
	}
}

func assertStringSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertHasSuffix(t *testing.T, name, got, suffix string) {
	t.Helper()
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("%s: %q does not end with %q", name, got, suffix)
	}
}

func TestVideo_validateAndFilterVideos(t *testing.T) {
	dur300 := 300
	rat45 := float32(4.5)
	manyTags := make([]string, maxVideoTags+1)
	for i := range manyTags {
		manyTags[i] = fmt.Sprintf("tag%d", i)
	}

	tests := []struct {
		name       string
		strict     bool
		videos     []Video
		wantVideos int
		wantErrs   int
	}{
		{"empty input returns empty", false, nil, 0, 0},
		{"tolerant: valid video kept", false, []Video{{ThumbnailLoc: "https://example.com/thumb.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4"}}, 1, 0},
		{"tolerant: empty ThumbnailLoc silently dropped", false, []Video{{ThumbnailLoc: ""}}, 0, 0},
		{"tolerant: ThumbnailLoc exceeding max length rejected with error", false, []Video{{ThumbnailLoc: "https://example.com/" + strings.Repeat("a", maxLocLength)}}, 0, 1},
		{"tolerant: non-HTTP scheme accepted", false, []Video{{ThumbnailLoc: "ftp://example.com/thumb.jpg"}}, 1, 0},
		{"tolerant: multiple videos one without ThumbnailLoc dropped", false, []Video{{ThumbnailLoc: "https://example.com/a.jpg"}, {ThumbnailLoc: ""}, {ThumbnailLoc: "https://example.com/b.jpg"}}, 2, 0},
		{"strict: valid video kept without errors", true, []Video{{ThumbnailLoc: "https://example.com/thumb.jpg", Title: "Title", Description: "Description", ContentLoc: "https://example.com/video.mp4", Duration: &dur300, Rating: &rat45, Tags: []string{"a", "b"}}}, 1, 0},
		{"strict: empty ThumbnailLoc produces error and drops video", true, []Video{{ThumbnailLoc: ""}}, 0, 1},
		{"strict: ThumbnailLoc exceeding max length rejected", true, []Video{{ThumbnailLoc: "https://example.com/" + strings.Repeat("a", maxLocLength)}}, 0, 1},
		{"strict: non-HTTP scheme rejected", true, []Video{{ThumbnailLoc: "ftp://example.com/thumb.jpg"}}, 0, 1},
		{"strict: unparseable ThumbnailLoc rejected", true, []Video{{ThumbnailLoc: "https://example.com/path%zzinvalid"}}, 0, 1},
		{"strict: empty title produces error, video kept", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "", Description: "D", ContentLoc: "https://example.com/v.mp4"}}, 1, 1},
		{"strict: empty description produces error, video kept", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "", ContentLoc: "https://example.com/v.mp4"}}, 1, 1},
		{"strict: no ContentLoc and no PlayerLoc produces error, video kept", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D"}}, 1, 1},
		{"strict: PlayerLoc alone satisfies content requirement", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", PlayerLoc: "https://example.com/player"}}, 1, 0},
		{"strict: Duration below 1 produces error", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Duration: pointerOfInt(0)}}, 1, 1},
		{"strict: Duration above 28800 produces error", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Duration: pointerOfInt(maxVideoDuration + 1)}}, 1, 1},
		{"strict: Rating below 0 produces error", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Rating: pointerOfFloat32Video(-0.1)}}, 1, 1},
		{"strict: Rating above 5.0 produces error", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Rating: pointerOfFloat32Video(5.1)}}, 1, 1},
		{"strict: Tags exceeding 32 produces error, video kept", true, []Video{{ThumbnailLoc: "https://example.com/t.jpg", Title: "T", Description: "D", ContentLoc: "https://example.com/v.mp4", Tags: manyTags}}, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.strict {
				s = s.SetStrict(true)
			}
			got, errs := s.validateAndFilterVideos(tt.videos)
			if len(got) != tt.wantVideos || len(errs) != tt.wantErrs {
				t.Errorf("expected %d videos, %d errors; got %d, %d: %v", tt.wantVideos, tt.wantErrs, len(got), len(errs), errs)
			}
		})
	}
}

func TestHreflang_validateAndFilterHreflangs(t *testing.T) {
	tests := []struct {
		name      string
		strict    bool
		links     []AlternateLink
		wantLinks int
		wantErrs  int
	}{
		{"nil or empty", false, nil, 0, 0},
		{"tolerant mode: drop empty href", false, []AlternateLink{{Href: ""}, {Href: "http://example.com/"}}, 1, 0},
		{"both modes: reject oversized href", false, []AlternateLink{{Href: "http://example.com/" + strings.Repeat("a", maxLocLength)}}, 0, 1},
		{"strict mode: valid link", true, []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "http://example.com/"}}, 1, 0},
		{"strict mode: reject empty href", true, []AlternateLink{{Href: ""}}, 0, 1},
		{"strict mode: reject invalid rel", true, []AlternateLink{{Rel: "canonical", Hreflang: "en", Href: "http://example.com/"}}, 0, 1},
		{"strict mode: reject empty hreflang", true, []AlternateLink{{Rel: "alternate", Hreflang: "", Href: "http://example.com/"}}, 0, 1},
		{"strict mode: reject invalid URL", true, []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "http://example.com/%%invalid"}}, 0, 1},
		{"strict mode: reject unsupported scheme", true, []AlternateLink{{Rel: "alternate", Hreflang: "en", Href: "ftp://example.com/"}}, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.strict {
				s = s.SetStrict(true)
			}
			got, errs := s.validateAndFilterHreflangs(tt.links)
			if len(got) != tt.wantLinks || len(errs) != tt.wantErrs {
				t.Errorf("expected %d links, %d errors; got %d, %d", tt.wantLinks, tt.wantErrs, len(got), len(errs))
			}
		})
	}
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
		urlSet := requireURLSetParse(t, s, data)
		u := urlSet.URL[0]
		if len(u.Videos) != 1 {
			t.Fatalf("expected 1 video, got %d", len(u.Videos))
		}
		v := u.Videos[0]
		mustEqual(t, "ThumbnailLoc", v.ThumbnailLoc, "https://example.com/thumb.jpg")
		mustEqual(t, "Title", v.Title, "Example Video")
		mustEqual(t, "Description", v.Description, "A description")
		mustEqual(t, "ContentLoc", v.ContentLoc, "https://example.com/video.mp4")
		mustEqual(t, "PlayerLoc", v.PlayerLoc, "https://example.com/player")
		assertPtrInt(t, "Duration", v.Duration, 600)
		assertPtrFloat32(t, "Rating", v.Rating, 4.5)
		assertPtrInt(t, "ViewCount", v.ViewCount, 1000)
		mustEqual(t, "FamilyFriendly", v.FamilyFriendly, "yes")
		assertVideoRestriction(t, v.Restriction, "allow", "HU AT")
		assertVideoPlatform(t, v.Platform, "allow", "web mobile")
		mustEqual(t, "RequiresSubscription", v.RequiresSubscription, "no")
		assertVideoUploader(t, v.Uploader, "Channel", "https://example.com/uploader")
		mustEqual(t, "Live", v.Live, "no")
		assertStringSlice(t, "Tags", v.Tags, []string{"golang", "sitemap"})
	})

	t.Run("URL without video has nil Videos slice", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page</loc></url>
</urlset>`
		s := New()
		urlSet := requireURLSetParse(t, s, data)
		mustEqual(t, "Videos count", len(urlSet.URL[0].Videos), 0)
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
		urlSet := requireURLSetParse(t, s, data)
		mustEqual(t, "Videos count (no namespace)", len(urlSet.URL[0].Videos), 0)
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
		urlSet := requireURLSetParse(t, s, data)
		mustEqual(t, "Videos count on first URL", len(urlSet.URL[0].Videos), 1)
		mustEqual(t, "Videos count on second URL", len(urlSet.URL[1].Videos), 0)
	})
}

func TestVideo_Parse_integration(t *testing.T) {
	server := testServer()
	defer server.Close()

	t.Run("fixture with video parses correctly", func(t *testing.T) {
		s := New()
		requireParse(t, s, server.URL+"/sitemap-video-01.xml", nil)
		assertCounts(t, s, 2, 0)

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
		assertHasSuffix(t, "ThumbnailLoc", v.ThumbnailLoc, "/thumb.jpg")
		mustEqual(t, "Title", v.Title, "Example Video")
		assertPtrInt(t, "Duration", v.Duration, 600)
		assertPtrFloat32(t, "Rating", v.Rating, 4.5)
		assertPtrInt(t, "ViewCount", v.ViewCount, 1000)
		assertVideoRestriction(t, v.Restriction, "allow", "")
		assertVideoPlatform(t, v.Platform, "allow", "")
		assertVideoUploader(t, v.Uploader, "ExampleChannel", "")
		mustEqual(t, "Tags count", len(v.Tags), 2)
		mustEqual(t, "plain Videos count", len(plain.Videos), 0)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "errors count", s.GetErrorsCount(), int64(0))
		mustEqual(t, "Videos count", len(s.GetURLs()[0].Videos), 1)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "errors count", s.GetErrorsCount(), int64(0))
		mustEqual(t, "Videos count", len(s.GetURLs()[0].Videos), 1)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		// title, description, content_loc+player_loc = 3 errors
		mustEqual(t, "errors count", s.GetErrorsCount(), int64(3))
		mustEqual(t, "Videos count", len(s.GetURLs()[0].Videos), 1)
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
		requireParse(t, s, server.URL+"/sitemap.xml", &content)
		mustEqual(t, "errors count", s.GetErrorsCount(), int64(0))
		mustEqual(t, "Videos count", len(s.GetURLs()[0].Videos), 1)
	})
}

func TestS_resolveAndValidateLoc(t *testing.T) {
	const baseURL = "https://example.com/sitemaps/index.xml"
	longURL2049 := "https://example.com/" + strings.Repeat("a", 2049-len("https://example.com/"))
	longURL2048 := "https://example.com/" + strings.Repeat("a", 2048-len("https://example.com/"))
	longRelPath := "/" + strings.Repeat("a", 2049-len("https://example.com/"))

	tests := []struct {
		name         string
		strict       bool
		loc          string
		base         string
		wantErr      bool
		wantResolved string
	}{
		{"tolerant absolute URL", false, "https://example.com/page1", baseURL, false, "https://example.com/page1"},
		{"tolerant relative URL with leading slash", false, "/products/page1.html", baseURL, false, "https://example.com/products/page1.html"},
		{"tolerant relative URL without leading slash", false, "page2.html", baseURL, false, "https://example.com/sitemaps/page2.html"},
		{"tolerant ftp URL rejected", false, "ftp://example.com/file", baseURL, true, ""},
		{"tolerant unparseable loc", false, "%%", baseURL, true, ""},
		{"tolerant unparseable base URL", false, "/page", "%%", true, ""},
		{"strict valid absolute URL", true, "https://example.com/page1", baseURL, false, "https://example.com/page1"},
		{"strict rejects relative URL", true, "/products/page1.html", baseURL, true, ""},
		{"strict rejects ftp scheme", true, "ftp://example.com/file", baseURL, true, ""},
		{"strict rejects different host", true, "https://other.com/page", baseURL, true, ""},
		{"strict rejects different protocol", true, "http://example.com/page", baseURL, true, ""},
		{"strict rejects URL exceeding 2048 chars", true, longURL2049, baseURL, true, ""},
		{"strict accepts URL at exactly 2048 chars", true, longURL2048, baseURL, false, ""},
		{"strict rejects missing host", true, "https:///path", baseURL, true, ""},
		{"strict accepts host in another letter case", true, "https://EXAMPLE.com/page1", baseURL, false, "https://EXAMPLE.com/page1"},
		{"strict accepts the default port", true, "https://example.com:443/page1", baseURL, false, "https://example.com:443/page1"},
		{"strict rejects another port", true, "https://example.com:8443/page1", baseURL, true, ""},
		{"strict rejects a space", true, "https://example.com/page 1", baseURL, true, ""},
		{"tolerant encodes a space of the path", false, "https://example.com/page 1", baseURL, false, "https://example.com/page%201"},
		{"tolerant encodes a space of the query", false, "https://example.com/page?q=a b", baseURL, false, "https://example.com/page?q=a%20b"},
		{"tolerant encodes a space of a relative URL", false, "page 1?q=a b", baseURL, false, "https://example.com/sitemaps/page%201?q=a%20b"},
		{"tolerant rejects resolved URL exceeding 2048 chars", false, longURL2049, baseURL, true, ""},
		{"tolerant accepts resolved URL at exactly 2048 chars", false, longURL2048, baseURL, false, ""},
		{"tolerant rejects relative URL that resolves beyond 2048 chars", false, longRelPath, baseURL, true, ""},
		{"tolerant rejects empty loc", false, "", baseURL, true, ""},
		{"strict rejects empty loc", true, "", baseURL, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.strict {
				s = s.SetStrict(true)
			}
			resolved, err := s.resolveAndValidateLoc(tt.loc, tt.base)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (resolved=%q)", resolved)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.wantResolved != "" && resolved != tt.wantResolved {
					t.Errorf("expected %q, got %q", tt.wantResolved, resolved)
				}
			}
		})
	}
}

// TestS_resolveAndValidateLoc_EmptyLoc verifies that an empty location is
// rejected rather than resolved: as a relative URL it resolves to the base, the
// URL of the sitemap it was read from.
func TestS_resolveAndValidateLoc_EmptyLoc(t *testing.T) {
	const baseURL = "https://example.com/sitemaps/index.xml"

	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			s := New().SetStrict(strict)

			resolved, err := s.resolveAndValidateLoc("", baseURL)

			mustEqual(t, "resolved", resolved, "")
			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *ValidationError, got %T: %v", err, err)
			}
			mustEqual(t, "error URL", valErr.URL, baseURL)
			mustEqual(t, "error", valErr.Error(), `validate "https://example.com/sitemaps/index.xml": <loc> of an entry is empty or missing`)
		})
	}
}

// TestS_resolveAndValidateLoc_Host verifies that a location has to name a host
// and has to resolve to a URL that can be parsed. A reference that begins with
// "//" gives the host itself: net/url resolves one without a host to the host
// of the sitemap, "//" to the sitemap itself, and reads the host of one
// leniently, so that the URL it resolves to may not be one.
func TestS_resolveAndValidateLoc_Host(t *testing.T) {
	const baseURL = "https://example.com/sitemaps/index.xml"
	const noScheme = `strict mode: unsupported scheme ""`

	tests := []struct {
		name string
		loc  string
		// want is the URL tolerant mode resolves loc to, empty if it rejects loc.
		want string
		// wantErr is the error of tolerant mode, empty if it accepts loc. An error of
		// net/url is given without its reason, which is worded by net/url.
		wantErr string
		// wantStrictErr is the error of strict mode, empty if it accepts loc.
		wantStrictErr string
	}{
		{"URL with a host", "https://example.com/page", "https://example.com/page", "", ""},
		{"URL with a host and a port", "https://example.com:443/page", "https://example.com:443/page", "", ""},
		{"URL with an empty port", "https://example.com:/page", "https://example.com:/page", "", ""},
		{"URL with a user", "https://user@example.com/page", "https://user@example.com/page", "", ""},
		{"URL without a host", "https:///page", "", "missing host", "strict mode: missing host"},
		{"URL with a port and no host", "https://:8080/page", "", "missing host", "strict mode: missing host"},
		{"URL with an empty port and no host", "https://:", "", "missing host", "strict mode: missing host"},
		{"URL with a user and no host", "https://user@/page", "", "missing host", "strict mode: missing host"},
		{"URL with a user, a port and no host", "https://user@:8080/page", "", "missing host", "strict mode: missing host"},
		{"URL with nothing after the slashes", "https://", "", "missing host", "strict mode: missing host"},
		{"URL with a single slash", "https:/page", "", "missing host", "strict mode: missing host"},
		{"URL without a slash", "https:page", "", "missing host", "strict mode: missing host"},
		{"URL without a slash and with a space", "https:a page", "", "missing host", "strict mode: missing host"},
		{"scheme alone", "http:", "", "missing host", "strict mode: missing host"},
		{"scheme and a query", "https:?page=2", "", "missing host", "strict mode: missing host"},
		{"reference with a host", "//cdn.example.net/page", "https://cdn.example.net/page", "", noScheme},
		{"reference with a host and a port", "//cdn.example.net:8443/page", "https://cdn.example.net:8443/page", "", noScheme},
		{"reference with an IPv6 host", "//[2001:db8::1]:8443/page", "https://[2001:db8::1]:8443/page", "", noScheme},
		{"reference with a user and a host", "//user@cdn.example.net/page", "https://user@cdn.example.net/page", "", noScheme},
		{"reference without a host", "//", "", "missing host", noScheme},
		{"reference without a host, with a query", "//?page=2", "", "missing host", noScheme},
		{"reference without a host, with a fragment", "//#top", "", "missing host", noScheme},
		{"reference without a host, with a path", "///page", "", "missing host", noScheme},
		{"reference with a port and no host", "//:8080/page", "", "missing host", noScheme},
		{"reference with an empty port and no host", "//:", "", "missing host", noScheme},
		{"reference with a user and no host", "//user@/page", "", "missing host", noScheme},
		{"reference with an empty user and no host", "//@", "", "missing host", noScheme},
		{"reference with colons for a host", "//::", "", `parse "https://::": `, noScheme},
		{"reference with two ports", "//cdn.example.net:80:8080/page", "", `parse "https://cdn.example.net:80:8080/page": `, noScheme},
		{"reference with an IPv6 host without brackets", "//::1/page", "", `parse "https://::1/page": `, noScheme},
		{"path that begins with a slash", "/page", "https://example.com/page", "", noScheme},
		{"path with two slashes in it", "/a//b", "https://example.com/a//b", "", noScheme},
		{"query alone", "?page=2", "https://example.com/sitemaps/index.xml?page=2", "", noScheme},
	}

	for _, tt := range tests {
		t.Run(tt.name+", tolerant mode", func(t *testing.T) {
			resolved, err := New().resolveAndValidateLoc(tt.loc, baseURL)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mustEqual(t, "resolved", resolved, tt.want)
				if _, err := neturl.Parse(resolved); err != nil {
					t.Errorf("resolved to a URL that cannot be parsed: %v", err)
				}
				return
			}
			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *ValidationError, got %T: %v (resolved=%q)", err, err, resolved)
			}
			// The error names the location as the document gives it.
			mustEqual(t, "error URL", valErr.URL, tt.loc)
			mustEqual(t, "resolved", resolved, tt.loc)
			want := fmt.Sprintf("validate %q: %s", tt.loc, tt.wantErr)
			if !strings.HasSuffix(tt.wantErr, ": ") {
				mustEqual(t, "error", err.Error(), want)
				return
			}
			// What net/url has against the URL is its own wording, the URL it names is not.
			var urlErr *neturl.Error
			if !errors.As(err, &urlErr) {
				t.Fatalf("expected the error of net/url to be wrapped, got %v", err)
			}
			if !strings.HasPrefix(err.Error(), want) {
				t.Errorf("error: %q does not begin with %q", err.Error(), want)
			}
		})

		t.Run(tt.name+", strict mode", func(t *testing.T) {
			resolved, err := New().SetStrict(true).resolveAndValidateLoc(tt.loc, baseURL)

			mustEqual(t, "resolved", resolved, tt.loc)
			if tt.wantStrictErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *ValidationError, got %T: %v", err, err)
			}
			mustEqual(t, "error", err.Error(), fmt.Sprintf("validate %q: %s", tt.loc, tt.wantStrictErr))
		})
	}
}

// TestNamesNoHost verifies which references are taken for one that begins
// with "//" and names no host after it.
func TestNamesNoHost(t *testing.T) {
	tests := []struct {
		loc  string
		want bool
	}{
		{"//", true},
		{"//?page=2", true},
		{"//#top", true},
		{"///page", true},
		{"////page", true},
		{"//:8080/page", true},
		{"//user@/page", true},
		{"//@", true},
		{"//cdn.example.net/page", false},
		{"//cdn.example.net:8080/page", false},
		{"//user@cdn.example.net/page", false},
		{"/page", false},
		{"/a//b", false},
		{"page//", false},
		{"?next=//", false},
		{"https:///page", false},
		{"https://example.com//", false},
	}

	for _, tt := range tests {
		t.Run(tt.loc, func(t *testing.T) {
			parsed, err := neturl.Parse(tt.loc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustEqual(t, "namesNoHost", namesNoHost(tt.loc, parsed), tt.want)
		})
	}
}

// TestS_Parse_LocationWithoutHost verifies, for every format and in both
// modes, that a location which names no host, or which does not resolve to a
// URL, is skipped and reported: it is not returned as a page, and it is not
// requested as a sitemap. The entries around it are kept.
func TestS_Parse_LocationWithoutHost(t *testing.T) {
	const noScheme = `strict mode: unsupported scheme ""`
	// parseError returns what net/url has against rawURL, which is worded by net/url.
	parseError := func(rawURL string) string {
		_, err := neturl.Parse(rawURL)
		if err == nil {
			t.Fatalf("expected net/url to turn down %q", rawURL)
		}
		return err.Error()
	}

	// rejected holds the locations that are turned down, with the error of either mode.
	rejected := []struct{ loc, err, strictErr string }{
		{"https:///no-host", "missing host", "strict mode: missing host"},
		{"https://:8080/port-only", "missing host", "strict mode: missing host"},
		{"//", "missing host", noScheme},
		{"///path-only", "missing host", noScheme},
		{"//:8080/port-only", "missing host", noScheme},
		{"//::", parseError("https://::"), noScheme},
	}

	// wrap returns the document that holds an entry for each location: before is what
	// stands in front of a location, after is what follows it.
	wrap := func(head, before, after, tail string) func(locs []string) string {
		return func(locs []string) string {
			var b strings.Builder
			b.WriteString(head)
			for _, loc := range locs {
				b.WriteString(before + loc + after)
			}
			b.WriteString(tail)
			return b.String()
		}
	}
	formats := []struct {
		name string
		url  string
		// document returns the document that lists locs.
		document func(locs []string) string
		// sitemaps tells whether the document lists sitemaps, which are requested, or pages.
		sitemaps bool
		// absoluteOnly tells whether the format takes nothing for an entry but an absolute URL.
		absoluteOnly bool
	}{
		{name: "urlset", url: "https://example.com/sitemap.xml", document: wrap(`<urlset>`, `<url><loc>`, `</loc></url>`, `</urlset>`)},
		{name: "sitemap index", url: "https://example.com/sitemap.xml", document: wrap(`<sitemapindex>`, `<sitemap><loc>`, `</loc></sitemap>`, `</sitemapindex>`), sitemaps: true},
		{name: "RSS", url: "https://example.com/feed.xml", document: wrap(`<rss><channel>`, `<item><link>`, `</link></item>`, `</channel></rss>`)},
		{name: "Atom", url: "https://example.com/feed.xml", document: wrap(`<feed>`, `<entry><link href="`, `"/></entry>`, `</feed>`)},
		{name: "text", url: "https://example.com/sitemap.txt", document: wrap("", "", "\n", ""), absoluteOnly: true},
		{name: "robots.txt", url: "https://example.com/robots.txt", document: wrap("User-agent: *\n", "Sitemap: ", "\n", ""), sitemaps: true},
	}

	for _, format := range formats {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", format.name, strict), func(t *testing.T) {
				// The entries that are kept stand before and after the ones that are not.
				locs := []string{"https://example.com/first"}
				wantErrs := []string{}
				for _, r := range rejected {
					if format.absoluteOnly && !strings.HasPrefix(r.loc, "https://") {
						continue
					}
					locs = append(locs, r.loc)
					reason := r.err
					if strict {
						reason = r.strictErr
					}
					wantErrs = append(wantErrs, fmt.Sprintf("validate %q: %s", r.loc, reason))
				}
				locs = append(locs, "https://example.com/last")
				content := format.document(locs)

				requested := []string{}
				client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					requested = append(requested, req.URL.String())
					body := `<urlset><url><loc>https://example.com/page-of-` + strings.TrimPrefix(req.URL.Path, "/") + `</loc></url></urlset>`
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{},
						Body:       io.NopCloser(strings.NewReader(body)),
						Request:    req,
					}, nil
				})}
				s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(client)
				requireParse(t, s, format.url, &content)

				if format.sitemaps {
					assertStringSlice(t, "requests", requested, []string{"https://example.com/first", "https://example.com/last"})
					assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/page-of-first", "https://example.com/page-of-last"})
				} else {
					assertStringSlice(t, "requests", requested, []string{})
					assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/first", "https://example.com/last"})
				}
				assertStringSlice(t, "errors", errorsOf(s), wantErrs)
				for _, err := range s.GetErrors() {
					var valErr *ValidationError
					if !errors.As(err, &valErr) {
						t.Errorf("expected *ValidationError, got %T: %v", err, err)
					}
				}
			})
		}
	}
}

// TestS_Parse_URLWithoutHost verifies that Parse turns down a URL that names
// no host before anything is requested, one that names a port alone included.
func TestS_Parse_URLWithoutHost(t *testing.T) {
	urls := []string{
		"https:///sitemap.xml",
		"https://:8080/sitemap.xml",
		"https://:",
		"http://:80",
		"https://user@/sitemap.xml",
		"https://user@:8080/sitemap.xml",
	}

	for _, url := range urls {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", url, strict), func(t *testing.T) {
				requested := []string{}
				client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					requested = append(requested, req.URL.String())
					return nil, errors.New("offline")
				})}
				s := New().SetStrict(strict).SetHTTPClient(client)
				_, err := s.Parse(url, nil)

				var valErr *ValidationError
				if !errors.As(err, &valErr) {
					t.Fatalf("expected *ValidationError, got %T: %v", err, err)
				}
				want := fmt.Sprintf("validate %q: missing host", url)
				mustEqual(t, "error", err.Error(), want)
				assertStringSlice(t, "errors", errorsOf(s), []string{want})
				assertStringSlice(t, "requests", requested, []string{})
			})
		}
	}
}

// TestS_Parse_EmptyLocationInIndex verifies that a sitemap index entry without
// a location is not followed. Resolved like a relative URL it would be the URL
// of the index, which would then be fetched once more.
func TestS_Parse_EmptyLocationInIndex(t *testing.T) {
	const indexContent = `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc></loc></sitemap>
    <sitemap><lastmod>2024-01-15</lastmod></sitemap>
</sitemapindex>`

	tests := []struct {
		name        string
		multiThread bool
		// passContent tells whether the index is handed to Parse or fetched by it.
		passContent bool
		wantFetches int
	}{
		{"fetched index, sequential", false, false, 1},
		{"fetched index, multi-thread", true, false, 1},
		{"index passed as content, sequential", false, true, 0},
		{"index passed as content, multi-thread", true, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetchCount int
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				fetchCount++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/xml")
				_, _ = fmt.Fprint(w, indexContent)
			}))
			defer srv.Close()

			indexURL := srv.URL + "/sitemapindex.xml"
			var content *string
			if tt.passContent {
				content = pointerOfString(indexContent)
			}
			s := New().SetMultiThread(tt.multiThread)
			requireParse(t, s, indexURL, content)

			mu.Lock()
			got := fetchCount
			mu.Unlock()
			mustEqual(t, "fetches", got, tt.wantFetches)

			assertCounts(t, s, 0, 2)
			for _, err := range s.GetErrors() {
				var valErr *ValidationError
				if !errors.As(err, &valErr) {
					t.Fatalf("expected *ValidationError, got %T: %v", err, err)
				}
				mustEqual(t, "error URL", valErr.URL, indexURL)
			}
		})
	}
}

func TestS_Parse_Deduplication(t *testing.T) {
	var fetchCount int
	var mu sync.Mutex

	urlsetContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://example.com/page-01</loc></url>
</urlset>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 0, 2)
	})

	t.Run("strict rejects cross-host loc", func(t *testing.T) {
		s := New().SetStrict(true)
		content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>https://other-domain.com/page-01</loc></url>
</urlset>`
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 0, 1)
	})

	t.Run("strict rejects relative loc in sitemapindex", func(t *testing.T) {
		s := New().SetStrict(true).SetMultiThread(false)
		content := `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <sitemap><loc>/sub-sitemap.xml</loc></sitemap>
</sitemapindex>`
		requireParse(t, s, fmt.Sprintf("%s/sitemapindex.xml", server.URL), &content)
		assertCounts(t, s, 0, 1)
	})

	t.Run("strict accepts same-host absolute URLs", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc></url>
    <url><loc>%s/page-02</loc></url>
</urlset>`, server.URL, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 2, 0)
	})

	t.Run("strict rejects priority below 0.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>-0.1</priority></url>
</urlset>`, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 0, 1)
	})

	t.Run("strict rejects priority above 1.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>1.1</priority></url>
</urlset>`, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 0, 1)
	})

	t.Run("strict accepts priority at 0.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>0.0</priority></url>
</urlset>`, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 1, 0)
	})

	t.Run("strict accepts priority at 1.0", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>1.0</priority></url>
</urlset>`, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 1, 0)
	})

	t.Run("strict accepts URL without priority", func(t *testing.T) {
		s := New().SetStrict(true)
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc></url>
</urlset>`, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 1, 0)
	})

	t.Run("tolerant accepts out-of-range priority", func(t *testing.T) {
		s := New()
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url><loc>%s/page-01</loc><priority>-0.5</priority></url>
    <url><loc>%s/page-02</loc><priority>1.5</priority></url>
</urlset>`, server.URL, server.URL)
		requireParse(t, s, fmt.Sprintf("%s/sitemap.xml", server.URL), &content)
		assertCounts(t, s, 2, 0)
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
		robotsTxtSitemapURLs []string
		sitemapLocations     []string
		urls                 []URL
		errs                 []error
		strict               bool
	}{
		{
			name:                 "unparseable url",
			url:                  "%%",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("validate \"%%\": parse \"%%\": invalid URL escape \"%%\""),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{fmt.Errorf("fetch %q: received HTTP status 404", server.URL)},
		},
		{
			name:                 "page not found",
			url:                  fmt.Sprintf("%s/404", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString(fmt.Sprintf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/404", server.URL))),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{fmt.Errorf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/404", server.URL))},
		},

		// robots.txt
		{
			name:                 "robots.txt empty file",
			url:                  fmt.Sprintf("%s/robots-empty/robots.txt", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
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
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
		{
			name:        "robots.txt with two sitemapindex",
			url:         fmt.Sprintf("%s/robots-with-sitemapindex-2/robots.txt", server.URL),
			multiThread: false,
			follow:      []string{},
			rules:       []string{},
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
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-07", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqNever),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-08", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-09", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-10", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-11", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-12", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: []string{fmt.Sprintf("%s/invalid.xml", server.URL)},
			sitemapLocations:     nil,
			urls:                 nil,
			errs:                 []error{fmt.Errorf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/invalid.xml", server.URL))},
		},
		{
			name:                 "robots.txt with sitemapindex.xml.gz",
			url:                  fmt.Sprintf("%s/robots-with-sitemapindex-gz/robots.txt", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
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
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty-corrupted.xml.gz", server.URL), "")),
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty-corrupted.xml.gz", server.URL), "")},
		},
		{
			name:                 "sitemapindex.xml.gz empty file",
			url:                  fmt.Sprintf("%s/sitemapindex-empty.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemapindex-empty.xml.gz", server.URL))),
			errs:                 []error{fmt.Errorf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemapindex-empty.xml.gz", server.URL))},
		},
		{
			name:                 "sitemapindex.xml.gz",
			url:                  fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
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
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemap-empty.xml.gz", server.URL))),
			errs:                 []error{fmt.Errorf("parse %q: sitemap content is empty", fmt.Sprintf("%s/sitemap-empty.xml.gz", server.URL))},
		},
		{
			name:                 "sitemap.xml.gz",
			url:                  fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")),
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")},
		},
		{
			name:                 "sitemapindex.xml empty content",
			url:                  fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")),
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemapindex-empty.xml", server.URL), "")},
		},
		{
			name:                 "sitemapindex.xml",
			url:                  fmt.Sprintf("%s/sitemapindex-1.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
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
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-with-invalid-sitemap.xml", server.URL),
				fmt.Sprintf("%s/invalid.xml", server.URL),
			},
			urls: nil,
			errs: []error{fmt.Errorf("fetch %q: received HTTP status 404", fmt.Sprintf("%s/invalid.xml", server.URL))},
		},
		{
			name:                 "sitemapindex with follow and rules",
			url:                  fmt.Sprintf("%s/sitemapindex-follow-1.xml", server.URL),
			multiThread:          false,
			follow:               []string{`alpha`},
			rules:                []string{`page`},
			robotsTxtSitemapURLs: nil,
			sitemapLocations: []string{
				fmt.Sprintf("%s/sitemapindex-follow-1.xml", server.URL),
				fmt.Sprintf("%s/sitemap-follow-alpha-01.xml", server.URL),
				fmt.Sprintf("%s/sitemap-follow-alpha-02.xml", server.URL),
			},
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-alpha-01", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-alpha-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
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
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemap-empty.xml", server.URL), "")),
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
			urls: []URL{
				{Loc: "http://www.example.com/atom-entry-1"},
				{Loc: "http://www.example.com/atom-entry-2"},
			},
		},
		{
			name:        "Plain Text sitemap",
			url:         "http://www.example.com/sitemap.txt",
			multiThread: true,
			content:     pointerOfString("http://www.example.com/text-url-1\n# comment\n  \nhttps://www.example.com/text-url-2"),
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
			urls: nil,
		},
		{
			name:        "RSS empty",
			url:         "http://www.example.com/rss-empty.xml",
			multiThread: true,
			content:     pointerOfString(""),
			err:         pointerOfString("parse \"http://www.example.com/rss-empty.xml\": sitemap content is empty"),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/rss-empty.xml\": sitemap content is empty")},
		},
		{
			name:        "Atom empty",
			url:         "http://www.example.com/atom-empty.xml",
			multiThread: true,
			content:     pointerOfString(""),
			err:         pointerOfString("parse \"http://www.example.com/atom-empty.xml\": sitemap content is empty"),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/atom-empty.xml\": sitemap content is empty")},
		},
		{
			name:        "RSS 2.0 malformed XML",
			url:         "http://www.example.com/rss-malformed.xml",
			multiThread: true,
			content:     pointerOfString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><item>`),
			err:         pointerOfString("parse \"http://www.example.com/rss-malformed.xml\": XML syntax error on line 1: unexpected EOF"),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/rss-malformed.xml\": XML syntax error on line 1: unexpected EOF")},
		},
		{
			name:        "Atom 1.0 malformed XML",
			url:         "http://www.example.com/atom-malformed.xml",
			multiThread: true,
			content:     pointerOfString(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><entry>`),
			err:         pointerOfString("parse \"http://www.example.com/atom-malformed.xml\": XML syntax error on line 1: unexpected EOF"),
			errs:        []error{fmt.Errorf("parse \"http://www.example.com/atom-malformed.xml\": XML syntax error on line 1: unexpected EOF")},
		},
		{
			name:        "RSS 2.0 with relative URL in strict mode",
			url:         "http://www.example.com/rss-strict.xml",
			strict:      true,
			multiThread: true,
			content: pointerOfString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item><link>/relative</link></item>
  </channel>
</rss>`),
			errs: []error{&ValidationError{URL: "/relative", Err: errors.New("strict mode: unsupported scheme \"\"")}},
		},
		{
			name:                 "sitemap.xml empty content",
			url:                  fmt.Sprintf("%s/sitemap-empty.xml", server.URL),
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			content:              pointerOfString("\n"),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			err:                  pointerOfString(fmt.Sprintf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemap-empty.xml", server.URL), "")),
			errs:                 []error{fmt.Errorf("parse %q: unrecognized sitemap format (root element: %q)", fmt.Sprintf("%s/sitemap-empty.xml", server.URL), "")},
		},
		{
			name:                 "sitemap.xml",
			url:                  fmt.Sprintf("%s/sitemap-02.xml.gz", server.URL),
			multiThread:          false,
			follow:               []string{},
			rules:                []string{},
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls: []URL{
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(LastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(ChangeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New().SetStrict(test.strict)
			sitemap, err := s.SetMultiThread(test.multiThread).SetFollow(test.follow).SetRules(test.rules).Parse(test.url, test.content)
			switch {
			case test.err == nil && err != nil:
				t.Errorf("Unexpected error: %v", err)
			case test.err != nil && err == nil:
				t.Errorf("Expected error %q, but got nil", *test.err)
			case test.err != nil && err.Error() != *test.err:
				t.Errorf("Expected error %q, but got %q", *test.err, err)
			}

			if sitemap == nil {
				t.Fatal("Expected not nil object, but got nil")
			}

			if err == nil {
				if sitemap.mainURL != test.url {
					t.Fatalf("Expected URL to be %s, but got %s", test.url, sitemap.mainURL)
				}
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

// TestS_Parse_ReuseAfterErrors verifies that errors recorded by one Parse call
// neither block the next call nor leak into its results.
func TestS_Parse_ReuseAfterErrors(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	valid := `<urlset><url><loc>https://example.com/ok</loc></url></urlset>`

	t.Run("after a call that recorded a non-fatal error", func(t *testing.T) {
		withInvalidLoc := `<urlset><url><loc>https://example.com/a</loc></url><url><loc>ftp://example.com/b</loc></url></urlset>`
		s := New()
		requireParse(t, s, url, &withInvalidLoc)
		assertCounts(t, s, 1, 1)

		requireParse(t, s, url, &valid)
		assertCounts(t, s, 1, 0)
		mustEqual(t, "Loc", s.GetURLs()[0].Loc, "https://example.com/ok")
	})

	t.Run("after a call whose fetch failed", func(t *testing.T) {
		server := testServer()
		defer server.Close()

		s := New()
		if _, err := s.Parse(server.URL+"/nonexistent.xml", nil); err == nil {
			t.Fatal("expected a fetch error, got nil")
		}
		assertCounts(t, s, 0, 1)

		requireParse(t, s, url, &valid)
		assertCounts(t, s, 1, 0)
	})

	t.Run("after a call with an invalid input URL", func(t *testing.T) {
		s := New()
		requireParse(t, s, url, &valid)
		assertCounts(t, s, 1, 0)

		// The rejected call must not keep serving the results of the call before it.
		_, err := s.Parse("ftp://example.com/sitemap.xml", nil)
		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("expected *ValidationError, got %v", err)
		}
		assertCounts(t, s, 0, 1)

		requireParse(t, s, url, &valid)
		assertCounts(t, s, 1, 0)
	})
}

// TestS_Parse_ConfigErrors verifies that configuration errors, unlike the errors
// of a Parse call, persist until the setting is corrected.
func TestS_Parse_ConfigErrors(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	const blocked = "errors occurred before parsing, see GetErrors() for details"
	valid := `<urlset><url><loc>https://example.com/ok</loc></url></urlset>`

	requireBlocked := func(t *testing.T, s *S) {
		t.Helper()
		_, err := s.Parse(url, &valid)
		if err == nil || err.Error() != blocked {
			t.Fatalf("expected %q, got %v", blocked, err)
		}
	}

	t.Run("block parsing until the setting is corrected", func(t *testing.T) {
		s := New().SetMaxDepth(0)
		requireBlocked(t, s)
		// A blocked call must leave the configuration error in place, exactly once.
		requireBlocked(t, s)
		assertCounts(t, s, 0, 1)

		s.SetMaxDepth(5)
		requireParse(t, s, url, &valid)
		assertCounts(t, s, 1, 0)
	})

	t.Run("discard the results of the previous call", func(t *testing.T) {
		withInvalidLoc := `<urlset><url><loc>https://example.com/a</loc></url><url><loc>ftp://example.com/b</loc></url></urlset>`
		s := New()
		requireParse(t, s, url, &withInvalidLoc)
		assertCounts(t, s, 1, 1)

		s.SetRules([]string{`(`})
		requireBlocked(t, s)
		assertCounts(t, s, 0, 1)
		var cfgErr *ConfigError
		if !errors.As(s.GetErrors()[0], &cfgErr) {
			t.Fatalf("expected the remaining error to be a *ConfigError, got %T", s.GetErrors()[0])
		}
		mustEqual(t, "ConfigError.Field", cfgErr.Field, "rules")
	})
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

// TestS_Parse_SettersDuringParse verifies that the configuration may be set
// and read while Parse is running. Every setter writes the value already in
// effect, so the outcome of the calls stays predictable; what the test is
// after is the race detector, which reports a setting that is read without
// the lock.
func TestS_Parse_SettersDuringParse(t *testing.T) {
	// robots.txt lists three sitemap indexes of two sitemaps each, every
	// sitemap holding one URL.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/robots.txt":
			for i := 0; i < 3; i++ {
				_, _ = fmt.Fprintf(w, "Sitemap: %s/index-%d.xml\n", srv.URL, i)
			}
		case strings.HasPrefix(r.URL.Path, "/index-"):
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/index-"), ".xml")
			_, _ = fmt.Fprint(w, `<sitemapindex>`)
			for _, part := range []string{"a", "b"} {
				_, _ = fmt.Fprintf(w, `<sitemap><loc>%s/sitemap-%s-%s.xml</loc></sitemap>`, srv.URL, name, part)
			}
			_, _ = fmt.Fprint(w, `</sitemapindex>`)
		default:
			_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s%s/page</loc></url></urlset>`, srv.URL, r.URL.Path)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	urlset := fmt.Sprintf(`<urlset><url><loc>%s/page</loc></url></urlset>`, srv.URL)

	tests := []struct {
		name        string
		multiThread bool
		path        string
		// content is handed to Parse, nil has it fetched. A call that fetches
		// nothing is cheap, so it is repeated far more often.
		content  *string
		calls    int
		wantURLs int64
	}{
		{"robots.txt, multi-thread", true, "/robots.txt", nil, 10, 6},
		{"robots.txt, sequential", false, "/robots.txt", nil, 10, 6},
		{"sitemap index, multi-thread", true, "/index-0.xml", nil, 10, 2},
		{"sitemap index, sequential", false, "/index-0.xml", nil, 10, 2},
		{"passed content, multi-thread", true, "/sitemap.xml", &urlset, 500, 1},
		{"passed content, sequential", false, "/sitemap.xml", &urlset, 500, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New().SetMultiThread(tt.multiThread).SetHTTPClient(client)

			stop := make(chan struct{})
			var wg sync.WaitGroup
			defer func() {
				close(stop)
				wg.Wait()
			}()

			// The two settings that Parse reads while it is fetching get a
			// goroutine of their own, so that they are written as often as
			// possible. Both goroutines yield after every round: spinning
			// freely they would starve Parse where only one CPU is available.
			wg.Add(2)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						s.SetMultiThread(tt.multiThread).SetMaxDepth(10)
						runtime.Gosched()
					}
				}
			}()
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						s.SetUserAgent("test-agent").
							SetFetchTimeout(3).
							SetMaxResponseSize(defaultMaxResponseSize).
							SetMaxConcurrency(defaultMaxConcurrency).
							SetFollow([]string{`\.xml$`}).
							SetRules([]string{`/page$`}).
							SetHTTPClient(client).
							SetStrict(false)
						_ = s.GetUserAgent()
						_ = s.GetFetchTimeout()
						_ = s.GetMultiThread()
						_ = s.GetMaxResponseSize()
						_ = s.GetMaxDepth()
						_ = s.GetMaxConcurrency()
						_ = s.GetFollow()
						_ = s.GetRules()
						_ = s.GetHTTPClient()
						_ = s.GetStrict()
						_ = s.GetURLs()
						_ = s.GetURLCount()
						_ = s.GetRandomURLs(1)
						_ = s.GetErrors()
						_ = s.GetErrorsCount()
						runtime.Gosched()
					}
				}
			}()

			for i := 0; i < tt.calls; i++ {
				requireParse(t, s, srv.URL+tt.path, tt.content)
				assertCounts(t, s, tt.wantURLs, 0)
			}
		})
	}
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
		{
			name: "non-empty URL list, n equals len(urls)",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       3,
			wantLen: 3,
		},
		{
			name: "non-empty URL list, n is zero",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       0,
			wantLen: 0,
		},
		{
			name: "non-empty URL list, n is negative",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       -1,
			wantLen: 0,
		},
		{
			name: "non-empty URL list, n is the smallest int",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       math.MinInt,
			wantLen: 0,
		},
		{
			name: "non-empty URL list, n is far greater than len(urls)",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       1_000_000,
			wantLen: 3,
		},
		{
			name: "non-empty URL list, n is the largest int",
			s: &S{
				urls: []URL{{}, {}, {}},
			},
			n:       math.MaxInt,
			wantLen: 3,
		},
		{
			name: "empty URL list, n is the largest int",
			s: &S{
				urls: []URL{},
			},
			n:       math.MaxInt,
			wantLen: 0,
		},
		{
			name:    "nil receiver, n is negative",
			s:       nil,
			n:       -1,
			wantLen: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.GetRandomURLs(test.n)

			if len(got) != test.wantLen {
				t.Errorf("GetRandomURLs() = %v, wantLen %v", len(got), test.wantLen)
			}
			if got == nil {
				t.Error("GetRandomURLs() = nil, want a non-nil slice")
			}
			// The result must be sized by the URLs available, not by n.
			if cap(got) > test.wantLen {
				t.Errorf("GetRandomURLs() capacity = %d, want at most %d", cap(got), test.wantLen)
			}
		})
	}

	t.Run("selects distinct URLs of the list", func(t *testing.T) {
		const count = 20
		urls := make([]URL, count)
		listed := make(map[string]bool, count)
		for i := range urls {
			urls[i].Loc = fmt.Sprintf("http://example.com/%d", i)
			listed[urls[i].Loc] = true
		}
		s := &S{urls: urls}

		for _, n := range []int{1, count / 2, count, count + 1, math.MaxInt} {
			got := s.GetRandomURLs(n)

			mustEqual(t, fmt.Sprintf("n=%d: length", n), len(got), min(n, count))
			selected := make(map[string]bool, len(got))
			for _, u := range got {
				if !listed[u.Loc] {
					t.Errorf("n=%d: %q is not a URL of the list", n, u.Loc)
				}
				if selected[u.Loc] {
					t.Errorf("n=%d: %q was selected more than once", n, u.Loc)
				}
				selected[u.Loc] = true
			}
		}
	})

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
			retURLContent, _, err := s.setContent(context.Background(), test.attrURLContent)
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
			name:   "robots.txt with UTF-8 BOM on the second line",
			input:  "Sitemap: https://example.com/first\n\ufeffSitemap: https://example.com/second",
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
			_, _, err := s.fetch(context.Background(), test.url)
			if (err != nil) != test.wantErr {
				t.Errorf("fetch() error = %v, wantErr %v", err, test.wantErr)
				return
			}
		})
	}
}

// TestS_fetch_ResponseSizeLimit verifies that a response is read up to the
// limit set with SetMaxResponseSize and rejected beyond it, and that the largest
// limit there is does not overflow: one byte past it there is no number, and a
// read limited to a negative number of bytes reads nothing.
func TestS_fetch_ResponseSizeLimit(t *testing.T) {
	const size = 1024
	body := strings.Repeat("A", size)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()

	tests := []struct {
		name    string
		limit   int64
		wantErr bool
	}{
		{"limit above the response size", 2 * size, false},
		{"limit equal to the response size", size, false},
		{"limit one byte below the response size", size - 1, true},
		{"limit far below the response size", 1, true},
		{"limit one below the maximum", math.MaxInt64 - 1, false},
		{"maximum limit does not overflow", math.MaxInt64, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New().SetMaxResponseSize(tt.limit)
			mustEqual(t, "errors", len(s.errs), 0)

			content, _, err := s.fetch(context.Background(), server.URL)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if content != body {
					t.Errorf("expected the %d bytes of the response, got %d bytes", size, len(content))
				}
				return
			}
			var netErr *NetworkError
			if !errors.As(err, &netErr) {
				t.Fatalf("expected *NetworkError, got %T: %v", err, err)
			}
			mustEqual(t, "error", err.Error(), fmt.Sprintf("fetch %q: response size exceeds limit of %d bytes", server.URL, tt.limit))
			mustEqual(t, "content", content, "")
		})
	}

	t.Run("response is read no further than one byte past the limit", func(t *testing.T) {
		const url = "https://example.com/sitemap.xml"
		const limit = 100 * 1024
		// The response does not end where the limit is, nor anywhere near it.
		source := &endless{}
		client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(source), Request: req}, nil
		})}
		s := New().SetMaxResponseSize(limit).SetHTTPClient(client)

		_, _, err := s.fetch(context.Background(), url)

		if err == nil {
			t.Fatal("expected a size limit error, got nil")
		}
		mustEqual(t, "error", err.Error(), fmt.Sprintf("fetch %q: response size exceeds limit of %d bytes", url, limit))
		mustEqual(t, "bytes read", source.read, int64(limit+1))
	})
}

// TestS_Parse_MaxResponseSize_Maximum verifies that the largest limit that can
// be set with SetMaxResponseSize lifts the limit rather than turning every
// response into an empty one: the documents of a call are read in full,
// compressed ones included.
func TestS_Parse_MaxResponseSize_Maximum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap-index.xml":
			_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>http://%[1]s/sitemap-1.xml</loc></sitemap><sitemap><loc>http://%[1]s/sitemap-2.xml.gz</loc></sitemap></sitemapindex>`, r.Host)
		case "/sitemap-1.xml":
			_, _ = fmt.Fprint(w, `<urlset><url><loc>https://example.com/page-1</loc></url></urlset>`)
		case "/sitemap-2.xml.gz":
			_, _ = w.Write(gzipByte(`<urlset><url><loc>https://example.com/page-2</loc></url></urlset>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, limit := range []int64{math.MaxInt64 - 1, math.MaxInt64} {
		t.Run(fmt.Sprintf("limit of %d bytes", limit), func(t *testing.T) {
			s := New().SetMultiThread(false).SetMaxResponseSize(limit)
			mustEqual(t, "GetMaxResponseSize", s.GetMaxResponseSize(), limit)
			requireParse(t, s, server.URL+"/sitemap-index.xml", nil)

			assertStringSlice(t, "errors", errorsOf(s), []string{})
			assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/page-1", "https://example.com/page-2"})
		})
	}
}

func TestS_fetch_NewRequestError(t *testing.T) {
	e := New()

	_, _, err := e.fetch(context.Background(), "://invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL but got none")
	}
}

func TestS_fetch_IOCopyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	_, _, err := e.fetch(context.Background(), server.URL)
	if err == nil {
		t.Error("expected io.Copy error but got none")
	}
}

func TestS_checkAndUnzipContent(t *testing.T) {
	const url = "https://example.com/sitemap.xml.gz"
	gzippedContent := string(gzipByte("test content"))

	tests := []struct {
		name    string
		content string
		want    string
		// wantErr is what is recorded about gzip content that cannot be unzipped. Nothing is
		// returned to be parsed then.
		wantErr string
	}{
		{
			name:    "Uncompressed data",
			content: "plain content",
			want:    "plain content",
		},
		{
			name:    "No data",
			content: "",
			want:    "",
		},
		{
			name:    "Gzipped data",
			content: gzippedContent,
			want:    "test content",
		},
		{
			name:    "Gzipped data of two members",
			content: gzippedContent + string(gzipByte(", and more")),
			want:    "test content, and more",
		},
		{
			name:    "Gzipped data that is empty",
			content: string(gzipByte("")),
			want:    "",
		},
		{
			name:    "Invalid data",
			content: "\x1f\x8b\x08" + "invalid", // gzip prefix + invalid content
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Gzipped data that is cut short",
			content: gzippedContent[:len(gzippedContent)-4],
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Gzipped data whose second member is cut short",
			content: gzippedContent + gzippedContent[:len(gzippedContent)-4],
			wantErr: "gzip decompression failed: unexpected EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &S{
				errs: []error{},
			}

			got, ok := s.checkAndUnzipContent(url, tt.content)

			if got != tt.want {
				t.Errorf("checkAndUnzipContent() got = %q, want %q", got, tt.want)
			}
			if tt.wantErr == "" {
				mustEqual(t, "ok", ok, true)
				mustEqual(t, "errors", len(s.errs), 0)
				return
			}
			mustEqual(t, "ok", ok, false)
			assertStringSlice(t, "errors", errorsOf(s), []string{fmt.Sprintf("parse %q: %s", url, tt.wantErr)})
			var parseErr *ParseError
			if len(s.errs) == 1 && !errors.As(s.errs[0], &parseErr) {
				t.Errorf("expected a *ParseError, got %T", s.errs[0])
			}
		})
	}
}

// requireSizeLimitError fails unless errs holds a *ParseError for url reporting
// that the decompressed size exceeded limit.
func requireSizeLimitError(t *testing.T, errs []error, url string, limit int64) {
	t.Helper()
	want := fmt.Sprintf("decompressed size exceeds limit of %d bytes", limit)
	for _, err := range errs {
		var parseErr *ParseError
		if errors.As(err, &parseErr) && parseErr.URL == url && strings.Contains(parseErr.Error(), want) {
			return
		}
	}
	t.Fatalf("expected a *ParseError for %q containing %q, got %v", url, want, errs)
}

func TestS_checkAndUnzipContent_SizeLimit(t *testing.T) {
	const url = "https://example.com/sitemap.xml.gz"
	payload := strings.Repeat("A", 64)
	gzipped := string(gzipByte(payload))

	t.Run("within limit", func(t *testing.T) {
		s := New().SetMaxResponseSize(64)
		got, ok := s.checkAndUnzipContent(url, gzipped)
		mustEqual(t, "ok", ok, true)
		mustEqual(t, "content", got, payload)
		mustEqual(t, "errors", len(s.errs), 0)
	})

	t.Run("exceeds limit", func(t *testing.T) {
		s := New().SetMaxResponseSize(63)
		got, ok := s.checkAndUnzipContent(url, gzipped)
		mustEqual(t, "ok", ok, false)
		mustEqual(t, "content", got, "")
		mustEqual(t, "errors", len(s.errs), 1)
		requireSizeLimitError(t, s.errs, url, 63)
	})

	t.Run("members exceed the limit together", func(t *testing.T) {
		s := New().SetMaxResponseSize(127)
		got, ok := s.checkAndUnzipContent(url, gzipped+gzipped)
		mustEqual(t, "ok", ok, false)
		mustEqual(t, "content", got, "")
		mustEqual(t, "errors", len(s.errs), 1)
		requireSizeLimitError(t, s.errs, url, 127)
	})

	t.Run("zero-value S falls back to the default limit", func(t *testing.T) {
		s := &S{}
		got, ok := s.checkAndUnzipContent(url, gzipped)
		mustEqual(t, "ok", ok, true)
		mustEqual(t, "content", got, payload)
		mustEqual(t, "errors", len(s.errs), 0)
	})
}

// TestS_Parse_GzipSizeLimit covers the decompression-bomb case end to end: a
// gzip payload small enough to pass the response size limit must not be allowed
// to expand beyond that same limit.
func TestS_Parse_GzipSizeLimit(t *testing.T) {
	const limit = 4096
	const lines = 4000
	payload := strings.Repeat("https://example.com/page\n", lines)
	gzipped := gzipByte(payload)
	if len(gzipped) >= limit || len(payload) <= limit {
		t.Fatalf("fixture must compress below the limit and expand beyond it: compressed %d, uncompressed %d, limit %d",
			len(gzipped), len(payload), limit)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gzipped)
	}))
	defer server.Close()
	url := server.URL + "/sitemap.txt.gz"

	t.Run("fetched content expanding beyond the limit is rejected", func(t *testing.T) {
		s := New().SetMaxResponseSize(limit)
		_, err := s.Parse(url, nil)
		mustEqual(t, "GetURLCount", s.GetURLCount(), 0)
		requireSizeLimitError(t, s.GetErrors(), url, limit)
		// The document Parse was called for cannot be parsed, so the call fails with it.
		requireSizeLimitError(t, []error{err}, url, limit)
	})

	t.Run("fetched content within the limit is parsed", func(t *testing.T) {
		s := New().SetMaxResponseSize(int64(len(payload)))
		requireParse(t, s, url, nil)
		assertCounts(t, s, lines, 0)
	})

	t.Run("supplied content expanding beyond the limit is rejected", func(t *testing.T) {
		const suppliedURL = "https://example.com/sitemap.txt.gz"
		content := string(gzipped)
		s := New().SetMaxResponseSize(limit)
		_, err := s.Parse(suppliedURL, &content)
		mustEqual(t, "GetURLCount", s.GetURLCount(), 0)
		requireSizeLimitError(t, s.GetErrors(), suppliedURL, limit)
		requireSizeLimitError(t, []error{err}, suppliedURL, limit)
	})
}

// gzipMembersOf returns content as gzip content of several members: content is cut every
// size bytes, and each piece is compressed on its own.
func gzipMembersOf(content string, size int) string {
	var members strings.Builder
	for len(content) > size {
		members.Write(gzipByte(content[:size]))
		content = content[size:]
	}
	members.Write(gzipByte(content))
	return members.String()
}

// TestS_Parse_GzipMembers verifies that a gzip document is read to the end of its last
// member, whatever its format and wherever its members end: every URL it lists is collected.
func TestS_Parse_GzipMembers(t *testing.T) {
	pages := []string{"https://example.com/page-1", "https://example.com/page-2", "https://example.com/page-3"}

	for path, document := range documentsListing(pages) {
		// Two members, one for about every URL, and one for every few bytes.
		for _, size := range []int{len(document)/2 + 1, len(document) / 3, 16} {
			for _, strict := range []bool{false, true} {
				for name, trailing := range map[string]string{"nothing": "", "a newline": "\n"} {
					t.Run(fmt.Sprintf("%s, members of %d bytes, strict=%v, %s after them", path, size, strict, name), func(t *testing.T) {
						content := gzipMembersOf(document, size) + trailing
						if members := strings.Count(content, gzipPrefix); members < 2 {
							t.Fatalf("fixture must hold several members, got %d", members)
						}

						s := New().SetStrict(strict)
						requireParse(t, s, "https://example.com"+path+".gz", &content)

						assertStringSlice(t, "URLs", locsOf(s), pages)
						assertStringSlice(t, "errors", errorsOf(s), []string{})
					})
				}
			}
		}
	}
}

// TestS_Parse_GzipMembers_Fetched verifies the same for documents that are fetched, a
// sitemap index and the sitemaps it lists, and that a document with a member that cannot be
// read is reported once and yields nothing: neither is it parsed for what its other members
// hold, nor reported a second time for not being a sitemap.
func TestS_Parse_GzipMembers_Fetched(t *testing.T) {
	const firstPages = `<urlset><url><loc>https://example.com/page-1</loc></url><url><loc>https://example.com/page-2</loc></url></urlset>`
	// A file that was appended to: a member for every line.
	lines := []string{"https://example.com/page-3\n", "https://example.com/page-4\n", "https://example.com/page-5\n"}
	var appended strings.Builder
	for _, line := range lines {
		appended.Write(gzipByte(line))
	}
	damaged := gzipMembersOf(firstPages, len(firstPages)/2)
	damaged = damaged[:len(damaged)-1]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := func(paths ...string) string {
			var sitemaps strings.Builder
			for _, path := range paths {
				_, _ = fmt.Fprintf(&sitemaps, "<sitemap><loc>http://%s%s</loc></sitemap>", r.Host, path)
			}
			return "<sitemapindex>" + sitemaps.String() + "</sitemapindex>"
		}

		switch r.URL.Path {
		case "/index.xml.gz":
			_, _ = fmt.Fprint(w, gzipMembersOf(index("/pages.xml.gz", "/appended.txt.gz"), 40))
		case "/index-damaged.xml.gz":
			_, _ = fmt.Fprint(w, gzipMembersOf(index("/damaged.xml.gz", "/appended.txt.gz"), 40))
		case "/pages.xml.gz":
			_, _ = fmt.Fprint(w, gzipMembersOf(firstPages, len(firstPages)/2))
		case "/appended.txt.gz":
			_, _ = fmt.Fprint(w, appended.String())
		case "/damaged.xml.gz":
			_, _ = fmt.Fprint(w, damaged)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	wantDamaged := fmt.Sprintf("parse %q: gzip decompression failed: unexpected EOF", server.URL+"/damaged.xml.gz")
	appendedPages := []string{"https://example.com/page-3", "https://example.com/page-4", "https://example.com/page-5"}

	for _, multiThread := range []bool{false, true} {
		t.Run(fmt.Sprintf("every member of every document is read, multiThread=%v", multiThread), func(t *testing.T) {
			s := New().SetMultiThread(multiThread)
			requireParse(t, s, server.URL+"/index.xml.gz", nil)

			assertStringSlice(t, "URLs", sortedCopy(locsOf(s)), append([]string{"https://example.com/page-1", "https://example.com/page-2"}, appendedPages...))
			assertStringSlice(t, "errors", errorsOf(s), []string{})
		})

		t.Run(fmt.Sprintf("a damaged member fails the document, multiThread=%v", multiThread), func(t *testing.T) {
			s := New().SetMultiThread(multiThread)
			_, err := s.Parse(server.URL+"/damaged.xml.gz", nil)

			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("expected a *ParseError to be returned, got %T: %v", err, err)
			}
			mustEqual(t, "error", err.Error(), wantDamaged)
			// The first member holds a page, and could be read.
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{wantDamaged})
		})

		t.Run(fmt.Sprintf("a damaged member of a listed sitemap is reported once, multiThread=%v", multiThread), func(t *testing.T) {
			s := New().SetMultiThread(multiThread)
			requireParse(t, s, server.URL+"/index-damaged.xml.gz", nil)

			assertStringSlice(t, "URLs", locsOf(s), appendedPages)
			assertStringSlice(t, "errors", errorsOf(s), []string{wantDamaged})
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
			s.parseAndFetchUrlsMultiThread(context.Background(), server.URL+"/index.xml", test.locations, 0)

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
			s.parseAndFetchUrlsSequential(context.Background(), server.URL+"/index.xml", test.locations, 0)

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
	index := server.URL + "/index.xml"
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsMultiThread(context.Background(), index, locations, 1)

	if len(s.urls) != 0 {
		t.Errorf("expected 0 URLs at depth limit, got %d", len(s.urls))
	}
	requireDepthLimitErrors(t, s.GetErrors(), 1, index)
}

func TestS_parseAndFetchUrlsSequential_MaxDepth(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := New().SetMaxDepth(1).SetMultiThread(false)
	index := server.URL + "/index.xml"
	locations := []string{fmt.Sprintf("%s/sitemapindex-1.xml", server.URL)}
	s.parseAndFetchUrlsSequential(context.Background(), index, locations, 1)

	if len(s.urls) != 0 {
		t.Errorf("expected 0 URLs at depth limit, got %d", len(s.urls))
	}
	requireDepthLimitErrors(t, s.GetErrors(), 1, index)
}

// requireDepthLimitErrors verifies that errs holds nothing but the errors of
// the depth limit being reached, one for each of the documents given: the
// documents whose sitemaps were not followed.
func requireDepthLimitErrors(t *testing.T, errs []error, maxDepth int, documents ...string) {
	t.Helper()

	var got []string
	for _, err := range errs {
		var parseErr *ParseError
		if !errors.As(err, &parseErr) {
			t.Fatalf("expected *ParseError, got %T: %v", err, err)
		}
		mustEqual(t, "error", parseErr.Err.Error(), fmt.Sprintf("max recursion depth of %d reached", maxDepth))
		got = append(got, parseErr.URL)
	}
	assertStringSlice(t, "documents named by the depth limit errors", sortedCopy(got), sortedCopy(documents))
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

// encodedDocuments returns one document of every XML format the parser reads.
// Each of them declares encoding in its XML declaration and holds a single
// location, whose last path segment is word. The caller passes word in the
// bytes of that encoding.
func encodedDocuments(encoding, word string) map[string]string {
	templates := map[string]string{
		"sitemapindex": `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>https://example.com/%s</loc></sitemap></sitemapindex>`,
		"urlset":       `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/%s</loc></url></urlset>`,
		"rss":          `<rss version="2.0"><channel><item><link>https://example.com/%s</link></item></channel></rss>`,
		"feed":         `<feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="https://example.com/%s"/></entry></feed>`,
	}

	documents := make(map[string]string, len(templates))
	for format, template := range templates {
		documents[format] = fmt.Sprintf(`<?xml version="1.0" encoding="%s"?>`, encoding) + fmt.Sprintf(template, word)
	}
	return documents
}

// parsedLocations runs parse over content and returns every location it
// yields: the sitemaps to follow for a sitemap index, the URLs otherwise.
func parsedLocations(s *S, content string) []string {
	locations := append([]string(nil), s.parse("https://example.com/sitemap.xml", content)...)
	for _, u := range s.urls {
		locations = append(locations, u.Loc)
	}
	return locations
}

func TestDetectRootElement(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"no XML declaration", `<urlset></urlset>`, "urlset"},
		{"UTF-8 declared", `<?xml version="1.0" encoding="UTF-8"?><sitemapindex/>`, "sitemapindex"},
		{"comment before the root element", `<?xml version="1.0"?><!-- comment --><rss/>`, "rss"},
		{"namespace prefix", `<a:feed xmlns:a="http://www.w3.org/2005/Atom"/>`, "feed"},
		{"ISO-8859-1 declared", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><!-- caf\xe9 --><urlset/>", "urlset"},
		{"windows-1252 declared", "<?xml version=\"1.0\" encoding=\"windows-1252\"?><rss title=\"\x80\"/>", "rss"},
		{"US-ASCII declared", `<?xml version="1.0" encoding="US-ASCII"?><feed/>`, "feed"},
		{"unquoted attribute value", `<urlset version=1></urlset>`, "urlset"},
		{"unsupported encoding declared", `<?xml version="1.0" encoding="IBM437"?><urlset/>`, "urlset"},
		{"plain text", "https://example.com/page", ""},
		{"malformed XML", "<<<<<<", ""},
		{"empty", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mustEqual(t, "root element", detectRootElement(test.content), test.want)
		})
	}
}

// TestS_parse_DeclaredEncoding verifies that a document declaring an encoding
// other than UTF-8 is recognised and transcoded, whichever XML format it is in.
func TestS_parse_DeclaredEncoding(t *testing.T) {
	tests := []struct {
		encoding string
		// word is a path segment in the bytes of the encoding, escaped is the
		// same segment as it appears in the parsed location.
		word    string
		escaped string
	}{
		{"UTF-8", "caf\xc3\xa9", "caf%C3%A9"},
		{"utf8", "caf\xc3\xa9", "caf%C3%A9"},
		{"US-ASCII", "cafe", "cafe"},
		{"ISO-8859-1", "caf\xe9", "caf%C3%A9"},
		{"iso-8859-1", "caf\xe9", "caf%C3%A9"},
		{"latin1", "caf\xe9", "caf%C3%A9"},
		{"windows-1252", "\x80uro", "%E2%82%ACuro"},
		{"ISO-8859-2", "t\xfbr\xf5", "t%C5%B1r%C5%91"},
		{"windows-1250", "t\xfbr\xf5", "t%C5%B1r%C5%91"},
	}

	for _, test := range tests {
		for format, content := range encodedDocuments(test.encoding, test.word) {
			t.Run(test.encoding+" "+format, func(t *testing.T) {
				s := New()
				locations := parsedLocations(s, content)

				if len(s.errs) != 0 {
					t.Fatalf("unexpected errors: %v", s.errs)
				}
				if len(locations) != 1 {
					t.Fatalf("expected 1 location, got %d: %q", len(locations), locations)
				}
				mustEqual(t, "location", locations[0], "https://example.com/"+test.escaped)
			})
		}
	}
}

// TestS_parse_UnsupportedEncoding verifies that a document declaring an
// encoding that cannot be transcoded is reported as such, rather than as a
// document of an unknown format.
func TestS_parse_UnsupportedEncoding(t *testing.T) {
	const url = "https://example.com/sitemap.xml"

	for format, content := range encodedDocuments("IBM437", "cafe") {
		t.Run(format, func(t *testing.T) {
			s := New()
			locations := parsedLocations(s, content)

			if len(locations) != 0 {
				t.Errorf("expected no locations, got %q", locations)
			}
			if len(s.errs) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(s.errs), s.errs)
			}
			var parseErr *ParseError
			if !errors.As(s.errs[0], &parseErr) {
				t.Fatalf("expected *ParseError, got %T: %v", s.errs[0], s.errs[0])
			}
			mustEqual(t, "error URL", parseErr.URL, url)
			if !strings.Contains(parseErr.Error(), `"IBM437"`) {
				t.Errorf("error does not name the encoding: %v", parseErr)
			}
			if strings.Contains(parseErr.Error(), "unrecognized sitemap format") {
				t.Errorf("document was not recognised: %v", parseErr)
			}
		})
	}
}

// TestS_Parse_DeclaredEncoding verifies the whole path for a fetched document
// in an encoding other than UTF-8: the text it contains comes back as UTF-8.
func TestS_Parse_DeclaredEncoding(t *testing.T) {
	const (
		title     = "Árvíztűrő tükörfúrógép"
		isoLatin2 = "\xc1rv\xedzt\xfbr\xf5 t\xfck\xf6rf\xfar\xf3g\xe9p"
	)
	content := `<?xml version="1.0" encoding="ISO-8859-2"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url>
    <loc>/page</loc>
    <image:image>
      <image:loc>/image.jpg</image:loc>
      <image:title>` + isoLatin2 + `</image:title>
    </image:image>
  </url>
</urlset>`

	tests := []struct {
		name string
		body []byte
	}{
		{"plain", []byte(content)},
		{"gzip", gzipByte(content)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(test.body)
			}))
			defer server.Close()

			s := New()
			requireParse(t, s, server.URL+"/sitemap.xml", nil)
			assertCounts(t, s, 1, 0)

			u := s.GetURLs()[0]
			mustEqual(t, "location", u.Loc, server.URL+"/page")
			if len(u.Images) != 1 {
				t.Fatalf("expected 1 image, got %d", len(u.Images))
			}
			mustEqual(t, "image title", u.Images[0].Title, title)
		})
	}
}

// TestS_Parse_ByteOrderMark verifies that the UTF-8 byte order mark a document
// begins with is no part of its content, whatever the format of the document
// and whether it is compressed or not: every URL it lists is collected, the
// first one included.
func TestS_Parse_ByteOrderMark(t *testing.T) {
	pages := []string{"https://example.com/page-1", "https://example.com/page-2", "https://example.com/page-3"}

	for path, document := range documentsListing(pages) {
		for _, compressed := range []bool{false, true} {
			for _, strict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, compressed=%v, strict=%v", path, compressed, strict), func(t *testing.T) {
					content := "\ufeff" + document
					if compressed {
						content = string(gzipByte(content))
					}

					s := New().SetStrict(strict)
					requireParse(t, s, "https://example.com"+path, &content)

					assertStringSlice(t, "URLs", locsOf(s), pages)
					if errs := s.GetErrors(); len(errs) != 0 {
						t.Errorf("unexpected errors: %v", errs)
					}
				})
			}
		}
	}
}

// TestS_Parse_ByteOrderMark_Text verifies how a text sitemap that begins with
// a UTF-8 byte order mark is read: the mark is taken off, and the first line
// is read like any other line.
func TestS_Parse_ByteOrderMark_Text(t *testing.T) {
	const (
		bom        = "\ufeff"
		sitemapURL = "https://example.com/sitemap.txt"
		first      = "https://example.com/first"
		second     = "https://example.com/second"
		unknown    = `unrecognized sitemap format (root element: "")`
	)

	tests := []struct {
		name    string
		content string
		want    []string
		// wantErr tells what is wrong with a document that lists no URL, which
		// fails the call. It is empty for a document that is a sitemap.
		wantErr string
	}{
		{"single URL", bom + first + "\n", []string{first}, ""},
		{"single URL without line end", bom + first, []string{first}, ""},
		{"several URLs", bom + first + "\n" + second + "\n", []string{first, second}, ""},
		{"CRLF line ends", bom + first + "\r\n" + second + "\r\n", []string{first, second}, ""},
		{"whitespace before the first URL", bom + " \t" + first + "\n" + second + "\n", []string{first, second}, ""},
		{"comment on the first line", bom + "# pages\n" + first + "\n" + second + "\n", []string{first, second}, ""},
		{"empty first line", bom + "\n" + first + "\n" + second + "\n", []string{first, second}, ""},
		// The mark is not content: a document of nothing else is empty.
		{"nothing but the mark", bom, []string{}, "sitemap content is empty"},
		{"empty line only", bom + "\n", []string{}, unknown},
		{"no URL", bom + "no URL here\n", []string{}, unknown},
		// Only what the document begins with is a byte order mark, and only once.
		// Anywhere else it is a character like any other one, and a line that
		// begins with it is no URL.
		{"mark on the second line", first + "\n" + bom + second + "\n", []string{first}, ""},
		{"mark after whitespace", " " + bom + first + "\n" + second + "\n", []string{second}, ""},
		{"second mark", bom + bom + first + "\n" + second + "\n", []string{second}, ""},
	}

	for _, test := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", test.name, strict), func(t *testing.T) {
				content := test.content
				s := New().SetStrict(strict)
				_, err := s.Parse(sitemapURL, &content)

				assertStringSlice(t, "URLs", locsOf(s), test.want)
				errs := s.GetErrors()
				if test.wantErr == "" {
					if err != nil || len(errs) != 0 {
						t.Fatalf("unexpected errors: %v, %v", err, errs)
					}
					return
				}

				if len(errs) != 1 {
					t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
				}
				var parseErr *ParseError
				if !errors.As(errs[0], &parseErr) {
					t.Fatalf("expected *ParseError, got %T: %v", errs[0], errs[0])
				}
				mustEqual(t, "error", parseErr.Error(), fmt.Sprintf("parse %q: %s", sitemapURL, test.wantErr))
				if err != errs[0] {
					t.Errorf("Parse returned %v, expected the error recorded: %v", err, errs[0])
				}
			})
		}
	}
}

// TestS_Parse_ByteOrderMark_Fetched verifies the whole path for fetched
// documents that begin with a UTF-8 byte order mark: a robots.txt lists a
// sitemap index, which lists two text sitemaps, one of them compressed.
func TestS_Parse_ByteOrderMark_Fetched(t *testing.T) {
	const bom = "\ufeff"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site := "http://" + r.Host
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = fmt.Fprintf(w, bom+"Sitemap: %s/sitemap-index.xml\n", site)
		case "/sitemap-index.xml":
			_, _ = fmt.Fprintf(w, bom+`<sitemapindex><sitemap><loc>%[1]s/pages.txt</loc></sitemap><sitemap><loc>%[1]s/posts.txt.gz</loc></sitemap></sitemapindex>`, site)
		case "/pages.txt":
			_, _ = fmt.Fprintf(w, bom+"%[1]s/page-1\n%[1]s/page-2\n", site)
		case "/posts.txt.gz":
			_, _ = w.Write(gzipByte(fmt.Sprintf(bom+"%[1]s/post-1\n%[1]s/post-2\n", site)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := New()
	requireParse(t, s, server.URL+"/robots.txt", nil)

	assertStringSlice(t, "URLs", sortedCopy(locsOf(s)), []string{
		server.URL + "/page-1",
		server.URL + "/page-2",
		server.URL + "/post-1",
		server.URL + "/post-2",
	})
	if errs := s.GetErrors(); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

// TestS_Parse_InvalidValues verifies that an element whose content cannot be
// parsed costs only itself: the rest of its entry and the rest of the document
// are kept. Strict mode skips the entry when the element is one of the <url>
// itself, and keeps it when the element belongs to an extension.
func TestS_Parse_InvalidValues(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		secondURL  = "https://example.com/second"
	)

	// document returns a urlset of three entries, the second of which holds
	// the given elements next to a valid <changefreq>.
	document := func(elements string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
  <url><loc>https://example.com/first</loc><lastmod>2024-01-15</lastmod></url>
  <url><loc>` + secondURL + `</loc><changefreq>daily</changefreq>` + elements + `</url>
  <url><loc>https://example.com/third</loc><priority>0.8</priority></url>
</urlset>`
	}
	// video and news return an extension entry that satisfies strict mode,
	// apart from the given element.
	video := func(element string) string {
		return `<video:video>
  <video:thumbnail_loc>https://example.com/thumb.jpg</video:thumbnail_loc>
  <video:title>Video title</video:title>
  <video:description>Video description</video:description>
  <video:content_loc>https://example.com/video.mp4</video:content_loc>` + element + `</video:video>`
	}
	news := func(element string) string {
		return `<news:news>
  <news:publication><news:name>Example News</news:name><news:language>en</news:language></news:publication>
  <news:title>News title</news:title>` + element + `</news:news>`
	}
	videoOf := func(t *testing.T, u URL) Video {
		t.Helper()
		if len(u.Videos) != 1 {
			t.Fatalf("expected 1 video, got %d", len(u.Videos))
		}
		mustEqual(t, "video title", u.Videos[0].Title, "Video title")
		return u.Videos[0]
	}

	tests := []struct {
		name     string
		elements string
		// message is the text of the error the invalid value is reported with.
		message string
		// own tells that the element belongs to the <url> itself.
		own bool
		// unset reports whether the field of the invalid element is left unset.
		unset func(t *testing.T, u URL) bool
	}{
		{
			name:     "lastmod without time zone",
			elements: `<lastmod>2024-01-15T10:30:00</lastmod>`,
			message:  `invalid <lastmod> value "2024-01-15T10:30:00"`,
			own:      true,
			unset:    func(_ *testing.T, u URL) bool { return u.LastMod == nil },
		},
		{
			name:     "lastmod with zone offset without colon",
			elements: `<lastmod>2024-01-15T10:30:00+0100</lastmod>`,
			message:  `invalid <lastmod> value "2024-01-15T10:30:00+0100"`,
			own:      true,
			unset:    func(_ *testing.T, u URL) bool { return u.LastMod == nil },
		},
		{
			name:     "lastmod with space separator",
			elements: `<lastmod> 2024-01-15 10:30:00 </lastmod>`,
			message:  `invalid <lastmod> value "2024-01-15 10:30:00"`,
			own:      true,
			unset:    func(_ *testing.T, u URL) bool { return u.LastMod == nil },
		},
		{
			name:     "priority with decimal comma",
			elements: `<priority>0,5</priority>`,
			message:  `invalid <priority> value "0,5"`,
			own:      true,
			unset:    func(_ *testing.T, u URL) bool { return u.Priority == nil },
		},
		{
			name:     "video duration",
			elements: video(`<video:duration>1:30</video:duration>`),
			message:  `invalid video <duration> value "1:30"`,
			unset:    func(t *testing.T, u URL) bool { return videoOf(t, u).Duration == nil },
		},
		{
			name:     "video rating",
			elements: video(`<video:rating>five</video:rating>`),
			message:  `invalid video <rating> value "five"`,
			unset:    func(t *testing.T, u URL) bool { return videoOf(t, u).Rating == nil },
		},
		{
			name:     "video view count",
			elements: video(`<video:view_count>1,234</video:view_count>`),
			message:  `invalid video <view_count> value "1,234"`,
			unset:    func(t *testing.T, u URL) bool { return videoOf(t, u).ViewCount == nil },
		},
		{
			name:     "video expiration date",
			elements: video(`<video:expiration_date>next year</video:expiration_date>`),
			message:  `invalid video <expiration_date> value "next year"`,
			unset:    func(t *testing.T, u URL) bool { return videoOf(t, u).ExpirationDate == nil },
		},
		{
			name:     "video publication date",
			elements: video(`<video:publication_date>15/01/2024</video:publication_date>`),
			message:  `invalid video <publication_date> value "15/01/2024"`,
			unset:    func(t *testing.T, u URL) bool { return videoOf(t, u).PublicationDate == nil },
		},
		{
			name:     "news publication date",
			elements: news(`<news:publication_date>yesterday</news:publication_date>`),
			message:  `invalid news <publication_date> value "yesterday"`,
			unset: func(t *testing.T, u URL) bool {
				t.Helper()
				if u.News == nil {
					t.Fatal("expected News to be non-nil")
				}
				mustEqual(t, "news title", u.News.Title, "News title")
				return u.News.PublicationDate == nil
			},
		},
	}

	for _, test := range tests {
		for _, strict := range []bool{false, true} {
			mode := "tolerant"
			if strict {
				mode = "strict"
			}
			t.Run(test.name+" "+mode, func(t *testing.T) {
				content := document(test.elements)
				s := New().SetStrict(strict)
				requireParse(t, s, sitemapURL, &content)

				skipped := strict && test.own
				wantURLs := int64(3)
				if skipped {
					wantURLs = 2
				}
				assertCounts(t, s, wantURLs, 1)

				var valErr *ValidationError
				if !errors.As(s.GetErrors()[0], &valErr) {
					t.Fatalf("expected *ValidationError, got %T: %v", s.GetErrors()[0], s.GetErrors()[0])
				}
				mustEqual(t, "error URL", valErr.URL, secondURL)
				mustEqual(t, "error message", valErr.Err.Error(), test.message)

				urls := s.GetURLs()
				mustEqual(t, "first location", urls[0].Loc, "https://example.com/first")
				if urls[0].LastMod == nil {
					t.Error("first entry lost its <lastmod>")
				}
				last := urls[len(urls)-1]
				mustEqual(t, "last location", last.Loc, "https://example.com/third")
				assertPtrFloat32(t, "last priority", last.Priority, 0.8)

				if skipped {
					return
				}
				second := urls[1]
				mustEqual(t, "second location", second.Loc, secondURL)
				if second.ChangeFreq == nil || *second.ChangeFreq != ChangeFreqDaily {
					t.Errorf("second entry lost its <changefreq>: %v", second.ChangeFreq)
				}
				if !test.unset(t, second) {
					t.Error("field of the invalid element is set")
				}
			})
		}
	}

	t.Run("every invalid value of an entry is reported", func(t *testing.T) {
		content := document(`<lastmod>never</lastmod><priority>high</priority>` + video(`<video:duration>long</video:duration>`))
		want := []string{
			`validate "` + secondURL + `": invalid <lastmod> value "never"`,
			`validate "` + secondURL + `": invalid <priority> value "high"`,
			`validate "` + secondURL + `": invalid video <duration> value "long"`,
		}

		for _, strict := range []bool{false, true} {
			s := New().SetStrict(strict)
			requireParse(t, s, sitemapURL, &content)

			wantURLs := int64(3)
			if strict {
				wantURLs = 2
			}
			assertCounts(t, s, wantURLs, int64(len(want)))
			for i, err := range s.GetErrors() {
				mustEqual(t, fmt.Sprintf("strict=%v error %d", strict, i), err.Error(), want[i])
			}
		}
	})

	t.Run("values of an entry skipped for its location are not reported", func(t *testing.T) {
		content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>ftp://example.com/page</loc><lastmod>never</lastmod></url>
</urlset>`
		s := New()
		requireParse(t, s, sitemapURL, &content)
		assertCounts(t, s, 0, 1)
		if !strings.Contains(s.GetErrors()[0].Error(), "unsupported scheme") {
			t.Errorf("unexpected error: %v", s.GetErrors()[0])
		}
	})

	t.Run("empty elements are not invalid", func(t *testing.T) {
		content := document(`<lastmod> </lastmod><priority> </priority>` +
			video(`<video:duration></video:duration><video:rating/><video:view_count> </video:view_count>`))

		s := New()
		requireParse(t, s, sitemapURL, &content)
		assertCounts(t, s, 3, 0)

		second := s.GetURLs()[1]
		// An empty date is no date, see TestS_Parse_EmptyDates. An empty number reads as
		// zero, as it does when encoding/xml decodes it.
		if second.LastMod != nil {
			t.Errorf("lastmod: got %v, want nil", second.LastMod)
		}
		assertPtrFloat32(t, "priority", second.Priority, 0)
		v := videoOf(t, second)
		assertPtrInt(t, "duration", v.Duration, 0)
		assertPtrFloat32(t, "rating", v.Rating, 0)
		assertPtrInt(t, "view count", v.ViewCount, 0)
	})
}

// TestS_Parse_EmptyDates verifies that a date element that is there but holds
// nothing is read as an absent one: its field is nil, not the zero time. It is
// not reported either, except where strict mode requires the date.
func TestS_Parse_EmptyDates(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		pageURL    = "https://example.com/page"
	)

	// document returns a urlset of one entry that has news and a video, both of which
	// satisfy strict mode apart from their dates. The four date elements of the entry
	// are what element returns for their names.
	document := func(element func(name string) string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
  <url>
    <loc>` + pageURL + `</loc>` + element("lastmod") + `
    <news:news>
      <news:publication><news:name>Example News</news:name><news:language>en</news:language></news:publication>` + element("news:publication_date") + `
      <news:title>News title</news:title>
    </news:news>
    <video:video>
      <video:thumbnail_loc>https://example.com/thumb.jpg</video:thumbnail_loc>
      <video:title>Video title</video:title>
      <video:description>Video description</video:description>
      <video:content_loc>https://example.com/video.mp4</video:content_loc>` + element("video:expiration_date") + element("video:publication_date") + `
    </video:video>
  </url>
</urlset>`
	}
	// as returns the element function that writes every date element in the given
	// format, which takes the name of the element.
	as := func(format string) func(string) string {
		return func(name string) string {
			return fmt.Sprintf(format, name)
		}
	}
	// parse parses the document with a new instance and returns the four dates of its
	// entry, by the name of their fields, and the errors recorded.
	parse := func(t *testing.T, strict bool, content string) (map[string]*LastModTime, []string) {
		t.Helper()

		s := New().SetStrict(strict)
		requireParse(t, s, sitemapURL, &content)
		urls := s.GetURLs()
		if len(urls) != 1 || urls[0].News == nil || len(urls[0].Videos) != 1 {
			t.Fatalf("expected one URL with news and a video, got %+v, errors: %v", urls, s.GetErrors())
		}
		mustEqual(t, "news title", urls[0].News.Title, "News title")
		mustEqual(t, "video title", urls[0].Videos[0].Title, "Video title")

		errs := []string{}
		for _, err := range s.GetErrors() {
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Errorf("expected a *ValidationError, got %T: %v", err, err)
				continue
			}
			mustEqual(t, "URL of the error", validationErr.URL, pageURL)
			errs = append(errs, validationErr.Err.Error())
		}
		return map[string]*LastModTime{
			"LastMod":               urls[0].LastMod,
			"News.PublicationDate":  urls[0].News.PublicationDate,
			"Video.ExpirationDate":  urls[0].Videos[0].ExpirationDate,
			"Video.PublicationDate": urls[0].Videos[0].PublicationDate,
		}, errs
	}

	emptyElements := []struct {
		name   string
		format string
	}{
		{"start and end tag", "<%[1]s></%[1]s>"},
		{"empty-element tag", "<%[1]s/>"},
		{"space", "<%[1]s> </%[1]s>"},
		{"line break and indentation", "<%[1]s>\r\n\t  </%[1]s>"},
		{"empty CDATA section", "<%[1]s><![CDATA[]]></%[1]s>"},
		{"CDATA section of whitespace", "<%[1]s><![CDATA[ \n]]></%[1]s>"},
		{"comment", "<%[1]s><!-- not known --></%[1]s>"},
		{"character reference of a space", "<%[1]s>&#32;</%[1]s>"},
		{"character reference of a no-break space", "<%[1]s>&#160;</%[1]s>"},
	}
	for _, empty := range emptyElements {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", empty.name, strict), func(t *testing.T) {
				dates, errs := parse(t, strict, document(as(empty.format)))

				for field, date := range dates {
					if date != nil {
						t.Errorf("%s: expected nil, got %v", field, date.Time)
					}
				}
				// The dates are optional but for the one of the news, which strict mode
				// requires.
				want := []string{}
				if strict {
					want = append(want, "strict mode: news <publication_date> is empty")
				}
				assertStringSlice(t, "errors", errs, want)
			})
		}
	}

	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("absent, strict=%v", strict), func(t *testing.T) {
			dates, errs := parse(t, strict, document(func(string) string { return "" }))

			for field, date := range dates {
				if date != nil {
					t.Errorf("%s: expected nil, got %v", field, date.Time)
				}
			}
			want := []string{}
			if strict {
				want = append(want, "strict mode: news <publication_date> is missing")
			}
			assertStringSlice(t, "errors", errs, want)
		})

		t.Run(fmt.Sprintf("date, strict=%v", strict), func(t *testing.T) {
			dates, errs := parse(t, strict, document(as("<%[1]s>\n  2024-01-15\n</%[1]s>")))

			for field, date := range dates {
				if date == nil || !date.Equal(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)) {
					t.Errorf("%s: expected 2024-01-15, got %v", field, date)
				}
			}
			assertStringSlice(t, "errors", errs, []string{})
		})

		// A field that is set holds what the document gives, be that the zero time: an
		// empty element is told from it by the field being nil.
		t.Run(fmt.Sprintf("first day of the year 1, strict=%v", strict), func(t *testing.T) {
			dates, errs := parse(t, strict, document(as("<%[1]s>0001-01-01</%[1]s>")))

			for field, date := range dates {
				if date == nil || !date.IsZero() {
					t.Errorf("%s: expected the zero time, got %v", field, date)
				}
			}
			assertStringSlice(t, "errors", errs, []string{})
		})

		// Only the extensions hold a date that is no date here: strict mode skips an
		// entry whose own <lastmod> is none.
		t.Run(fmt.Sprintf("no date, strict=%v", strict), func(t *testing.T) {
			dates, errs := parse(t, strict, document(func(name string) string {
				if name == "lastmod" {
					return ""
				}
				return fmt.Sprintf("<%[1]s>yesterday</%[1]s>", name)
			}))

			for field, date := range dates {
				if date != nil {
					t.Errorf("%s: expected nil, got %v", field, date.Time)
				}
			}
			// The date of the news is reported as what it is, and as nothing else.
			assertStringSlice(t, "errors", errs, []string{
				`invalid news <publication_date> value "yesterday"`,
				`invalid video <expiration_date> value "yesterday"`,
				`invalid video <publication_date> value "yesterday"`,
			})
		})
	}

	t.Run("empty date among dates", func(t *testing.T) {
		dates, errs := parse(t, true, document(func(name string) string {
			if name == "video:expiration_date" {
				return "<video:expiration_date/>"
			}
			return fmt.Sprintf("<%[1]s>2024-01-15</%[1]s>", name)
		}))

		for field, date := range dates {
			if (date == nil) != (field == "Video.ExpirationDate") {
				t.Errorf("%s: got %v", field, date)
			}
		}
		assertStringSlice(t, "errors", errs, []string{})
	})
}

// TestS_parse_MalformedXML verifies how the two modes treat XML that is not
// well-formed: tolerant mode reads past the mistakes encoding/xml can recover
// from, strict mode rejects the document.
func TestS_parse_MalformedXML(t *testing.T) {
	const url = "https://example.com/sitemap.xml"

	type document struct {
		name    string
		content string
		// locations are the locations tolerant mode reads from the document.
		locations []string
	}
	var documents []document
	for format, content := range encodedDocuments("UTF-8", "page?a=1&b=2") {
		documents = append(documents, document{format + " unescaped ampersand", content, []string{"https://example.com/page?a=1&b=2"}})
	}
	for format, content := range encodedDocuments("UTF-8", "page?q=a&nbsp;b") {
		documents = append(documents, document{format + " unknown entity", content, []string{"https://example.com/page?q=a&nbsp;b"}})
	}
	documents = append(documents,
		document{
			name:      "missing end tag",
			content:   `<urlset><url><loc>https://example.com/a</loc><lastmod>2024-01-15</url><url><loc>https://example.com/b</loc></url></urlset>`,
			locations: []string{"https://example.com/a", "https://example.com/b"},
		},
		document{
			name:      "unquoted attribute value",
			content:   `<urlset version=1><url><loc>https://example.com/a</loc></url></urlset>`,
			locations: []string{"https://example.com/a"},
		},
	)

	for _, doc := range documents {
		t.Run(doc.name+" tolerant", func(t *testing.T) {
			s := New()
			locations := parsedLocations(s, doc.content)

			if len(s.errs) != 0 {
				t.Fatalf("unexpected errors: %v", s.errs)
			}
			assertStringSlice(t, "locations", locations, doc.locations)
		})

		t.Run(doc.name+" strict", func(t *testing.T) {
			s := New().SetStrict(true)
			locations := parsedLocations(s, doc.content)

			if len(locations) != 0 {
				t.Errorf("expected no locations, got %q", locations)
			}
			if len(s.errs) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(s.errs), s.errs)
			}
			var parseErr *ParseError
			if !errors.As(s.errs[0], &parseErr) {
				t.Fatalf("expected *ParseError, got %T: %v", s.errs[0], s.errs[0])
			}
			mustEqual(t, "error URL", parseErr.URL, url)
			if !strings.Contains(parseErr.Error(), "XML syntax error") {
				t.Errorf("error does not report the XML syntax error: %v", parseErr)
			}
		})
	}

	t.Run("truncated document is rejected in both modes", func(t *testing.T) {
		content := `<urlset><url><loc>https://example.com/a</loc></url><url><loc>https://example.com/b`

		for _, strict := range []bool{false, true} {
			s := New().SetStrict(strict)
			locations := parsedLocations(s, content)

			if len(locations) != 0 {
				t.Errorf("strict=%v: expected no locations, got %q", strict, locations)
			}
			if len(s.errs) != 1 || !strings.Contains(s.errs[0].Error(), "unexpected EOF") {
				t.Errorf("strict=%v: expected an unexpected EOF error, got %v", strict, s.errs)
			}
		}
	})
}

// TestS_parse_EmptyLocation verifies that an entry without a location yields
// no location at all. Resolved like a relative URL, an empty location would be
// the URL of the sitemap itself.
func TestS_parse_EmptyLocation(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	wantLocations := []string{"https://example.com/a", "https://example.com/b"}

	// A sitemap entry must have a <loc>: an entry without one is skipped and
	// reported. Each document holds such an entry between two valid ones.
	formats := map[string]string{
		"urlset":       `<urlset><url><loc>https://example.com/a</loc></url><url>%s</url><url><loc>https://example.com/b</loc></url></urlset>`,
		"sitemapindex": `<sitemapindex><sitemap><loc>https://example.com/a</loc></sitemap><sitemap>%s</sitemap><sitemap><loc>https://example.com/b</loc></sitemap></sitemapindex>`,
	}
	entries := []struct {
		name    string
		content string
	}{
		{"empty loc", `<loc></loc>`},
		{"self-closing loc", `<loc/>`},
		{"whitespace-only loc", "<loc> \n\t </loc>"},
		{"missing loc", `<lastmod>2024-01-15</lastmod>`},
		{"empty entry", ``},
	}

	for format, template := range formats {
		for _, entry := range entries {
			for _, strict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s %s strict=%v", format, entry.name, strict), func(t *testing.T) {
					s := New().SetStrict(strict)
					locations := parsedLocations(s, fmt.Sprintf(template, entry.content))

					assertStringSlice(t, "locations", locations, wantLocations)
					if format == "sitemapindex" {
						assertStringSlice(t, "sitemap locations", s.sitemapLocations, append([]string{url}, wantLocations...))
					}
					if len(s.errs) != 1 {
						t.Fatalf("expected 1 error, got %d: %v", len(s.errs), s.errs)
					}
					var valErr *ValidationError
					if !errors.As(s.errs[0], &valErr) {
						t.Fatalf("expected *ValidationError, got %T: %v", s.errs[0], s.errs[0])
					}
					mustEqual(t, "error URL", valErr.URL, url)
					mustEqual(t, "error", valErr.Error(), `validate "https://example.com/sitemap.xml": <loc> of an entry is empty or missing`)
				})
			}
		}
	}

	// The link of a feed item is optional: an item without one is skipped
	// without an error.
	feeds := []struct {
		name    string
		content string
	}{
		{"rss item without link", `<rss><channel><item><link>https://example.com/a</link></item><item><title>No link</title></item><item><link>https://example.com/b</link></item></channel></rss>`},
		{"rss empty link", `<rss><channel><item><link>https://example.com/a</link></item><item><link></link></item><item><link>https://example.com/b</link></item></channel></rss>`},
		{"rss whitespace-only link", "<rss><channel><item><link>https://example.com/a</link></item><item><link> \n\t </link></item><item><link>https://example.com/b</link></item></channel></rss>"},
		{"atom entry without link", `<feed><entry><link href="https://example.com/a"/></entry><entry><title>No link</title></entry><entry><link href="https://example.com/b"/></entry></feed>`},
		{"atom empty href", `<feed><entry><link href="https://example.com/a"/></entry><entry><link href=""/></entry><entry><link href="https://example.com/b"/></entry></feed>`},
		{"atom whitespace-only href", `<feed><entry><link href="https://example.com/a"/></entry><entry><link href="   "/></entry><entry><link href="https://example.com/b"/></entry></feed>`},
	}

	for _, feed := range feeds {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s strict=%v", feed.name, strict), func(t *testing.T) {
				s := New().SetStrict(strict)
				locations := parsedLocations(s, feed.content)

				assertStringSlice(t, "locations", locations, wantLocations)
				if len(s.errs) != 0 {
					t.Errorf("unexpected errors: %v", s.errs)
				}
			})
		}
	}

	t.Run("locations are trimmed", func(t *testing.T) {
		documents := map[string]string{
			"rss":  "<rss><channel><item><link>\n  https://example.com/a\n</link></item></channel></rss>",
			"atom": `<feed><entry><link href="  https://example.com/a  "/></entry></feed>`,
		}
		for format, content := range documents {
			s := New()
			locations := parsedLocations(s, content)

			assertStringSlice(t, format+" locations", locations, []string{"https://example.com/a"})
			if len(s.errs) != 0 {
				t.Errorf("%s: unexpected errors: %v", format, s.errs)
			}
		}
	})
}

func TestParseElement(t *testing.T) {
	t.Run("absent element", func(t *testing.T) {
		var invalid []invalidValue
		if got := parseElement(&invalid, "<priority>", nil, parseFloat32); got != nil {
			t.Errorf("got %v, want nil", *got)
		}
		mustEqual(t, "invalid values", len(invalid), 0)
	})

	t.Run("valid content", func(t *testing.T) {
		var invalid []invalidValue
		assertPtrFloat32(t, "value", parseElement(&invalid, "<priority>", pointerOfString(" 0.5 "), parseFloat32), 0.5)
		mustEqual(t, "invalid values", len(invalid), 0)
	})

	t.Run("invalid content", func(t *testing.T) {
		var invalid []invalidValue
		if got := parseElement(&invalid, "<priority>", pointerOfString(" 0,5 "), parseFloat32); got != nil {
			t.Errorf("got %v, want nil", *got)
		}
		if len(invalid) != 1 {
			t.Fatalf("expected 1 invalid value, got %d", len(invalid))
		}
		mustEqual(t, "error", invalid[0].err().Error(), `invalid <priority> value "0,5"`)
	})
}

func TestParseDate(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		text *string
		// want is the date the element yields, nil if it yields none.
		want *time.Time
		// wantInvalid is the error the element is reported with, if it is.
		wantInvalid string
	}{
		{name: "absent element", text: nil},
		{name: "empty element", text: pointerOfString("")},
		{name: "element of whitespace", text: pointerOfString(" \t\r\n")},
		{name: "date", text: pointerOfString("2024-01-15"), want: &date},
		{name: "date in whitespace", text: pointerOfString("\n  2024-01-15\n"), want: &date},
		{name: "first day of the year 1", text: pointerOfString("0001-01-01"), want: &time.Time{}},
		{name: "no date", text: pointerOfString("yesterday"), wantInvalid: `invalid <lastmod> value "yesterday"`},
		{name: "no date in whitespace", text: pointerOfString(" - "), wantInvalid: `invalid <lastmod> value "-"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var invalid []invalidValue
			got := parseDate(&invalid, "<lastmod>", test.text)

			switch {
			case test.want == nil && got != nil:
				t.Errorf("got %v, want nil", got.Time)
			case test.want != nil && (got == nil || !got.Equal(*test.want)):
				t.Errorf("got %v, want %v", got, *test.want)
			}

			reported := []string{}
			for _, value := range invalid {
				reported = append(reported, value.err().Error())
			}
			want := []string{}
			if test.wantInvalid != "" {
				want = append(want, test.wantInvalid)
			}
			assertStringSlice(t, "invalid values", reported, want)
		})
	}
}

func TestEmptyElement(t *testing.T) {
	tests := []struct {
		text *string
		want bool
	}{
		{nil, false},
		{pointerOfString(""), true},
		{pointerOfString(" "), true},
		{pointerOfString(" \t\r\n"), true},
		{pointerOfString("2024-01-15"), false},
		{pointerOfString(" 0 "), false},
	}

	for _, test := range tests {
		name := "absent"
		if test.text != nil {
			name = fmt.Sprintf("%q", *test.text)
		}
		t.Run(name, func(t *testing.T) {
			mustEqual(t, "emptyElement", emptyElement(test.text), test.want)
		})
	}
}

// textValues returns the text values v holds: every string in it, whether it is a field of
// a structure, an element of a slice or behind a pointer, by the path that leads to it.
// Dates are no text values.
func textValues(v any) map[string]string {
	values := map[string]string{}

	var walk func(path string, value reflect.Value)
	walk = func(path string, value reflect.Value) {
		switch value.Kind() {
		case reflect.String:
			values[path] = value.String()
		case reflect.Pointer:
			if !value.IsNil() {
				walk(path, value.Elem())
			}
		case reflect.Slice:
			for i := 0; i < value.Len(); i++ {
				walk(fmt.Sprintf("%s[%d]", path, i), value.Index(i))
			}
		case reflect.Struct:
			if value.Type() == reflect.TypeOf(LastModTime{}) {
				return
			}
			for i := 0; i < value.NumField(); i++ {
				walk(strings.TrimPrefix(path+"."+value.Type().Field(i).Name, "."), value.Field(i))
			}
		}
	}
	walk("", reflect.ValueOf(v))

	return values
}

// TestS_Parse_SurroundingWhitespace verifies that the whitespace around the content of an
// element or the value of an attribute is no part of the value, for every text value an
// entry can hold and in both modes: whichever way the values of a document are padded, the
// entry is the one that the document gives without padding.
func TestS_Parse_SurroundingWhitespace(t *testing.T) {
	const sitemapURL = "https://example.com/sitemap.xml"

	// document returns a urlset of one entry, which holds every text value an entry can
	// hold and satisfies strict mode. Each of the values is padded by pad.
	document := func(pad func(string) string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>` + pad("https://example.com/page") + `</loc>
    <changefreq>` + pad("weekly") + `</changefreq>
    <image:image>
      <image:loc>` + pad("https://cdn.example.com/image.jpg") + `</image:loc>
      <image:title>` + pad("Image title") + `</image:title>
      <image:caption>` + pad("Image caption") + `</image:caption>
      <image:geo_location>` + pad("Budapest, Hungary") + `</image:geo_location>
      <image:license>` + pad("https://example.com/license") + `</image:license>
    </image:image>
    <news:news>
      <news:publication>
        <news:name>` + pad("Example News") + `</news:name>
        <news:language>` + pad("en") + `</news:language>
      </news:publication>
      <news:publication_date>2024-01-15</news:publication_date>
      <news:title>` + pad("News title") + `</news:title>
    </news:news>
    <video:video>
      <video:thumbnail_loc>` + pad("https://example.com/thumb.jpg") + `</video:thumbnail_loc>
      <video:title>` + pad("Video title") + `</video:title>
      <video:description>` + pad("Video description") + `</video:description>
      <video:content_loc>` + pad("https://example.com/video.mp4") + `</video:content_loc>
      <video:player_loc>` + pad("https://example.com/player") + `</video:player_loc>
      <video:family_friendly>` + pad("yes") + `</video:family_friendly>
      <video:restriction relationship="` + pad("allow") + `">` + pad("IE GB US") + `</video:restriction>
      <video:platform relationship="` + pad("deny") + `">` + pad("web tv") + `</video:platform>
      <video:requires_subscription>` + pad("no") + `</video:requires_subscription>
      <video:uploader info="` + pad("https://example.com/uploader") + `">` + pad("Uploader name") + `</video:uploader>
      <video:live>` + pad("no") + `</video:live>
      <video:tag>` + pad("first tag") + `</video:tag>
      <video:tag>` + pad("second tag") + `</video:tag>
    </video:video>
    <xhtml:link rel="` + pad("alternate") + `" hreflang="` + pad("de") + `" href="` + pad("https://example.com/de/page") + `"/>
  </url>
</urlset>`
	}
	want := map[string]string{
		"Loc":                                "https://example.com/page",
		"ChangeFreq":                         "weekly",
		"Images[0].Loc":                      "https://cdn.example.com/image.jpg",
		"Images[0].Title":                    "Image title",
		"Images[0].Caption":                  "Image caption",
		"Images[0].GeoLocation":              "Budapest, Hungary",
		"Images[0].License":                  "https://example.com/license",
		"News.Publication.Name":              "Example News",
		"News.Publication.Language":          "en",
		"News.Title":                         "News title",
		"Videos[0].ThumbnailLoc":             "https://example.com/thumb.jpg",
		"Videos[0].Title":                    "Video title",
		"Videos[0].Description":              "Video description",
		"Videos[0].ContentLoc":               "https://example.com/video.mp4",
		"Videos[0].PlayerLoc":                "https://example.com/player",
		"Videos[0].FamilyFriendly":           "yes",
		"Videos[0].Restriction.Relationship": "allow",
		"Videos[0].Restriction.Value":        "IE GB US",
		"Videos[0].Platform.Relationship":    "deny",
		"Videos[0].Platform.Value":           "web tv",
		"Videos[0].RequiresSubscription":     "no",
		"Videos[0].Uploader.Info":            "https://example.com/uploader",
		"Videos[0].Uploader.Value":           "Uploader name",
		"Videos[0].Live":                     "no",
		"Videos[0].Tags[0]":                  "first tag",
		"Videos[0].Tags[1]":                  "second tag",
		"Hreflangs[0].Rel":                   "alternate",
		"Hreflangs[0].Hreflang":              "de",
		"Hreflangs[0].Href":                  "https://example.com/de/page",
	}

	paddings := map[string]string{
		"none":               "%s",
		"spaces":             "  %s  ",
		"tabs":               "\t%s\t",
		"lines of their own": "\n        %s\n      ",
		"CRLF line ends":     "\r\n        %s\r\n      ",
		"leading only":       " \n%s",
		"trailing only":      "%s\n ",
		"every kind at once": " \t\r\n%s\n\r\t ",
	}
	for name, padding := range paddings {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", name, strict), func(t *testing.T) {
				content := document(func(value string) string { return fmt.Sprintf(padding, value) })
				s := New().SetStrict(strict)
				requireParse(t, s, sitemapURL, &content)

				if errs := s.GetErrors(); len(errs) != 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
				urls := s.GetURLs()
				if len(urls) != 1 {
					t.Fatalf("expected 1 URL, got %d", len(urls))
				}
				got := textValues(urls[0])
				if !reflect.DeepEqual(got, want) {
					for path, value := range got {
						if expected, ok := want[path]; !ok || value != expected {
							t.Errorf("%s: got %q, want %q", path, value, expected)
						}
					}
					for path := range want {
						if _, ok := got[path]; !ok {
							t.Errorf("%s is missing", path)
						}
					}
				}
			})
		}
	}

	// The document has to hold every text value of an entry for the above to cover them
	// all: a text field that is added to one of the types has to be added to it as well.
	t.Run("every text value is covered", func(t *testing.T) {
		content := document(func(value string) string { return value })
		s := New()
		requireParse(t, s, sitemapURL, &content)
		for path, value := range textValues(s.GetURLs()[0]) {
			if value == "" {
				t.Errorf("%s is not set by the document", path)
			}
		}
	})
}

// TestS_Parse_SurroundingWhitespace_EveryEntry verifies that the whitespace around a value
// is dropped for every entry of a document and for every entry of an extension, not only
// for the first one.
func TestS_Parse_SurroundingWhitespace_EveryEntry(t *testing.T) {
	// padded returns the value the way the document holds it.
	padded := func(value string) string { return "\n      " + value + "\n    " }
	// page returns the entry of the page of the given name, which has two images, two
	// videos and two alternate links.
	page := func(name string) string {
		entry := `<url><loc>https://example.com/` + name + `</loc><changefreq>` + padded("monthly") + `</changefreq>`
		for _, n := range []string{"1", "2"} {
			entry += `<image:image><image:loc>` + padded("https://example.com/"+name+"-"+n+".jpg") + `</image:loc></image:image>` +
				`<video:video><video:thumbnail_loc>` + padded("https://example.com/"+name+"-"+n+".png") + `</video:thumbnail_loc>` +
				`<video:title>` + padded("Video "+n) + `</video:title><video:description>` + padded("Description "+n) + `</video:description>` +
				`<video:content_loc>` + padded("https://example.com/"+name+"-"+n+".mp4") + `</video:content_loc>` +
				`<video:tag>` + padded("tag "+n) + `</video:tag></video:video>` +
				`<xhtml:link rel="` + padded("alternate") + `" hreflang="` + padded("l"+n) + `" href="` + padded("https://example.com/l"+n+"/"+name) + `"/>`
		}
		return entry + `</url>`
	}
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">` + page("first") + page("second") + `</urlset>`

	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			s := New().SetStrict(strict)
			requireParse(t, s, "https://example.com/sitemap.xml", &content)

			if errs := s.GetErrors(); len(errs) != 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
			urls := s.GetURLs()
			if len(urls) != 2 {
				t.Fatalf("expected 2 URLs, got %d", len(urls))
			}
			for i, name := range []string{"first", "second"} {
				u := urls[i]
				if len(u.Images) != 2 || len(u.Videos) != 2 || len(u.Hreflangs) != 2 {
					t.Fatalf("%s: expected 2 images, videos and alternate links, got %d, %d and %d", name, len(u.Images), len(u.Videos), len(u.Hreflangs))
				}
				for path, value := range textValues(u) {
					if value != strings.TrimSpace(value) {
						t.Errorf("%s: %s has whitespace around it: %q", name, path, value)
					}
				}
				mustEqual(t, name+": second image", u.Images[1].Loc, "https://example.com/"+name+"-2.jpg")
				mustEqual(t, name+": second video", u.Videos[1].ThumbnailLoc, "https://example.com/"+name+"-2.png")
				mustEqual(t, name+": tag of the second video", u.Videos[1].Tags[0], "tag 2")
				mustEqual(t, name+": second alternate link", u.Hreflangs[1], AlternateLink{Rel: "alternate", Hreflang: "l2", Href: "https://example.com/l2/" + name})
			}
		})
	}
}

// TestS_Parse_ChangeFreq verifies how the content of a <changefreq> is read. Whitespace
// around it is no part of the value, and an element that holds nothing else names no
// frequency: the field is nil then, as it is without the element. Neither costs the entry
// or is reported.
// A value of the protocol that is written in another letter case is read as that value in
// tolerant mode, and a value the protocol does not know is kept there as it is. Strict mode
// accepts the values of the protocol as the protocol writes them and nothing else: any
// other value is reported and costs the entry, though no other entry.
func TestS_Parse_ChangeFreq(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		pageURL    = "https://example.com/page"
	)

	type test struct {
		element string
		// tolerant is the value of the field in tolerant mode, nil for a field that is nil.
		tolerant *URLChangeFreq
		// strict is the value of the field in strict mode, nil for a field that is nil.
		strict *URLChangeFreq
		// invalid is the value strict mode rejects the entry for, empty if it accepts it.
		invalid string
	}
	freq := func(value URLChangeFreq) *URLChangeFreq { return &value }
	tests := []test{
		{`<changefreq>daily</changefreq>`, freq(ChangeFreqDaily), freq(ChangeFreqDaily), ""},
		{`<changefreq> daily </changefreq>`, freq(ChangeFreqDaily), freq(ChangeFreqDaily), ""},
		{"<changefreq>\n      daily\n    </changefreq>", freq(ChangeFreqDaily), freq(ChangeFreqDaily), ""},
		{"<changefreq>\tdaily\r\n</changefreq>", freq(ChangeFreqDaily), freq(ChangeFreqDaily), ""},
		{`<changefreq><![CDATA[ daily ]]></changefreq>`, freq(ChangeFreqDaily), freq(ChangeFreqDaily), ""},
		{`<changefreq>Daily</changefreq>`, freq(ChangeFreqDaily), nil, "Daily"},
		{`<changefreq> Daily </changefreq>`, freq(ChangeFreqDaily), nil, "Daily"},
		{`<changefreq>dAiLy</changefreq>`, freq(ChangeFreqDaily), nil, "dAiLy"},
		// A value the protocol does not know is kept as the document gives it in tolerant
		// mode.
		{`<changefreq>sometimes</changefreq>`, freq("sometimes"), nil, "sometimes"},
		{`<changefreq> Some Times </changefreq>`, freq("Some Times"), nil, "Some Times"},
		{`<changefreq>dailyish</changefreq>`, freq("dailyish"), nil, "dailyish"},
		{`<changefreq>Biweekly</changefreq>`, freq("Biweekly"), nil, "Biweekly"},
		{`<changefreq>biweekly</changefreq>`, freq("biweekly"), nil, "biweekly"},
		{`<changefreq>DAILY!</changefreq>`, freq("DAILY!"), nil, "DAILY!"},
		{`<changefreq>dail</changefreq>`, freq("dail"), nil, "dail"},
		{`<changefreq>daily weekly</changefreq>`, freq("daily weekly"), nil, "daily weekly"},
		{`<changefreq></changefreq>`, nil, nil, ""},
		{`<changefreq/>`, nil, nil, ""},
		{`<changefreq> </changefreq>`, nil, nil, ""},
		{"<changefreq>\n\t \r\n</changefreq>", nil, nil, ""},
		{`<changefreq><![CDATA[ ]]></changefreq>`, nil, nil, ""},
		{`<changefreq><!-- daily --></changefreq>`, nil, nil, ""},
		{``, nil, nil, ""},
	}
	for _, known := range changeFreqs {
		upper := strings.ToUpper(string(known))
		title := upper[:1] + string(known)[1:]
		tests = append(tests,
			test{`<changefreq>` + string(known) + `</changefreq>`, freq(known), freq(known), ""},
			test{"<changefreq>\n  " + string(known) + "\n</changefreq>", freq(known), freq(known), ""},
			test{`<changefreq>` + upper + `</changefreq>`, freq(known), nil, upper},
			test{"<changefreq>\n  " + title + "\n</changefreq>", freq(known), nil, title},
		)
	}

	for _, test := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q, strict=%v", test.element, strict), func(t *testing.T) {
				content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
					`<url><loc>https://example.com/before</loc></url>` +
					`<url><loc>` + pageURL + `</loc>` + test.element + `<priority>0.5</priority></url>` +
					`<url><loc>https://example.com/after</loc></url></urlset>`
				s := New().SetStrict(strict)
				requireParse(t, s, sitemapURL, &content)

				if strict && test.invalid != "" {
					assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/before", "https://example.com/after"})
					errs := s.GetErrors()
					if len(errs) != 1 {
						t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
					}
					var validationErr *ValidationError
					if !errors.As(errs[0], &validationErr) {
						t.Fatalf("expected a *ValidationError, got %T: %v", errs[0], errs[0])
					}
					mustEqual(t, "URL of the error", validationErr.URL, pageURL)
					mustEqual(t, "error", validationErr.Err.Error(), fmt.Sprintf("strict mode: invalid <changefreq> value %q", test.invalid))
					return
				}

				assertStringSlice(t, "errors", errorsOf(s), []string{})
				assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/before", pageURL, "https://example.com/after"})
				urls := s.GetURLs()
				if len(urls) != 3 {
					t.FailNow()
				}
				assertPtrFloat32(t, "Priority", urls[1].Priority, 0.5)
				if urls[0].ChangeFreq != nil || urls[2].ChangeFreq != nil {
					t.Errorf("the change frequency of an entry got into another one: %v, %v", urls[0].ChangeFreq, urls[2].ChangeFreq)
				}

				want := test.tolerant
				if strict {
					want = test.strict
				}
				got := urls[1].ChangeFreq
				switch {
				case want == nil && got != nil:
					t.Errorf("ChangeFreq: got %q, want nil", *got)
				case want != nil && got == nil:
					t.Errorf("ChangeFreq: got nil, want %q", *want)
				case want != nil && *got != *want:
					t.Errorf("ChangeFreq: got %q, want %q", *got, *want)
				}
			})
		}
	}
}

// TestS_Parse_Strict_ValuesNotAllowed verifies what strict mode reports for an entry that
// has more than one value of its own wrong. A value that cannot be parsed is reported in
// both modes and costs the entry in strict mode, which looks no further into it then. Of
// the values that can be parsed, strict mode reports every one the protocol does not
// allow, the change frequency and the priority alike, and skips the entry. Tolerant mode
// keeps the entry and the values in every case.
func TestS_Parse_Strict_ValuesNotAllowed(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		pageURL    = "https://example.com/page"

		changeFreqErr = `validate "https://example.com/page": strict mode: invalid <changefreq> value "sometimes"`
		priorityErr   = `validate "https://example.com/page": strict mode: priority 1.5 is out of range [0.0, 1.0]`
		lastModErr    = `validate "https://example.com/page": invalid <lastmod> value "yesterday"`
	)

	tests := []struct {
		name     string
		elements string
		// tolerant and strict are the errors of the two modes.
		tolerant []string
		strict   []string
	}{
		{"values of the protocol", `<changefreq>daily</changefreq><priority>1.0</priority>`, []string{}, []string{}},
		{"change frequency", `<changefreq>sometimes</changefreq><priority>1.0</priority>`, []string{}, []string{changeFreqErr}},
		{"priority", `<changefreq>daily</changefreq><priority>1.5</priority>`, []string{}, []string{priorityErr}},
		{"change frequency and priority", `<changefreq>sometimes</changefreq><priority>1.5</priority>`, []string{}, []string{changeFreqErr, priorityErr}},
		{"priority before the change frequency", `<priority>1.5</priority><changefreq>sometimes</changefreq>`, []string{}, []string{changeFreqErr, priorityErr}},
		{"date that cannot be parsed", `<lastmod>yesterday</lastmod><changefreq>sometimes</changefreq><priority>1.5</priority>`, []string{lastModErr}, []string{lastModErr}},
	}

	for _, test := range tests {
		t.Run(test.name+", tolerant mode", func(t *testing.T) {
			content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + pageURL + `</loc>` + test.elements + `</url></urlset>`
			s := New()
			requireParse(t, s, sitemapURL, &content)

			assertStringSlice(t, "errors", errorsOf(s), test.tolerant)
			assertStringSlice(t, "URLs", locsOf(s), []string{pageURL})
		})

		t.Run(test.name+", strict mode", func(t *testing.T) {
			content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + pageURL + `</loc>` + test.elements + `</url></urlset>`
			s := New().SetStrict(true)
			requireParse(t, s, sitemapURL, &content)

			assertStringSlice(t, "errors", errorsOf(s), test.strict)
			want := []string{pageURL}
			if len(test.strict) > 0 {
				want = []string{}
			}
			assertStringSlice(t, "URLs", locsOf(s), want)
		})
	}
}

// TestS_Parse_BlankRequiredValues verifies that a value an extension requires is missing
// when its element or attribute holds nothing but whitespace, the same as when it is empty.
// Tolerant mode leaves out an image, a video and an alternate link without a location;
// strict mode reports every value that is missing.
func TestS_Parse_BlankRequiredValues(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		pageURL    = "https://example.com/page"
		thumbnail  = "https://example.com/thumb.jpg"
		german     = "https://example.com/de/page"
	)

	for _, blank := range []string{"", " ", "\n      ", "\t\r\n "} {
		content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>` + pageURL + `</loc>
    <image:image><image:loc>` + blank + `</image:loc><image:title>Image title</image:title></image:image>
    <news:news>
      <news:publication><news:name>` + blank + `</news:name><news:language>` + blank + `</news:language></news:publication>
      <news:publication_date>2024-01-15</news:publication_date>
      <news:title>` + blank + `</news:title>
    </news:news>
    <video:video><video:thumbnail_loc>` + blank + `</video:thumbnail_loc><video:title>Video title</video:title></video:video>
    <video:video>
      <video:thumbnail_loc>` + thumbnail + `</video:thumbnail_loc>
      <video:title>` + blank + `</video:title>
      <video:description>` + blank + `</video:description>
      <video:content_loc>` + blank + `</video:content_loc>
      <video:player_loc>` + blank + `</video:player_loc>
    </video:video>
    <xhtml:link rel="alternate" hreflang="de" href="` + blank + `"/>
    <xhtml:link rel="alternate" hreflang="` + blank + `" href="` + german + `"/>
  </url>
</urlset>`

		t.Run(fmt.Sprintf("%q, tolerant mode", blank), func(t *testing.T) {
			s := New()
			requireParse(t, s, sitemapURL, &content)

			if errs := s.GetErrors(); len(errs) != 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
			urls := s.GetURLs()
			if len(urls) != 1 {
				t.Fatalf("expected 1 URL, got %d", len(urls))
			}
			u := urls[0]
			if len(u.Images) != 0 {
				t.Errorf("expected no image, got %+v", u.Images)
			}
			if len(u.Videos) != 1 || u.Videos[0].ThumbnailLoc != thumbnail {
				t.Errorf("expected the video that has a thumbnail, got %+v", u.Videos)
			}
			if len(u.Hreflangs) != 1 || u.Hreflangs[0] != (AlternateLink{Rel: "alternate", Href: german}) {
				t.Errorf("expected the alternate link that has a location, got %+v", u.Hreflangs)
			}
			if u.News == nil || *u.News != (News{PublicationDate: u.News.PublicationDate}) || u.News.PublicationDate == nil {
				t.Errorf("expected news of a publication date only, got %+v", u.News)
			}
		})

		t.Run(fmt.Sprintf("%q, strict mode", blank), func(t *testing.T) {
			s := New().SetStrict(true)
			requireParse(t, s, sitemapURL, &content)

			var got []string
			for _, err := range s.GetErrors() {
				got = append(got, err.Error())
			}
			assertStringSlice(t, "errors", got, []string{
				`validate "": strict mode: image <loc> is empty`,
				`validate "` + pageURL + `": strict mode: news <title> is empty`,
				`validate "` + pageURL + `": strict mode: news <publication><name> is empty`,
				`validate "` + pageURL + `": strict mode: news <publication><language> is empty`,
				`validate "": strict mode: video <thumbnail_loc> is empty`,
				`validate "` + thumbnail + `": strict mode: video <title> is empty`,
				`validate "` + thumbnail + `": strict mode: video <description> is empty`,
				`validate "` + thumbnail + `": strict mode: video must have at least one of <content_loc> or <player_loc>`,
				`validate "": strict mode: alternate link <href> is empty`,
				`validate "` + german + `": strict mode: alternate link <hreflang> is empty`,
			})

			urls := s.GetURLs()
			if len(urls) != 1 {
				t.Fatalf("expected 1 URL, got %d", len(urls))
			}
			u := urls[0]
			if len(u.Images) != 0 || len(u.Videos) != 1 || len(u.Hreflangs) != 0 || u.News == nil {
				t.Errorf("expected the news and the video that has a thumbnail, got %+v", u)
			}
		})
	}
}

// TestS_Parse_PaddedLocationLength verifies that the limit on the length of a URL applies
// to the URL, not to the whitespace around it: a location of an extension that is as long
// as the limit allows is accepted however it is padded, in both modes.
func TestS_Parse_PaddedLocationLength(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		prefix     = "https://example.com/"
	)
	// entry returns the extension entry of each kind that is located at loc.
	entries := map[string]func(loc string) string{
		"image": func(loc string) string { return `<image:image><image:loc>` + loc + `</image:loc></image:image>` },
		"video": func(loc string) string {
			return `<video:video><video:thumbnail_loc>` + loc + `</video:thumbnail_loc><video:title>Video title</video:title><video:description>Video description</video:description><video:player_loc>https://example.com/player</video:player_loc></video:video>`
		},
		"alternate link": func(loc string) string { return `<xhtml:link rel="alternate" hreflang="de" href="` + loc + `"/>` },
	}
	count := func(u URL) int { return len(u.Images) + len(u.Videos) + len(u.Hreflangs) }

	for kind, entry := range entries {
		for _, strict := range []bool{false, true} {
			for _, over := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s, strict=%v, %d over the limit", kind, strict, over), func(t *testing.T) {
					loc := prefix + strings.Repeat("a", maxLocLength-len(prefix)+over)
					content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url><loc>https://example.com/page</loc>` + entry("\n      "+loc+"\n    ") + `</url>
</urlset>`
					s := New().SetStrict(strict)
					requireParse(t, s, sitemapURL, &content)

					urls := s.GetURLs()
					if len(urls) != 1 {
						t.Fatalf("expected 1 URL, got %d", len(urls))
					}
					errs := s.GetErrors()
					if over == 0 {
						if len(errs) != 0 || count(urls[0]) != 1 {
							t.Fatalf("expected the entry and no error, got %d entries, errors: %v", count(urls[0]), errs)
						}
						return
					}

					if count(urls[0]) != 0 {
						t.Errorf("expected the entry to be left out, got %d entries", count(urls[0]))
					}
					if len(errs) != 1 {
						t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
					}
					var validationErr *ValidationError
					if !errors.As(errs[0], &validationErr) {
						t.Fatalf("expected a *ValidationError, got %T: %v", errs[0], errs[0])
					}
					mustEqual(t, "URL of the error", validationErr.URL, loc)
					mustEqual(t, "error", validationErr.Err.Error(), fmt.Sprintf("URL exceeds maximum length of %d characters (%d)", maxLocLength, maxLocLength+1))
				})
			}
		}
	}
}

// TestS_Parse_Atom_PaddedRel verifies that the whitespace around the relation of an Atom
// link is no part of it: the link of an entry is the one whose relation is "alternate" or
// that names none, however the value of the attribute is padded.
func TestS_Parse_Atom_PaddedRel(t *testing.T) {
	tests := []struct {
		name  string
		links string
		want  []string
	}{
		{"alternate", `<link rel="alternate" href="https://example.com/a"/>`, []string{"https://example.com/a"}},
		{"padded alternate", `<link rel=" alternate " href="https://example.com/a"/>`, []string{"https://example.com/a"}},
		{"alternate on a line of its own", "<link rel=\"\n  alternate\n\" href=\"https://example.com/a\"/>", []string{"https://example.com/a"}},
		{"padded alternate after another relation", `<link rel="self" href="https://example.com/self"/><link rel="	alternate	" href="https://example.com/a"/>`, []string{"https://example.com/a"}},
		{"blank relation", `<link rel=" " href="https://example.com/a"/>`, []string{"https://example.com/a"}},
		{"padded other relation", `<link rel=" self " href="https://example.com/self"/>`, []string{}},
	}

	for _, test := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, strict=%v", test.name, strict), func(t *testing.T) {
				content := `<feed xmlns="http://www.w3.org/2005/Atom"><entry>` + test.links + `</entry></feed>`
				s := New().SetStrict(strict)
				requireParse(t, s, "https://example.com/atom.xml", &content)

				assertStringSlice(t, "URLs", locsOf(s), test.want)
				if errs := s.GetErrors(); len(errs) != 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
			})
		}
	}
}

func TestTrimChangeFreq(t *testing.T) {
	tests := []struct {
		changeFreq *string
		want       *string
	}{
		{nil, nil},
		{pointerOfString(""), nil},
		{pointerOfString(" "), nil},
		{pointerOfString(" \t\r\n"), nil},
		{pointerOfString("daily"), pointerOfString("daily")},
		{pointerOfString(" daily "), pointerOfString("daily")},
		{pointerOfString("\n\tDaily\r\n"), pointerOfString("Daily")},
		{pointerOfString(" some times "), pointerOfString("some times")},
	}

	for _, test := range tests {
		name := "absent"
		if test.changeFreq != nil {
			name = fmt.Sprintf("%q", *test.changeFreq)
		}
		t.Run(name, func(t *testing.T) {
			var changeFreq *URLChangeFreq
			if test.changeFreq != nil {
				value := URLChangeFreq(*test.changeFreq)
				changeFreq = &value
			}

			got := trimChangeFreq(changeFreq)
			switch {
			case test.want == nil && got != nil:
				t.Errorf("got %q, want nil", *got)
			case test.want != nil && got == nil:
				t.Errorf("got nil, want %q", *test.want)
			case test.want != nil && string(*got) != *test.want:
				t.Errorf("got %q, want %q", *got, *test.want)
			}
		})
	}
}

func TestCanonicalChangeFreq(t *testing.T) {
	tests := map[URLChangeFreq]URLChangeFreq{
		"daily":     ChangeFreqDaily,
		"Daily":     ChangeFreqDaily,
		"DAILY":     ChangeFreqDaily,
		"dAILy":     ChangeFreqDaily,
		"ALWAYS":    ChangeFreqAlways,
		"Hourly":    ChangeFreqHourly,
		"WeeKly":    ChangeFreqWeekly,
		"MONTHLY":   ChangeFreqMonthly,
		"yearlY":    ChangeFreqYearly,
		"Never":     ChangeFreqNever,
		"":          "",
		"sometimes": "sometimes",
		"Sometimes": "Sometimes",
		"dail":      "dail",
		"biweekly":  "biweekly",
		"Bi-Weekly": "Bi-Weekly",
		"DAILYS":    "DAILYS",
		// The whitespace is dropped before a value gets here.
		" Daily ": " Daily ",
	}
	for _, known := range changeFreqs {
		tests[known] = known
	}

	for changeFreq, want := range tests {
		t.Run(fmt.Sprintf("%q", string(changeFreq)), func(t *testing.T) {
			mustEqual(t, "canonicalChangeFreq", canonicalChangeFreq(changeFreq), want)
		})
	}
	mustEqual(t, "values of the protocol", len(changeFreqs), 7)
}

// TestS_Parse_SameHost verifies which URLs strict mode takes to be on the host of the
// sitemap. The name of a host is not case-sensitive, and a URL that names no port is on
// the default port of its protocol, so neither difference makes for another host. A
// different name or port does. Tolerant mode does not compare the hosts at all.
func TestS_Parse_SameHost(t *testing.T) {
	const (
		httpsSitemap = "https://example.com/sitemap.xml"
		httpSitemap  = "http://example.com/sitemap.xml"
	)

	tests := []struct {
		name    string
		sitemap string
		loc     string
		same    bool
	}{
		{"same host", httpsSitemap, "https://example.com/page", true},
		{"host of the URL in capitals", httpsSitemap, "https://EXAMPLE.COM/page", true},
		{"host of the URL in mixed case", httpsSitemap, "https://Example.Com/page", true},
		{"host of the sitemap in capitals", "https://EXAMPLE.COM/sitemap.xml", "https://example.com/page", true},
		{"default port of HTTPS on the URL", httpsSitemap, "https://example.com:443/page", true},
		{"default port of HTTPS on the sitemap", "https://example.com:443/sitemap.xml", "https://example.com/page", true},
		{"default port of HTTPS on both", "https://example.com:443/sitemap.xml", "https://example.com:443/page", true},
		{"default port of HTTP on the URL", httpSitemap, "http://example.com:80/page", true},
		{"default port of HTTP on the sitemap", "http://example.com:80/sitemap.xml", "http://example.com/page", true},
		{"empty port on the URL", httpsSitemap, "https://example.com:/page", true},
		{"default port and capitals", httpsSitemap, "https://EXAMPLE.com:443/page", true},
		{"same port that is not the default one", "https://example.com:8443/sitemap.xml", "https://Example.com:8443/page", true},
		{"IPv4 address with the default port", "https://192.0.2.1/sitemap.xml", "https://192.0.2.1:443/page", true},
		{"IPv6 address in capitals with the default port", "https://[2001:db8::1]/sitemap.xml", "https://[2001:DB8::1]:443/page", true},
		{"host that is not ASCII in capitals", "https://bücher.example/sitemap.xml", "https://BÜCHER.example/page", true},
		{"user information on the URL", httpsSitemap, "https://user@example.com/page", true},

		{"other host", httpsSitemap, "https://example.org/page", false},
		{"subdomain", httpsSitemap, "https://www.example.com/page", false},
		{"parent domain", "https://www.example.com/sitemap.xml", "https://example.com/page", false},
		{"host that begins with the host of the sitemap", httpsSitemap, "https://example.com.evil.test/page", false},
		{"host that ends with the host of the sitemap", httpsSitemap, "https://notexample.com/page", false},
		{"host with a trailing dot", httpsSitemap, "https://example.com./page", false},
		{"other host in capitals", httpsSitemap, "https://EXAMPLE.ORG/page", false},
		{"other port", httpsSitemap, "https://example.com:8443/page", false},
		{"other port on the sitemap", "https://example.com:8443/sitemap.xml", "https://example.com/page", false},
		{"default port against another port of the sitemap", "https://example.com:8443/sitemap.xml", "https://example.com:443/page", false},
		{"port of HTTP on an HTTPS URL", httpsSitemap, "https://example.com:80/page", false},
		{"port of HTTPS on an HTTP URL", httpSitemap, "http://example.com:443/page", false},
		{"port that begins with the default one", httpsSitemap, "https://example.com:4430/page", false},
		{"same port on another host", "https://example.com:8443/sitemap.xml", "https://example.org:8443/page", false},
		{"other IPv6 address", "https://[2001:db8::1]/sitemap.xml", "https://[2001:db8::2]/page", false},
	}

	for _, test := range tests {
		content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + test.loc + `</loc></url></urlset>`

		t.Run(test.name+", strict mode", func(t *testing.T) {
			s := New().SetStrict(true)
			requireParse(t, s, test.sitemap, &content)

			if test.same {
				// Strict mode returns the URL the way the document gives it.
				assertStringSlice(t, "URLs", locsOf(s), []string{test.loc})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
				return
			}
			loc, err := neturl.Parse(test.loc)
			if err != nil {
				t.Fatal(err)
			}
			sitemap, err := neturl.Parse(test.sitemap)
			if err != nil {
				t.Fatal(err)
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{
				fmt.Sprintf(`validate %q: strict mode: host %q does not match sitemap host %q`, test.loc, loc.Host, sitemap.Host),
			})
		})

		t.Run(test.name+", tolerant mode", func(t *testing.T) {
			s := New()
			requireParse(t, s, test.sitemap, &content)

			mustEqual(t, "URLs", s.GetURLCount(), 1)
			assertStringSlice(t, "errors", errorsOf(s), []string{})
		})
	}
}

// TestS_Parse_SameProtocol verifies that strict mode requires the protocol of the sitemap
// whatever port the URL names: a URL of the other protocol is not on the host of the
// sitemap for naming the port the sitemap is served on. The letter case of the protocol
// makes no difference. Tolerant mode does not compare the protocols.
func TestS_Parse_SameProtocol(t *testing.T) {
	const (
		httpsSitemap = "https://example.com/sitemap.xml"
		httpSitemap  = "http://example.com/sitemap.xml"
	)

	tests := []struct {
		name    string
		sitemap string
		loc     string
		// err is the error of strict mode, empty for a URL it accepts.
		err string
	}{
		{"HTTPS URL in an HTTPS sitemap", httpsSitemap, "https://example.com/page", ""},
		{"HTTP URL in an HTTP sitemap", httpSitemap, "http://example.com/page", ""},
		{"protocol in capitals", httpsSitemap, "HTTPS://example.com/page", ""},
		{"HTTP URL in an HTTPS sitemap", httpsSitemap, "http://example.com/page", `strict mode: scheme "http" does not match sitemap scheme "https"`},
		{"HTTP URL on the port of HTTPS in an HTTPS sitemap", httpsSitemap, "http://example.com:443/page", `strict mode: scheme "http" does not match sitemap scheme "https"`},
		{"HTTPS URL in an HTTP sitemap", httpSitemap, "https://example.com/page", `strict mode: scheme "https" does not match sitemap scheme "http"`},
		{"HTTPS URL on the port of HTTP in an HTTP sitemap", httpSitemap, "https://example.com:80/page", `strict mode: scheme "https" does not match sitemap scheme "http"`},
		{"HTTP URL on the port of an HTTPS sitemap", "https://example.com:8443/sitemap.xml", "http://example.com:8443/page", `strict mode: scheme "http" does not match sitemap scheme "https"`},
	}

	for _, test := range tests {
		content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + test.loc + `</loc></url></urlset>`

		t.Run(test.name+", strict mode", func(t *testing.T) {
			s := New().SetStrict(true)
			requireParse(t, s, test.sitemap, &content)

			if test.err == "" {
				assertStringSlice(t, "URLs", locsOf(s), []string{test.loc})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
				return
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{fmt.Sprintf(`validate %q: %s`, test.loc, test.err)})
		})

		t.Run(test.name+", tolerant mode", func(t *testing.T) {
			s := New()
			requireParse(t, s, test.sitemap, &content)

			mustEqual(t, "URLs", s.GetURLCount(), 1)
			assertStringSlice(t, "errors", errorsOf(s), []string{})
		})
	}
}

// TestS_Parse_SameHost_SitemapIndex verifies that the sitemaps a sitemap index lists are
// compared with the host of the index the same way: a host in another letter case and the
// default port are no other host, so the sitemap is fetched in strict mode.
func TestS_Parse_SameHost_SitemapIndex(t *testing.T) {
	content := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://EXAMPLE.com:443/sitemap-1.xml</loc></sitemap>
  <sitemap><loc>https://example.com:8443/sitemap-2.xml</loc></sitemap>
</sitemapindex>`

	var requested []string
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`<urlset><url><loc>https://example.com/page</loc></url></urlset>`)),
			Request:    req,
		}, nil
	})}
	s := New().SetStrict(true).SetMultiThread(false).SetHTTPClient(client)
	requireParse(t, s, "https://example.com/sitemap-index.xml", &content)

	assertStringSlice(t, "requests", requested, []string{"https://EXAMPLE.com:443/sitemap-1.xml"})
	assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/page"})
	assertStringSlice(t, "errors", errorsOf(s), []string{
		`validate "https://example.com:8443/sitemap-2.xml": strict mode: host "example.com:8443" does not match sitemap host "example.com"`,
	})
}

func TestSameHost(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://example.com/", "https://example.com/other?query#fragment", true},
		{"https://example.com/", "https://EXAMPLE.com/", true},
		{"https://example.com/", "https://example.com:443/", true},
		{"https://example.com/", "https://example.com:/", true},
		{"http://example.com/", "http://example.com:80/", true},
		{"https://example.com:8443/", "https://example.com:8443/", true},
		{"https://example.com/", "https://example.org/", false},
		{"https://example.com/", "https://www.example.com/", false},
		{"https://example.com/", "https://example.com:8443/", false},
		{"https://example.com/", "https://example.com:80/", false},
		{"http://example.com/", "http://example.com:443/", false},
		{"https://example.com:8443/", "https://example.com:8444/", false},
		{"https://example.com:8443/", "https://example.org:8443/", false},
	}

	for _, test := range tests {
		t.Run(test.a+" and "+test.b, func(t *testing.T) {
			a, err := neturl.Parse(test.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := neturl.Parse(test.b)
			if err != nil {
				t.Fatal(err)
			}
			mustEqual(t, "sameHost", sameHost(a, b), test.want)
			mustEqual(t, "sameHost the other way round", sameHost(b, a), test.want)
		})
	}
}

// TestS_Parse_SpaceInURL verifies what becomes of a URL that has a space in it, which a URL
// has to give as "%20". Tolerant mode percent-encodes the space wherever it stands: in the
// path, in the query and in the fragment, so that the URL it returns can be requested as
// it is. Strict mode rejects the URL. It makes no difference which format lists the URL.
func TestS_Parse_SpaceInURL(t *testing.T) {
	tests := []struct {
		name string
		loc  string
		// tolerant is the URL tolerant mode returns.
		tolerant string
	}{
		{"path", "https://example.com/a b", "https://example.com/a%20b"},
		{"query", "https://example.com/search?q=a b", "https://example.com/search?q=a%20b"},
		{"fragment", "https://example.com/page#a b", "https://example.com/page#a%20b"},
		{"path, query and fragment", "https://example.com/a b?c d#e f", "https://example.com/a%20b?c%20d#e%20f"},
		{"several in the query", "https://example.com/search?q=a  b c", "https://example.com/search?q=a%20%20b%20c"},
		{"end of the query, before an empty fragment", "https://example.com/search?q=a #", "https://example.com/search?q=a%20"},
		{"end of the path, before an empty fragment", "https://example.com/a #", "https://example.com/a%20"},
		{"beside one that is encoded", "https://example.com/a%20b?c=d%20e f+g", "https://example.com/a%20b?c=d%20e%20f+g"},
	}
	formats := []struct {
		name    string
		url     string
		content func(loc string) string
	}{
		{"urlset", "https://example.com/sitemap.xml", func(loc string) string {
			return `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + loc + `</loc></url></urlset>`
		}},
		{"text", "https://example.com/sitemap.txt", func(loc string) string { return loc + "\n" }},
		{"RSS", "https://example.com/rss.xml", func(loc string) string {
			return `<rss version="2.0"><channel><item><link>` + loc + `</link></item></channel></rss>`
		}},
		{"Atom", "https://example.com/atom.xml", func(loc string) string {
			return `<feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="` + loc + `"/></entry></feed>`
		}},
	}

	for _, format := range formats {
		for _, test := range tests {
			content := format.content(test.loc)

			t.Run(format.name+", "+test.name+", tolerant mode", func(t *testing.T) {
				s := New()
				requireParse(t, s, format.url, &content)

				assertStringSlice(t, "URLs", locsOf(s), []string{test.tolerant})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
			})

			t.Run(format.name+", "+test.name+", strict mode", func(t *testing.T) {
				s := New().SetStrict(true)
				requireParse(t, s, format.url, &content)

				assertStringSlice(t, "URLs", locsOf(s), []string{})
				errs := s.GetErrors()
				if len(errs) != 1 {
					t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
				}
				var validationErr *ValidationError
				if !errors.As(errs[0], &validationErr) {
					t.Fatalf("expected a *ValidationError, got %T: %v", errs[0], errs[0])
				}
				mustEqual(t, "URL of the error", validationErr.URL, test.loc)
				mustEqual(t, "error", validationErr.Err.Error(), "strict mode: URL contains a space")
			})
		}

		// A URL that gives its spaces the way a URL has to is left as it is in both modes.
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, no space, strict=%v", format.name, strict), func(t *testing.T) {
				const loc = "https://example.com/a%20b?c=d%20e+f#g%20h"
				content := format.content(loc)
				s := New().SetStrict(strict)
				requireParse(t, s, format.url, &content)

				assertStringSlice(t, "URLs", locsOf(s), []string{loc})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
			})
		}
	}
}

// TestS_Parse_ControlCharacterInURL verifies what becomes of a URL that has a control
// character in it, a tab or a line break for one. In the path and in the query it makes a
// URL that cannot be parsed, which is rejected in both modes. In the fragment tolerant
// mode percent-encodes it, as it does a space, and strict mode rejects the URL.
func TestS_Parse_ControlCharacterInURL(t *testing.T) {
	const parseErr = "net/url: invalid control character in URL"

	tests := []struct {
		name string
		// content is the text sitemap that lists the URL, loc the URL as the errors name it.
		content string
		loc     string
		// tolerant is the URL tolerant mode returns, empty if it rejects the URL.
		tolerant string
		// strict is the error of strict mode.
		strict string
	}{
		{"tab in the fragment", "https://example.com/page#a\tb\n", "https://example.com/page#a\tb", "https://example.com/page#a%09b", "strict mode: URL contains a control character"},
		{"control character in the fragment", "https://example.com/page#a\x1db\n", "https://example.com/page#a\x1db", "https://example.com/page#a%1Db", "strict mode: URL contains a control character"},
		{"control character at the end of the fragment", "https://example.com/page#\x1d\n", "https://example.com/page#\x1d", "https://example.com/page#%1D", "strict mode: URL contains a control character"},
		{"NUL in the fragment", "https://example.com/page#a\x00b\n", "https://example.com/page#a\x00b", "https://example.com/page#a%00b", "strict mode: URL contains a control character"},
		{"DEL in the fragment", "https://example.com/page#a\x7fb\n", "https://example.com/page#a\x7fb", "https://example.com/page#a%7Fb", "strict mode: URL contains a control character"},
		{"space and tab in the fragment", "https://example.com/page#a b\tc\n", "https://example.com/page#a b\tc", "https://example.com/page#a%20b%09c", "strict mode: URL contains a space"},
		{"tab in the path", "https://example.com/a\tb\n", "https://example.com/a\tb", "", parseErr},
		{"tab in the query", "https://example.com/page?q=a\tb\n", "https://example.com/page?q=a\tb", "", parseErr},
	}

	for _, test := range tests {
		t.Run(test.name+", tolerant mode", func(t *testing.T) {
			content := test.content
			s := New()
			requireParse(t, s, "https://example.com/sitemap.txt", &content)

			if test.tolerant != "" {
				assertStringSlice(t, "URLs", locsOf(s), []string{test.tolerant})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
				return
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{fmt.Sprintf(`validate %q: parse %q: %s`, test.loc, test.loc, parseErr)})
		})

		t.Run(test.name+", strict mode", func(t *testing.T) {
			content := test.content
			s := New().SetStrict(true)
			requireParse(t, s, "https://example.com/sitemap.txt", &content)

			want := fmt.Sprintf(`validate %q: %s`, test.loc, test.strict)
			if test.strict == parseErr {
				want = fmt.Sprintf(`validate %q: parse %q: %s`, test.loc, test.loc, parseErr)
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{want})
		})
	}

	// In an XML document the character is a tab or a line break: the others are no
	// characters of XML.
	for name, loc := range map[string]string{
		"tab as a character reference": "https://example.com/page#a&#9;b",
		"line break":                   "https://example.com/page#a\nb",
	} {
		content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url><loc>` + loc + `</loc></url>
  <url><loc>https://example.com/other</loc><image:image><image:loc>` + loc + `</image:loc></image:image></url>
</urlset>`

		t.Run("urlset, "+name+", tolerant mode", func(t *testing.T) {
			s := New()
			requireParse(t, s, "https://example.com/sitemap.xml", &content)

			encoded := "https://example.com/page#a%09b"
			if name == "line break" {
				encoded = "https://example.com/page#a%0Ab"
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{encoded, "https://example.com/other"})
			assertStringSlice(t, "errors", errorsOf(s), []string{})
		})

		t.Run("urlset, "+name+", strict mode", func(t *testing.T) {
			s := New().SetStrict(true)
			requireParse(t, s, "https://example.com/sitemap.xml", &content)

			raw := strings.NewReplacer("&#9;", "\t").Replace(loc)
			want := fmt.Sprintf(`validate %q: strict mode: URL contains a control character`, raw)
			assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/other"})
			assertStringSlice(t, "errors", errorsOf(s), []string{want, want})
			if urls := s.GetURLs(); len(urls) == 1 && len(urls[0].Images) != 0 {
				t.Errorf("expected the image to be left out, got %+v", urls[0].Images)
			}
		})
	}
}

func TestIsControl(t *testing.T) {
	for _, r := range []rune{0x00, '\t', '\n', '\r', 0x1b, 0x1f, 0x7f} {
		if !isControl(r) {
			t.Errorf("isControl(%q): got false, want true", r)
		}
	}
	// A space is no control character, and neither is a character beyond ASCII.
	for _, r := range []rune{' ', '!', 'a', '~', 0x80, 0x85, 0xa0, 'é', 0x2028} {
		if isControl(r) {
			t.Errorf("isControl(%q): got true, want false", r)
		}
	}
}

// TestS_Parse_SpaceInURL_Relative verifies that tolerant mode percent-encodes the spaces
// of a relative URL as well, the ones of its query included.
func TestS_Parse_SpaceInURL_Relative(t *testing.T) {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>/a b?c d#e f</loc></url>
  <url><loc>?q=a b</loc></url>
</urlset>`
	s := New()
	requireParse(t, s, "https://example.com/maps/sitemap.xml", &content)

	assertStringSlice(t, "URLs", locsOf(s), []string{
		"https://example.com/a%20b?c%20d#e%20f",
		"https://example.com/maps/sitemap.xml?q=a%20b",
	})
	assertStringSlice(t, "errors", errorsOf(s), []string{})
}

// TestS_Parse_SpaceInURL_OtherProtocol verifies that a URL of a protocol that is not
// supported is reported the way the document gives it: the space of a URL that is rejected
// anyway is left alone.
func TestS_Parse_SpaceInURL_OtherProtocol(t *testing.T) {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>mailto:someone@example.com?subject=a b</loc></url></urlset>`

	for strict, want := range map[bool]string{
		false: `validate "mailto:someone@example.com?subject=a b": unsupported scheme "mailto"`,
		true:  `validate "mailto:someone@example.com?subject=a b": strict mode: unsupported scheme "mailto"`,
	} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			s := New().SetStrict(strict)
			requireParse(t, s, "https://example.com/sitemap.xml", &content)

			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{want})
		})
	}
}

// TestS_Parse_SpaceInURL_Length verifies that tolerant mode applies the limit on the length
// of a URL to the URL it returns: a space counts as the three characters it is encoded to.
func TestS_Parse_SpaceInURL_Length(t *testing.T) {
	const prefix = "https://example.com/search?q=a b"

	for _, over := range []int{0, 1} {
		t.Run(fmt.Sprintf("%d over the limit", over), func(t *testing.T) {
			// The encoded space makes the URL two characters longer.
			loc := prefix + strings.Repeat("c", maxLocLength-2-len(prefix)+over)
			encoded := strings.Replace(loc, " ", "%20", 1)
			mustEqual(t, "length of the encoded URL", len(encoded), maxLocLength+over)

			content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + loc + `</loc></url></urlset>`
			s := New()
			requireParse(t, s, "https://example.com/sitemap.xml", &content)

			if over == 0 {
				assertStringSlice(t, "URLs", locsOf(s), []string{encoded})
				assertStringSlice(t, "errors", errorsOf(s), []string{})
				return
			}
			assertStringSlice(t, "URLs", locsOf(s), []string{})
			assertStringSlice(t, "errors", errorsOf(s), []string{
				fmt.Sprintf(`validate %q: URL exceeds maximum length of %d characters (%d)`, encoded, maxLocLength, maxLocLength+1),
			})
		})
	}
}

// TestS_Parse_SpaceInURL_SitemapIndex verifies that the request for a sitemap whose URL a
// sitemap index gives with a space in it is sent for the encoded URL in tolerant mode, a
// request for a URL with a space in it being malformed. Strict mode does not fetch it.
func TestS_Parse_SpaceInURL_SitemapIndex(t *testing.T) {
	content := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/site map.xml?v=a b</loc></sitemap>
</sitemapindex>`

	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			var requested []string
			client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				requested = append(requested, req.URL.RequestURI())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(`<urlset><url><loc>https://example.com/page</loc></url></urlset>`)),
					Request:    req,
				}, nil
			})}
			s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(client)
			requireParse(t, s, "https://example.com/sitemap-index.xml", &content)

			if strict {
				if len(requested) != 0 {
					t.Errorf("expected no request, got %v", requested)
				}
				assertStringSlice(t, "URLs", locsOf(s), []string{})
				assertStringSlice(t, "errors", errorsOf(s), []string{
					`validate "https://example.com/site map.xml?v=a b": strict mode: URL contains a space`,
				})
				return
			}
			assertStringSlice(t, "requests", requested, []string{"/site%20map.xml?v=a%20b"})
			assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/page"})
			assertStringSlice(t, "errors", errorsOf(s), []string{})
		})
	}
}

// TestS_Parse_Strict_ExtensionURL verifies what strict mode requires of the URL of an
// image, of the thumbnail of a video and of an alternate link: an absolute HTTP or HTTPS
// URL that names a host and has no space in it. Unlike the URL of the page it may be on
// any host. An entry whose URL is not one is left out and reported, the page is kept.
// Tolerant mode returns the URL as the document gives it.
func TestS_Parse_Strict_ExtensionURL(t *testing.T) {
	const (
		sitemapURL = "https://example.com/sitemap.xml"
		pageURL    = "https://example.com/page"
	)
	// entries holds, for each kind, the extension entry that is located at loc.
	entries := map[string]func(loc string) string{
		"image": func(loc string) string { return `<image:image><image:loc>` + loc + `</image:loc></image:image>` },
		"video": func(loc string) string {
			return `<video:video><video:thumbnail_loc>` + loc + `</video:thumbnail_loc><video:title>Video title</video:title><video:description>Video description</video:description><video:player_loc>https://example.com/player</video:player_loc></video:video>`
		},
		"alternate link": func(loc string) string { return `<xhtml:link rel="alternate" hreflang="de" href="` + loc + `"/>` },
	}
	locOf := func(u URL) []string {
		locs := []string{}
		for _, image := range u.Images {
			locs = append(locs, image.Loc)
		}
		for _, video := range u.Videos {
			locs = append(locs, video.ThumbnailLoc)
		}
		for _, link := range u.Hreflangs {
			locs = append(locs, link.Href)
		}
		return locs
	}

	tests := []struct {
		name string
		loc  string
		// err is the error of strict mode, empty for a URL it accepts.
		err string
	}{
		{"URL on the host of the page", "https://example.com/file", ""},
		{"URL on another host and port", "https://CDN.example.net:8443/a%20b?c=d+e#f", ""},
		{"HTTP URL", "http://cdn.example.net/file", ""},
		{"space in the path", "https://cdn.example.net/a b", "strict mode: URL contains a space"},
		{"space in the query", "https://cdn.example.net/file?v=a b", "strict mode: URL contains a space"},
		{"space in the fragment", "https://cdn.example.net/file#a b", "strict mode: URL contains a space"},
		{"no host", "https:///file", "strict mode: missing host"},
		{"no host and no path", "https:file", "strict mode: missing host"},
		{"no host and a space", "https:///a b", "strict mode: missing host"},
		{"port and no host", "https://:8080/file", "strict mode: missing host"},
		{"user and no host", "https://user@/file", "strict mode: missing host"},
		{"relative URL", "/file", `strict mode: unsupported scheme ""`},
		{"relative URL with a space", "/a b", `strict mode: unsupported scheme ""`},
		{"other protocol", "ftp://cdn.example.net/file", `strict mode: unsupported scheme "ftp"`},
		{"URL that cannot be parsed", "https://cdn.example.net/%zz", `parse "https://cdn.example.net/%zz": invalid URL escape "%zz"`},
	}

	for kind, entry := range entries {
		for _, test := range tests {
			content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url><loc>` + pageURL + `</loc>` + entry(test.loc) + `</url>
</urlset>`

			t.Run(kind+", "+test.name+", tolerant mode", func(t *testing.T) {
				s := New()
				requireParse(t, s, sitemapURL, &content)

				assertStringSlice(t, "errors", errorsOf(s), []string{})
				urls := s.GetURLs()
				if len(urls) != 1 {
					t.Fatalf("expected 1 URL, got %d", len(urls))
				}
				assertStringSlice(t, "entries", locOf(urls[0]), []string{test.loc})
			})

			t.Run(kind+", "+test.name+", strict mode", func(t *testing.T) {
				s := New().SetStrict(true)
				requireParse(t, s, sitemapURL, &content)

				assertStringSlice(t, "URLs", locsOf(s), []string{pageURL})
				urls := s.GetURLs()
				if len(urls) != 1 {
					t.FailNow()
				}
				if test.err == "" {
					assertStringSlice(t, "errors", errorsOf(s), []string{})
					assertStringSlice(t, "entries", locOf(urls[0]), []string{test.loc})
					return
				}
				assertStringSlice(t, "entries", locOf(urls[0]), []string{})
				errs := s.GetErrors()
				if len(errs) != 1 {
					t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
				}
				var validationErr *ValidationError
				if !errors.As(errs[0], &validationErr) {
					t.Fatalf("expected a *ValidationError, got %T: %v", errs[0], errs[0])
				}
				mustEqual(t, "URL of the error", validationErr.URL, test.loc)
				mustEqual(t, "error", validationErr.Err.Error(), test.err)
			})
		}
	}
}

func TestParseFloat32(t *testing.T) {
	tests := []struct {
		text    string
		want    float32
		wantErr bool
	}{
		{"0.5", 0.5, false},
		{" 1.0\n", 1, false},
		{"-2", -2, false},
		{"", 0, false},
		{" \t\n", 0, false},
		{"0,5", 0, true},
		{"high", 0, true},
		{"1e40", 0, true},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%q", test.text), func(t *testing.T) {
			got, err := parseFloat32(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil {
				mustEqual(t, "value", got, test.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		text    string
		want    int
		wantErr bool
	}{
		{"600", 600, false},
		{" 42\n", 42, false},
		{"+7", 7, false},
		{"-3", -3, false},
		{"", 0, false},
		{" \t\n", 0, false},
		{"1:30", 0, true},
		{"1,234", 0, true},
		{"12.5", 0, true},
		{"99999999999999999999", 0, true},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%q", test.text), func(t *testing.T) {
			got, err := parseInt(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil {
				mustEqual(t, "value", got, test.want)
			}
		})
	}
}

func TestParseLastModTime(t *testing.T) {
	tests := []struct {
		text    string
		want    time.Time
		wantErr bool
	}{
		{"2024-01-15", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), false},
		{" 2024-01-15T10:30:00Z\n", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"0001-01-01", time.Time{}, false},
		{"", time.Time{}, true},
		{" \t\n", time.Time{}, true},
		{"2024-01-15T10:30:00", time.Time{}, true},
		{"yesterday", time.Time{}, true},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%q", test.text), func(t *testing.T) {
			got, err := parseLastModTime(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if !got.Equal(test.want) {
				t.Errorf("got %v, want %v", got.Time, test.want)
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
			_, err := decodeURLSet(s, test.data)

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

// TestS_parseURLSet_Entries verifies which elements of a <urlset> parseURLSet
// hands on, and that the entries preceding an error have been handed on by the
// time the error is returned.
func TestS_parseURLSet_Entries(t *testing.T) {
	const page = "https://example.com/"
	entry := func(name string) string {
		return "<url><loc>" + page + name + "</loc></url>"
	}

	tests := []struct {
		name string
		data string
		want []string
		err  string
	}{
		{
			name: "entries in the order of the document",
			data: "<urlset>" + entry("1") + entry("2") + entry("3") + "</urlset>",
			want: []string{page + "1", page + "2", page + "3"},
		},
		{
			name: "no entries",
			data: "<urlset/>",
		},
		{
			name: "another element is skipped together with the url in it",
			data: "<urlset><other>" + entry("nested") + "</other>" + entry("1") + "</urlset>",
			want: []string{page + "1"},
		},
		{
			name: "url of another namespace",
			data: `<urlset xmlns:x="urn:x"><x:url><loc>` + page + `1</loc></x:url></urlset>`,
			want: []string{page + "1"},
		},
		{
			name: "what is not an element is passed over",
			data: "<?xml version=\"1.0\"?><!-- comment --><!DOCTYPE urlset>\n<urlset>text" + entry("1") + "<!-- comment --><?pi data?></urlset>",
			want: []string{page + "1"},
		},
		{
			name: "what follows the urlset is not read",
			data: "<urlset>" + entry("1") + "</urlset>" + entry("after") + "<",
			want: []string{page + "1"},
		},
		{
			name: "no element",
			data: "<!-- comment -->",
			err:  "EOF",
		},
		{
			name: "skipped element is not closed",
			data: "<urlset>" + entry("1") + "<other><loc>",
			want: []string{page + "1"},
			err:  "XML syntax error on line 1: unexpected EOF",
		},
		{
			name: "url is not closed",
			data: "<urlset>" + entry("1") + "<url><loc>" + page + "2",
			want: []string{page + "1"},
			err:  "XML syntax error on line 1: unexpected EOF",
		},
		{
			name: "urlset is not closed",
			data: "<urlset>" + entry("1"),
			want: []string{page + "1"},
			err:  "XML syntax error on line 1: unexpected EOF",
		},
	}

	for _, test := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s strict=%v", test.name, strict), func(t *testing.T) {
				us, err := decodeURLSet(New().SetStrict(strict), test.data)

				var got []string
				for _, e := range us.URL {
					got = append(got, e.Loc)
				}
				assertStringSlice(t, "entries", got, test.want)
				if test.err == "" {
					if err != nil {
						t.Errorf("unexpected error: %v", err)
					}
				} else if err == nil || err.Error() != test.err {
					t.Errorf("expected error %q, got %v", test.err, err)
				}
			})
		}
	}
}

// TestS_parseURLSetContent_AllOrNothing verifies that a <urlset> that cannot be
// read to its end adds nothing but the error about that: neither the URLs of
// the entries that precede the place of the error, nor the errors about them.
// What was collected before the document is kept.
func TestS_parseURLSetContent_AllOrNothing(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	// Two valid entries and one that is reported, then the document breaks off.
	const content = `<urlset><url><loc>https://example.com/a</loc></url><url><loc>ftp://example.com/b</loc></url><url><loc>https://example.com/c</loc></url><url><loc>https://example.com/d`

	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("strict=%v", strict), func(t *testing.T) {
			earlier := errors.New("earlier error")
			s := New().SetStrict(strict)
			s.urls = []URL{{Loc: "https://example.com/earlier"}}
			s.errs = []error{earlier}

			s.parseURLSetContent(url, content)

			mustEqual(t, "URLs", len(s.urls), 1)
			mustEqual(t, "URL kept", s.urls[0].Loc, "https://example.com/earlier")
			if len(s.errs) != 2 {
				t.Fatalf("expected 2 errors, got %d: %v", len(s.errs), s.errs)
			}
			if s.errs[0] != earlier {
				t.Errorf("expected the earlier error to be kept, got %v", s.errs[0])
			}
			var parseErr *ParseError
			if !errors.As(s.errs[1], &parseErr) {
				t.Fatalf("expected *ParseError, got %T: %v", s.errs[1], s.errs[1])
			}
			mustEqual(t, "error URL", parseErr.URL, url)
			if !strings.Contains(parseErr.Error(), "unexpected EOF") {
				t.Errorf("error does not report the unexpected EOF: %v", parseErr)
			}

			// The same content, complete, yields the entries and the error about one of them.
			s = New().SetStrict(strict)
			s.parseURLSetContent(url, content+`</loc></url></urlset>`)
			mustEqual(t, "URLs of the complete document", len(s.urls), 3)
			mustEqual(t, "errors of the complete document", len(s.errs), 1)
		})
	}
}

func TestS_parseRSS(t *testing.T) {
	t.Run("empty content returns error", func(t *testing.T) {
		s := New()
		_, err := s.parseRSS("")
		if err == nil || err.Error() != "rss is empty" {
			t.Errorf("expected %q, got %v", "rss is empty", err)
		}
	})

	t.Run("valid RSS with multiple items", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <item><link>http://example.com/item-1</link></item>
    <item><link>http://example.com/item-2</link></item>
    <item><link>http://example.com/item-3</link></item>
  </channel>
</rss>`
		s := New()
		rss, err := s.parseRSS(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rss.Channel.Item) != 3 {
			t.Fatalf("expected 3 items, got %d", len(rss.Channel.Item))
		}
		if rss.Channel.Item[0].Link != "http://example.com/item-1" {
			t.Errorf("item[0].Link: got %q", rss.Channel.Item[0].Link)
		}
		if rss.Channel.Item[1].Link != "http://example.com/item-2" {
			t.Errorf("item[1].Link: got %q", rss.Channel.Item[1].Link)
		}
		if rss.Channel.Item[2].Link != "http://example.com/item-3" {
			t.Errorf("item[2].Link: got %q", rss.Channel.Item[2].Link)
		}
	})

	t.Run("valid RSS with no items", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Empty</title></channel></rss>`
		s := New()
		rss, err := s.parseRSS(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rss.Channel.Item) != 0 {
			t.Errorf("expected 0 items, got %d", len(rss.Channel.Item))
		}
	})

	t.Run("malformed XML returns error", func(t *testing.T) {
		data := `<?xml version="1.0"?><rss version="2.0"><channel><item>`
		s := New()
		_, err := s.parseRSS(data)
		if err == nil {
			t.Error("expected error for malformed XML, got nil")
		}
	})
}

func TestS_parseAtom(t *testing.T) {
	t.Run("empty content returns error", func(t *testing.T) {
		s := New()
		_, err := s.parseAtom("")
		if err == nil || err.Error() != "atom is empty" {
			t.Errorf("expected %q, got %v", "atom is empty", err)
		}
	})

	t.Run("valid Atom with alternate and empty-rel links", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link href="http://example.com/entry-1"/>
  </entry>
  <entry>
    <link rel="alternate" href="http://example.com/entry-2"/>
  </entry>
  <entry>
    <link rel="self" href="http://example.com/entry-3-self"/>
    <link rel="alternate" href="http://example.com/entry-3-alt"/>
  </entry>
</feed>`
		s := New()
		atom, err := s.parseAtom(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(atom.Entry) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(atom.Entry))
		}
		// entry[0]: empty rel → treated as alternate
		if atom.Entry[0].Link[0].Href != "http://example.com/entry-1" {
			t.Errorf("entry[0] href: got %q", atom.Entry[0].Link[0].Href)
		}
		// entry[1]: rel="alternate"
		if atom.Entry[1].Link[0].Rel != "alternate" || atom.Entry[1].Link[0].Href != "http://example.com/entry-2" {
			t.Errorf("entry[1]: got rel=%q href=%q", atom.Entry[1].Link[0].Rel, atom.Entry[1].Link[0].Href)
		}
		// entry[2]: has both self and alternate links
		if len(atom.Entry[2].Link) != 2 {
			t.Fatalf("entry[2]: expected 2 links, got %d", len(atom.Entry[2].Link))
		}
	})

	t.Run("valid Atom with no entries", func(t *testing.T) {
		data := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Empty</title></feed>`
		s := New()
		atom, err := s.parseAtom(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(atom.Entry) != 0 {
			t.Errorf("expected 0 entries, got %d", len(atom.Entry))
		}
	})

	t.Run("malformed XML returns error", func(t *testing.T) {
		data := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry>`
		s := New()
		_, err := s.parseAtom(data)
		if err == nil {
			t.Error("expected error for malformed XML, got nil")
		}
	})
}

// flipByte returns s with the byte that is back bytes before its end inverted.
func flipByte(s string, back int) string {
	b := []byte(s)
	b[len(b)-back] ^= 0xff
	return string(b)
}

// TestUnzip verifies that gzip content is decompressed, and that content which cannot be
// decompressed to its end is rejected with an error that says so, and without any data.
func TestUnzip(t *testing.T) {
	valid := string(gzipByte("hello world"))

	tests := []struct {
		name    string
		input   string
		output  string
		wantErr string
	}{
		{
			name:   "Valid content",
			input:  valid,
			output: "hello world",
		},
		{
			name:   "Valid content that is empty",
			input:  string(gzipByte("")),
			output: "",
		},
		{
			name:    "Invalid gzip content",
			input:   "\x1f\x8b\x08" + "invalid",
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Invalid content",
			input:   "invalid content",
			wantErr: "gzip decompression failed: gzip: invalid header",
		},
		{
			name:    "Empty content",
			input:   "",
			wantErr: "gzip decompression failed: EOF",
		},
		{
			name:    "Content cut short in its header",
			input:   valid[:3],
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Content cut short in its data",
			input:   valid[:len(valid)-10],
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Content cut short in its trailer",
			input:   valid[:len(valid)-1],
			wantErr: "gzip decompression failed: unexpected EOF",
		},
		{
			name:    "Content with a wrong checksum",
			input:   flipByte(valid, 8),
			wantErr: "gzip decompression failed: gzip: invalid checksum",
		},
		{
			name:    "Content with a wrong size",
			input:   flipByte(valid, 1),
			wantErr: "gzip decompression failed: gzip: invalid checksum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uncompressed, err := unzip(test.input, defaultMaxResponseSize)

			if test.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else if err == nil || err.Error() != test.wantErr {
				t.Errorf("expected error %q, got %v", test.wantErr, err)
			}

			if uncompressed != test.output {
				t.Errorf("expected %q, got %q", test.output, uncompressed)
			}

		})
	}
}

// TestUnzip_Members verifies that gzip content is read to the end of its last member: a gzip
// file is a series of members, and its content is that of all of them. What follows the last
// member and is no member is not read, and a member that is cut short or damaged fails the
// content whichever member it is.
func TestUnzip_Members(t *testing.T) {
	const first, second, third = "first member,", "second member,", "third member"
	a, b, c := string(gzipByte(first)), string(gzipByte(second)), string(gzipByte(third))
	empty := string(gzipByte(""))

	// Members that do not fit the buffer the content is read through, so that a member ends
	// in the middle of a read as well as at the end of one.
	large := make([]string, 3)
	for i := range large {
		var lines strings.Builder
		for line := 0; lines.Len() < 100_000; line++ {
			_, _ = fmt.Fprintf(&lines, "https://example.com/member-%d/page-%d\n", i, line)
		}
		large[i] = lines.String()
	}

	tests := []struct {
		name    string
		input   string
		output  string
		wantErr string
	}{
		{name: "one member", input: a, output: first},
		{name: "two members", input: a + b, output: first + second},
		{name: "three members", input: a + b + c, output: first + second + third},
		{name: "the same member twice", input: a + a, output: first + first},
		{name: "an empty member first", input: empty + b, output: second},
		{name: "an empty member in between", input: a + empty + c, output: first + third},
		{name: "an empty member last", input: a + empty, output: first},
		{name: "empty members only", input: empty + empty + empty, output: ""},
		{name: "members of one byte", input: string(gzipByte("a")) + string(gzipByte("b")) + string(gzipByte("c")), output: "abc"},
		{
			name:   "members larger than the read buffer",
			input:  string(gzipByte(large[0])) + string(gzipByte(large[1])) + string(gzipByte(large[2])),
			output: large[0] + large[1] + large[2],
		},

		// What follows the last member and is no member is left unread.
		{name: "a newline after the member", input: a + "\n", output: first},
		{name: "a newline after the members", input: a + b + "\n", output: first + second},
		{name: "zero padding after the members", input: a + b + strings.Repeat("\x00", 512), output: first + second},
		{name: "text after the members", input: a + b + "what a server may send after the content", output: first + second},
		{name: "the gzip identification alone after the members", input: a + b + "\x1f\x8b", output: first + second},
		{name: "another compression method after the members", input: a + b + "\x1f\x8b\x07" + b[3:], output: first + second},
		// A member that does not follow a member directly is not read either.
		{name: "a member after a newline", input: a + "\n" + b, output: first},
		{name: "a member after zero padding", input: a + "\x00\x00\x00\x00" + b, output: first},

		// A member that is cut short or damaged fails the content.
		{name: "second member cut short in its header", input: a + b[:5], wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "second member cut short after its header", input: a + b[:10], wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "second member cut short in its data", input: a + b[:len(b)-10], wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "second member cut short in its trailer", input: a + b[:len(b)-1], wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "second member with a wrong checksum", input: a + flipByte(b, 8), wantErr: "gzip decompression failed: gzip: invalid checksum"},
		{name: "second member with a wrong size", input: a + flipByte(b, 1), wantErr: "gzip decompression failed: gzip: invalid checksum"},
		{name: "third member cut short", input: a + b + c[:len(c)-1], wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "first member with a wrong checksum", input: flipByte(a, 8) + b, wantErr: "gzip decompression failed: gzip: invalid checksum"},
		{name: "what begins like a member and is none", input: a + "\x1f\x8b\x08 not gzip", wantErr: "gzip decompression failed: unexpected EOF"},
		{name: "nothing but the beginning of a member", input: a + "\x1f\x8b\x08", wantErr: "gzip decompression failed: unexpected EOF"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uncompressed, err := unzip(test.input, defaultMaxResponseSize)

			if test.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else if err == nil || err.Error() != test.wantErr {
				t.Errorf("expected error %q, got %v", test.wantErr, err)
			}

			if uncompressed != test.output {
				t.Errorf("expected %d bytes, got %d: %.80q", len(test.output), len(uncompressed), uncompressed)
			}
		})
	}
}

func TestUnzip_SizeLimit(t *testing.T) {
	payload := strings.Repeat("A", 1024)
	gzipped := string(gzipByte(payload))

	tests := []struct {
		name     string
		maxSize  int64
		hasError bool
	}{
		{name: "Limit above payload size", maxSize: 2048},
		{name: "Limit equal to payload size", maxSize: 1024},
		{name: "Limit one byte below payload size", maxSize: 1023, hasError: true},
		{name: "Limit far below payload size", maxSize: 1, hasError: true},
		{name: "Maximum limit does not overflow", maxSize: math.MaxInt64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uncompressed, err := unzip(gzipped, test.maxSize)

			if !test.hasError {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				mustEqual(t, "content", uncompressed, payload)
				return
			}

			if err == nil {
				t.Fatal("expected a size limit error, got nil")
			}
			want := fmt.Sprintf("decompressed size exceeds limit of %d bytes", test.maxSize)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("expected error containing %q, got %v", want, err)
			}
			if uncompressed != "" {
				t.Errorf("expected no data alongside a size limit error, got %d bytes", len(uncompressed))
			}
		})
	}

	t.Run("the limit is on all members together", func(t *testing.T) {
		// Two members of 1,024 bytes each.
		members := gzipped + gzipped

		for _, maxSize := range []int64{1, 1023, 1024, 1025, 2047} {
			uncompressed, err := unzip(members, maxSize)
			if err == nil {
				t.Fatalf("limit %d: expected a size limit error, got nil", maxSize)
			}
			mustEqual(t, "error", err.Error(), fmt.Sprintf("decompressed size exceeds limit of %d bytes", maxSize))
			mustEqual(t, "content", uncompressed, "")
		}
		for _, maxSize := range []int64{2048, 2049, math.MaxInt64} {
			uncompressed, err := unzip(members, maxSize)
			if err != nil {
				t.Fatalf("limit %d: expected no error, got %v", maxSize, err)
			}
			mustEqual(t, "content", uncompressed, payload+payload)
		}
	})

	t.Run("no more is decompressed than one byte past the limit, however many members there are", func(t *testing.T) {
		// A decompression bomb of 64 members: 1 MB each, a kilobyte compressed. The limit is
		// reached in the second.
		const members, memberSize = 64, 1024 * 1024
		const limit = memberSize + 1024
		bomb := strings.Repeat(string(gzipByte(strings.Repeat("A", memberSize))), members)

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		uncompressed, err := unzip(bomb, limit)
		runtime.ReadMemStats(&after)

		if err == nil {
			t.Fatal("expected a size limit error, got nil")
		}
		mustEqual(t, "error", err.Error(), fmt.Sprintf("decompressed size exceeds limit of %d bytes", limit))
		mustEqual(t, "content", uncompressed, "")
		// What is read takes up the limit, and a few times as much while the string it is
		// read into grows. All the members would take up 64 MB.
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > members*memberSize/4 {
			t.Errorf("%d bytes were allocated to decompress up to a limit of %d bytes", allocated, limit)
		}
	})

	t.Run("no more is decompressed than one byte past the limit", func(t *testing.T) {
		const limit = 1024
		// A decompression bomb: 4 MB that compress to a few kilobytes.
		const size = 4 * 1024 * 1024
		bomb := string(gzipByte(strings.Repeat("A", size)))

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		uncompressed, err := unzip(bomb, limit)
		runtime.ReadMemStats(&after)

		if err == nil {
			t.Fatal("expected a size limit error, got nil")
		}
		mustEqual(t, "error", err.Error(), fmt.Sprintf("decompressed size exceeds limit of %d bytes", limit))
		mustEqual(t, "content", uncompressed, "")
		// What is read takes up a kilobyte, the reader and its buffers some tens of them.
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/8 {
			t.Errorf("%d bytes were allocated to decompress up to a limit of %d bytes", allocated, limit)
		}
	})
}

func TestReadString(t *testing.T) {
	t.Run("content is returned as it was read", func(t *testing.T) {
		// Sizes around that of the buffer the content is read through.
		for _, size := range []int{0, 1, 32*1024 - 1, 32 * 1024, 32*1024 + 1, 200 * 1024} {
			want := strings.Repeat("0123456789", size/10+1)[:size]

			got, err := readString(strings.NewReader(want))

			if err != nil {
				t.Fatalf("%d bytes: unexpected error: %v", size, err)
			}
			if got != want {
				t.Errorf("%d bytes: got %d bytes that differ from what was read", size, len(got))
			}
		}
	})

	t.Run("what was read is returned with the error", func(t *testing.T) {
		failure := errors.New("read failed")

		got, err := readString(io.MultiReader(strings.NewReader("read until then"), iotest.ErrReader(failure)))

		if err != failure {
			t.Errorf("expected %v, got %v", failure, err)
		}
		mustEqual(t, "content", got, "read until then")
	})

	t.Run("buffer is not allocated for every read", func(t *testing.T) {
		const reads = 1000

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < reads; i++ {
			if _, err := readString(strings.NewReader("small")); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		runtime.ReadMemStats(&after)

		// A buffer is 32 KB. The race detector has a pool drop some of what is put back
		// into it, so a read is not required to allocate nothing at all.
		if perRead := (after.TotalAlloc - before.TotalAlloc) / reads; perRead > 16*1024 {
			t.Errorf("%d bytes are allocated for a read of 5 bytes", perRead)
		}
	})
}

// endless is a source that does not come to an end: it holds more than a read
// with a limit may take from it. It counts the bytes that were read from it,
// and fails the read that goes beyond endlessSize, so that a read without a
// limit fails instead of taking up all the memory there is.
type endless struct {
	read int64
}

// endlessSize is far beyond every limit the tests set for a read from endless.
const endlessSize = 16 * 1024 * 1024

func (e *endless) Read(p []byte) (int, error) {
	if e.read >= endlessSize {
		return 0, errors.New("read far past the limit")
	}
	clear(p)
	e.read += int64(len(p))
	return len(p), nil
}

// TestReadAtMost verifies that a read goes one byte past its limit and no
// further: the byte that tells content which exceeds the limit from content
// that fits it exactly. The largest limit there is has no byte past it, and
// reads everything.
func TestReadAtMost(t *testing.T) {
	const content = "0123456789"

	tests := []struct {
		name  string
		limit int64
		want  string
	}{
		{"limit above the size", 11, content},
		{"limit equal to the size", 10, content},
		{"limit one byte below the size", 9, content},
		{"limit below the size", 4, "01234"},
		{"limit of zero", 0, "0"},
		{"limit one below the maximum", math.MaxInt64 - 1, content},
		{"maximum limit", math.MaxInt64, content},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readAtMost(strings.NewReader(content), tt.limit)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustEqual(t, "content", got, tt.want)
		})
	}

	t.Run("no more is read than one byte past the limit", func(t *testing.T) {
		// Limits around the size of the buffer the content is read through.
		for _, limit := range []int64{0, 1, 32*1024 - 1, 32 * 1024, 32*1024 + 1, 200 * 1024} {
			source := &endless{}

			got, err := readAtMost(source, limit)

			if err != nil {
				t.Fatalf("limit of %d bytes: unexpected error: %v", limit, err)
			}
			mustEqual(t, "bytes returned", int64(len(got)), limit+1)
			mustEqual(t, "bytes read", source.read, limit+1)
		}
	})

	t.Run("what was read is returned with the error", func(t *testing.T) {
		failure := errors.New("read failed")

		got, err := readAtMost(io.MultiReader(strings.NewReader("read until then"), iotest.ErrReader(failure)), 1024)

		if err != failure {
			t.Errorf("expected %v, got %v", failure, err)
		}
		mustEqual(t, "content", got, "read until then")
	})
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

			var got LastModTime
			err = got.UnmarshalXML(decoder, startElement)

			if (err != nil) != tt.wantErr {
				t.Errorf("LastModTime.UnmarshalXML() error = %v, expected error: %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				gotTime := got.Time
				if !gotTime.Equal(tt.want) {
					t.Errorf("LastModTime.UnmarshalXML() = %v, expected value: %v", gotTime, tt.want)
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
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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

	_, _, err := s.fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestS_ParseContext_Cancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	s := New()
	//nolint:staticcheck // intentionally passing nil to exercise the defensive branch
	body, _, err := s.fetch(nil, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "ok" {
		t.Errorf("unexpected body: %q", body)
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
	// Covers the final `if ctxErr := ctx.Err(); ctxErr != nil { ... }` on the
	// robots.txt path: with the context already cancelled, none of the sitemaps
	// the robots.txt lists is fetched, and the call reports that it was cut
	// short.
	// We pre-supply the robots.txt body via urlContent so setContent does not
	// perform an HTTP fetch (which would fail before the sitemaps are reached).
	robots := "Sitemap: http://127.0.0.1:1/sitemap.xml\n"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	s := New()
	_, err := s.ParseContext(ctx, "http://example.com/robots.txt", &robots)
	if cutShort := requireCutShort(t, s, err, "http://example.com/robots.txt", context.Canceled); cutShort != 0 {
		t.Errorf("expected no other error, got %v", s.GetErrors())
	}
}

func TestS_parseAndFetchUrlsMultiThread_PreCancelled(t *testing.T) {
	// Covers the loop-level `if ctx.Err() != nil { break }` branch in
	// parseAndFetchUrlsMultiThread.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	s.parseAndFetchUrlsMultiThread(ctx, "http://127.0.0.1:1/index.xml", []string{"http://127.0.0.1:1/a", "http://127.0.0.1:1/b"}, 0)

	// Nothing is fetched, and nothing is recorded: it is ParseContext that
	// records that the call was cut short.
	mustEqual(t, "errors", len(s.errs), 0)
}

func TestS_parseAndFetchUrlsMultiThread_AcquireSlotCancel(t *testing.T) {
	// Covers the acquireSlot ctx-cancel error branch of
	// parseAndFetchUrlsMultiThread. We pre-saturate the semaphore so the slot
	// of the first location has to be waited for, then cancel the context.
	// The loop-level ctx.Err() break is bypassed by using a context that
	// becomes cancelled only after the wait has begun.
	s := New().SetMaxConcurrency(1)
	s.sem = make(chan struct{}, 1)
	s.sem <- struct{}{} // saturate

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	s.parseAndFetchUrlsMultiThread(ctx, "http://127.0.0.1:1/index.xml", []string{"http://127.0.0.1:1/a", "http://127.0.0.1:1/b", "http://127.0.0.1:1/c"}, 0)

	// Nothing is recorded for the wait that was cut short, nor for the
	// locations that were still to come: it is ParseContext that records that
	// the call was cut short. Nothing is fetched without a slot either, a
	// fetch attempt would have left an error of its own.
	if len(s.errs) != 0 {
		t.Fatalf("expected no errors, got %d: %v", len(s.errs), s.errs)
	}
	// The location the slot was waited for is the only one that was reached.
	mustEqual(t, "locations reached", len(s.fetchedURLs), 1)
	mustEqual(t, "slots taken", len(s.sem), 1)
}

func TestS_parseAndFetchUrlsSequential_PreCancelled(t *testing.T) {
	// Covers the early ctx.Err() return in parseAndFetchUrlsSequential.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New()
	s.parseAndFetchUrlsSequential(ctx, "http://127.0.0.1:1/index.xml", []string{"http://127.0.0.1:1/a"}, 0)

	mustEqual(t, "errors", len(s.errs), 0)
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

// TestS_SetMaxSitemaps_SetMaxURLs verifies what the setters of the two limits
// of a call accept, and that a value they reject leaves the setting as it was.
func TestS_SetMaxSitemaps_SetMaxURLs(t *testing.T) {
	settings := []struct {
		field    string
		set      func(s *S, value int) *S
		get      func(s *S) int
		standard int
	}{
		{"maxSitemaps", (*S).SetMaxSitemaps, (*S).GetMaxSitemaps, defaultMaxSitemaps},
		{"maxURLs", (*S).SetMaxURLs, (*S).GetMaxURLs, defaultMaxURLs},
	}

	for _, setting := range settings {
		t.Run(setting.field+", default", func(t *testing.T) {
			mustEqual(t, "value", setting.get(New()), setting.standard)
		})

		for _, value := range []int{1, 25, math.MaxInt} {
			t.Run(fmt.Sprintf("%s, %d", setting.field, value), func(t *testing.T) {
				s := setting.set(New(), value)
				mustEqual(t, "value", setting.get(s), value)
				mustEqual(t, "errors", len(s.GetErrors()), 0)
			})
		}

		t.Run(setting.field+", 0 lifts the limit", func(t *testing.T) {
			s := setting.set(New(), 0)
			mustEqual(t, "value", setting.get(s), 0)
			mustEqual(t, "errors", len(s.GetErrors()), 0)
		})

		t.Run(setting.field+", negative value is rejected", func(t *testing.T) {
			s := setting.set(setting.set(New(), 25), -1)
			mustEqual(t, "value", setting.get(s), 25)

			errs := s.GetErrors()
			if len(errs) != 1 {
				t.Fatalf("expected 1 error, got %v", errs)
			}
			var configErr *ConfigError
			if !errors.As(errs[0], &configErr) {
				t.Fatalf("expected *ConfigError, got %T: %v", errs[0], errs[0])
			}
			mustEqual(t, "field", configErr.Field, setting.field)
			mustEqual(t, "error", errs[0].Error(), fmt.Sprintf("config %q: must be >= 0, got -1", setting.field))
		})

		t.Run(setting.field+", returns the instance", func(t *testing.T) {
			s := New()
			if setting.set(s, 5) != s || setting.set(s, -5) != s {
				t.Error("expected the setter to return the instance it was called on")
			}
		})
	}
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
	// Pre-cancelled ctx + maxConcurrency=1: the context error is returned
	// with a bounded number of fetch slots as well.
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
	// points to a sitemap index, and maxConcurrency is 1. The goroutine that
	// fetches the sitemap index must release its semaphore slot before
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

// TestS_Parse_RobotsTXT_MultiThread verifies that the sitemaps a robots.txt
// lists are fetched the way SetMultiThread asks for: one at a time and in the
// order they are listed when it is off, concurrently when it is on.
func TestS_Parse_RobotsTXT_MultiThread(t *testing.T) {
	const sitemaps = 4

	tests := []struct {
		name           string
		multiThread    bool
		maxConcurrency int
		// passContent tells whether the robots.txt is handed to Parse or fetched by it.
		passContent bool
		// wantInFlight is the highest number of sitemap requests in flight at once.
		wantInFlight int
	}{
		{"sequential, fetched robots.txt", false, defaultMaxConcurrency, false, 1},
		{"sequential, robots.txt passed as content", false, defaultMaxConcurrency, true, 1},
		{"multi-thread, fetched robots.txt", true, 0, false, sitemaps},
		{"multi-thread, robots.txt passed as content", true, 0, true, sitemaps},
		{"multi-thread, two fetches at most", true, 2, false, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A request for a sitemap is held until as many requests have been
			// in flight at once as the test expects at most, two at least, or
			// until hold has passed. Fetched one at a time, no second request
			// ever joins the first, so hold is short; fetched concurrently, the
			// requests let each other go at once and hold is only a safety net.
			hold := 10 * time.Millisecond
			if tt.multiThread {
				hold = 5 * time.Second
			}
			var mu sync.Mutex
			inFlight, maxInFlight := 0, 0
			overlapped := make(chan struct{})
			var overlappedOnce sync.Once

			var srv *httptest.Server
			robots := func() string {
				var b strings.Builder
				for i := range sitemaps {
					_, _ = fmt.Fprintf(&b, "Sitemap: %s/sitemap-%d.xml\n", srv.URL, i)
				}
				return b.String()
			}
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					_, _ = fmt.Fprint(w, robots())
					return
				}

				mu.Lock()
				inFlight++
				maxInFlight = max(maxInFlight, inFlight)
				if inFlight >= max(tt.wantInFlight, 2) {
					overlappedOnce.Do(func() { close(overlapped) })
				}
				mu.Unlock()

				select {
				case <-overlapped:
				case <-time.After(hold):
				}

				mu.Lock()
				inFlight--
				mu.Unlock()
				_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s%s/page</loc></url></urlset>`, srv.URL, r.URL.Path)
			}))
			defer srv.Close()

			var content *string
			if tt.passContent {
				content = pointerOfString(robots())
			}
			s := New().SetMultiThread(tt.multiThread).SetMaxConcurrency(tt.maxConcurrency)
			requireParse(t, s, srv.URL+"/robots.txt", content)

			assertCounts(t, s, sitemaps, 0)
			mu.Lock()
			got := maxInFlight
			mu.Unlock()
			mustEqual(t, "requests in flight at once", got, tt.wantInFlight)

			if !tt.multiThread {
				for i, u := range s.GetURLs() {
					mustEqual(t, fmt.Sprintf("URL %d", i), u.Loc, fmt.Sprintf("%s/sitemap-%d.xml/page", srv.URL, i))
				}
			}
		})
	}
}

// TestS_Parse_RobotsTXT_MaxDepth verifies that a robots.txt does not count
// towards the depth limit: the sitemaps it lists are followed as deep as the
// main URL of a call is when that is a sitemap index itself.
func TestS_Parse_RobotsTXT_MaxDepth(t *testing.T) {
	// robots.txt -> index.xml -> pages.xml (one URL)
	//                         -> nested.xml -> deep.xml (one URL)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = fmt.Fprintf(w, "Sitemap: %s/index.xml\n", srv.URL)
		case "/index.xml":
			_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%[1]s/pages.xml</loc></sitemap><sitemap><loc>%[1]s/nested.xml</loc></sitemap></sitemapindex>`, srv.URL)
		case "/nested.xml":
			_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/deep.xml</loc></sitemap></sitemapindex>`, srv.URL)
		default:
			_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s%s/page</loc></url></urlset>`, srv.URL, r.URL.Path)
		}
	}))
	defer srv.Close()

	tests := []struct {
		name     string
		maxDepth int
		wantURLs int64
		wantErrs int64
	}{
		{"nested index is not followed", 1, 1, 1},
		{"nested index is followed", 2, 2, 0},
	}

	for _, tt := range tests {
		for _, path := range []string{"/robots.txt", "/index.xml"} {
			for _, multiThread := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, %s, multiThread=%v", tt.name, path, multiThread), func(t *testing.T) {
					s := New().SetMaxDepth(tt.maxDepth).SetMultiThread(multiThread)
					requireParse(t, s, srv.URL+path, nil)

					assertCounts(t, s, tt.wantURLs, tt.wantErrs)
					// The error names the sitemap index whose sitemaps are not followed.
					var notFollowed []string
					if tt.wantErrs > 0 {
						notFollowed = []string{srv.URL + "/nested.xml"}
					}
					requireDepthLimitErrors(t, s.GetErrors(), tt.maxDepth, notFollowed...)
				})
			}
		}
	}
}

// roundTripperFunc is an http.RoundTripper made of a function.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// movedAndTarget starts two servers, which are two hosts, as they listen on
// different ports. The one at movedURL redirects every request to the same
// path at targetURL; the one at targetURL answers with the handler that
// handle makes of the two URLs.
func movedAndTarget(t *testing.T, handle func(movedURL, targetURL string) http.HandlerFunc) (movedURL, targetURL string) {
	t.Helper()
	moved := httptest.NewUnstartedServer(nil)
	target := httptest.NewUnstartedServer(nil)
	movedURL = "http://" + moved.Listener.Addr().String()
	targetURL = "http://" + target.Listener.Addr().String()
	moved.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL+r.URL.Path, http.StatusMovedPermanently)
	})
	target.Config.Handler = handle(movedURL, targetURL)
	moved.Start()
	target.Start()
	t.Cleanup(moved.Close)
	t.Cleanup(target.Close)
	return movedURL, targetURL
}

// sortedCopy returns the elements of list in sorted order, in a slice that is
// never nil.
func sortedCopy(list []string) []string {
	sorted := append([]string{}, list...)
	sort.Strings(sorted)
	return sorted
}

// TestS_fetch_ServedFrom verifies the URL that fetch reports a document was
// served from: the requested URL, as it was passed in, or the URL the request
// was redirected to.
func TestS_fetch_ServedFrom(t *testing.T) {
	movedURL, targetURL := movedAndTarget(t, func(_, _ string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/twice" {
				http.Redirect(w, r, "/sitemap.xml", http.StatusFound)
				return
			}
			_, _ = fmt.Fprint(w, r.URL.Path)
		}
	})
	// The scheme is not written the way a parsed URL prints it.
	upperCaseURL := strings.Replace(targetURL, "http://", "HTTP://", 1) + "/sitemap.xml"

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"not redirected", targetURL + "/sitemap.xml", targetURL + "/sitemap.xml"},
		{"not redirected, URL kept as it was passed in", upperCaseURL, upperCaseURL},
		{"redirected", movedURL + "/sitemap.xml", targetURL + "/sitemap.xml"},
		{"redirected twice", movedURL + "/twice", targetURL + "/sitemap.xml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, servedFrom, err := New().fetch(context.Background(), tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustEqual(t, "served from", servedFrom, tt.want)
			mustEqual(t, "content", content, "/sitemap.xml")
		})
	}

	// A RoundTripper is free to leave Response.Request unset. The requested
	// URL is all there is to go by then.
	requests := map[string]*http.Request{
		"transport that names no request":            nil,
		"transport that names a request without URL": {},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			})}
			_, servedFrom, err := New().SetHTTPClient(client).fetch(context.Background(), "https://example.com/sitemap.xml")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustEqual(t, "served from", servedFrom, "https://example.com/sitemap.xml")
		})
	}
}

// TestS_Parse_Redirect verifies that a sitemap reached through a redirect is
// treated as located where it was served from: relative URLs are resolved
// against that URL, strict mode compares the URLs with it, and the errors
// about the document name it.
func TestS_Parse_Redirect(t *testing.T) {
	movedURL, targetURL := movedAndTarget(t, func(movedURL, targetURL string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/robots.txt":
				// The sitemap index is listed at the host it has moved from.
				_, _ = fmt.Fprintf(w, "Sitemap: %s/index.xml\n", movedURL)
			case "/index.xml":
				_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/pages.xml</loc></sitemap><sitemap><loc>/relative.xml</loc></sitemap></sitemapindex>`, targetURL)
			case "/pages.xml":
				_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s/page-1</loc></url><url><loc>%s/page-2</loc></url></urlset>`, targetURL, movedURL)
			case "/relative.xml":
				_, _ = fmt.Fprint(w, `<urlset><url><loc>/page-3</loc></url></urlset>`)
			case "/faulty/robots.txt":
				_, _ = fmt.Fprintf(w, "Sitemap: %[1]s/no-loc.xml\nSitemap: %[1]s/corrupt.xml.gz\n", movedURL)
			case "/no-loc.xml":
				_, _ = fmt.Fprint(w, `<urlset><url><lastmod>2024-01-15</lastmod></url></urlset>`)
			case "/corrupt.xml.gz":
				_, _ = w.Write([]byte("\x1f\x8b\x08 not gzip"))
			case "/old/robots.txt":
				http.Redirect(w, r, "/new/robots.txt", http.StatusFound)
			case "/new/robots.txt":
				_, _ = fmt.Fprint(w, "Sitemap: sitemap.xml\n")
			case "/new/sitemap.xml":
				_, _ = fmt.Fprint(w, `<urlset><url><loc>page-4</loc></url></urlset>`)
			default:
				http.NotFound(w, r)
			}
		}
	})
	movedHost, targetHost := strings.TrimPrefix(movedURL, "http://"), strings.TrimPrefix(targetURL, "http://")

	noLoc := fmt.Sprintf(`validate "%s/no-loc.xml": <loc> of an entry is empty or missing`, targetURL)
	corrupt := fmt.Sprintf(`parse "%s/corrupt.xml.gz": gzip decompression failed: unexpected EOF`, targetURL)
	otherHost := fmt.Sprintf(`validate "%s/page-2": strict mode: host %q does not match sitemap host %q`, movedURL, movedHost, targetHost)
	relativeSitemap := `validate "/relative.xml": strict mode: unsupported scheme ""`

	tests := []struct {
		name     string
		strict   bool
		url      string
		wantURLs []string
		wantErrs []string
		// wantErr is the error Parse returns, if it returns one.
		wantErr string
	}{
		{
			name:     "relative URL of a redirected sitemap",
			url:      movedURL + "/relative.xml",
			wantURLs: []string{targetURL + "/page-3"},
		},
		{
			name:     "relative sitemap of a redirected sitemap index",
			url:      movedURL + "/index.xml",
			wantURLs: []string{targetURL + "/page-1", movedURL + "/page-2", targetURL + "/page-3"},
		},
		{
			name:     "redirected sitemap index of a robots.txt",
			url:      targetURL + "/robots.txt",
			wantURLs: []string{targetURL + "/page-1", movedURL + "/page-2", targetURL + "/page-3"},
		},
		{
			name:     "redirected robots.txt",
			url:      movedURL + "/robots.txt",
			wantURLs: []string{targetURL + "/page-1", movedURL + "/page-2", targetURL + "/page-3"},
		},
		{
			name:     "relative sitemap of a redirected robots.txt",
			url:      targetURL + "/old/robots.txt",
			wantURLs: []string{targetURL + "/new/page-4"},
		},
		{
			name:     "strict mode, redirected sitemap",
			strict:   true,
			url:      movedURL + "/pages.xml",
			wantURLs: []string{targetURL + "/page-1"},
			wantErrs: []string{otherHost},
		},
		{
			name:     "strict mode, redirected sitemap index",
			strict:   true,
			url:      movedURL + "/index.xml",
			wantURLs: []string{targetURL + "/page-1"},
			wantErrs: []string{otherHost, relativeSitemap},
		},
		{
			name:     "strict mode, redirected sitemap index of a robots.txt",
			strict:   true,
			url:      targetURL + "/robots.txt",
			wantURLs: []string{targetURL + "/page-1"},
			wantErrs: []string{otherHost, relativeSitemap},
		},
		{
			name:     "error about a redirected sitemap",
			url:      movedURL + "/no-loc.xml",
			wantErrs: []string{noLoc},
		},
		{
			name:     "error about the gzip content of a redirected sitemap",
			url:      movedURL + "/corrupt.xml.gz",
			wantErrs: []string{corrupt},
			wantErr:  corrupt,
		},
		{
			name:     "errors about the redirected sitemaps of a robots.txt",
			url:      targetURL + "/faulty/robots.txt",
			wantErrs: []string{noLoc, corrupt},
		},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, multiThread=%v", tt.name, multiThread), func(t *testing.T) {
				s := New().SetStrict(tt.strict).SetMultiThread(multiThread)
				_, err := s.Parse(tt.url, nil)
				if tt.wantErr == "" {
					if err != nil {
						t.Fatalf("unexpected parse error: %v", err)
					}
				} else if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected parse error %q, got %v", tt.wantErr, err)
				}

				var gotURLs, gotErrs []string
				for _, u := range s.GetURLs() {
					gotURLs = append(gotURLs, u.Loc)
				}
				for _, err := range s.GetErrors() {
					gotErrs = append(gotErrs, err.Error())
				}
				assertStringSlice(t, "URLs", sortedCopy(gotURLs), sortedCopy(tt.wantURLs))
				assertStringSlice(t, "errors", sortedCopy(gotErrs), sortedCopy(tt.wantErrs))
			})
		}
	}
}

// TestS_robotsTXTSitemapLocations verifies which of the sitemaps a robots.txt
// names are fetched. What a Sitemap line holds is resolved, validated and
// matched against the follow patterns like the <loc> of a sitemap index entry,
// except that a robots.txt may name a sitemap of another host in strict mode
// as well.
func TestS_robotsTXTSitemapLocations(t *testing.T) {
	const robotsTXTURL = "https://example.com/robots.txt"
	tooLong := "https://example.com/" + strings.Repeat("a", maxLocLength)
	tooLongErr := fmt.Sprintf(`validate %q: URL exceeds maximum length of %d characters (%d)`, tooLong, maxLocLength, len(tooLong))

	tests := []struct {
		name   string
		strict bool
		follow []string
		// sitemaps holds the values of the Sitemap lines of the robots.txt.
		sitemaps []string
		want     []string
		wantErrs []string
	}{
		{
			name: "no Sitemap line",
		},
		{
			name:     "absolute URLs",
			sitemaps: []string{"https://example.com/sitemap.xml", "https://example.com/news.xml.gz"},
			want:     []string{"https://example.com/sitemap.xml", "https://example.com/news.xml.gz"},
		},
		{
			name:     "absolute URLs, strict mode",
			strict:   true,
			sitemaps: []string{"https://example.com/sitemap.xml", "https://example.com/news.xml.gz"},
			want:     []string{"https://example.com/sitemap.xml", "https://example.com/news.xml.gz"},
		},
		{
			name:     "sitemaps of another host and protocol",
			sitemaps: []string{"https://cdn.example.net/sitemap.xml", "http://example.com/sitemap.xml"},
			want:     []string{"https://cdn.example.net/sitemap.xml", "http://example.com/sitemap.xml"},
		},
		{
			name:     "sitemaps of another host and protocol, strict mode",
			strict:   true,
			sitemaps: []string{"https://cdn.example.net/sitemap.xml", "http://example.com/sitemap.xml"},
			want:     []string{"https://cdn.example.net/sitemap.xml", "http://example.com/sitemap.xml"},
		},
		{
			name:     "relative URLs are resolved",
			sitemaps: []string{"/sitemap.xml", "maps/news.xml", "//cdn.example.net/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml", "https://example.com/maps/news.xml", "https://cdn.example.net/sitemap.xml"},
		},
		{
			name:     "relative URLs are rejected in strict mode",
			strict:   true,
			sitemaps: []string{"/sitemap.xml", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{`validate "/sitemap.xml": strict mode: unsupported scheme ""`},
		},
		{
			name:     "URLs that are not HTTP(S)",
			sitemaps: []string{"ftp://example.com/sitemap.xml", "file:///etc/passwd", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{
				`validate "ftp://example.com/sitemap.xml": unsupported scheme "ftp"`,
				`validate "file:///etc/passwd": unsupported scheme "file"`,
			},
		},
		{
			name:     "URLs that are not HTTP(S), strict mode",
			strict:   true,
			sitemaps: []string{"ftp://example.com/sitemap.xml", "file:///etc/passwd", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{
				`validate "ftp://example.com/sitemap.xml": strict mode: unsupported scheme "ftp"`,
				`validate "file:///etc/passwd": strict mode: unsupported scheme "file"`,
			},
		},
		{
			name:     "URLs without a host",
			sitemaps: []string{"https:///sitemap.xml", "https://:8080/sitemap.xml", "//", "///sitemap.xml", "//:8080/sitemap.xml", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{
				`validate "https:///sitemap.xml": missing host`,
				`validate "https://:8080/sitemap.xml": missing host`,
				`validate "//": missing host`,
				`validate "///sitemap.xml": missing host`,
				`validate "//:8080/sitemap.xml": missing host`,
			},
		},
		{
			name:     "URLs without a host, strict mode",
			strict:   true,
			sitemaps: []string{"https:///sitemap.xml", "https://:8080/sitemap.xml", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{
				`validate "https:///sitemap.xml": strict mode: missing host`,
				`validate "https://:8080/sitemap.xml": strict mode: missing host`,
			},
		},
		{
			name:     "URL with a space in it",
			sitemaps: []string{"https://example.com/site map.xml?v=a b", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/site%20map.xml?v=a%20b", "https://example.com/sitemap.xml"},
		},
		{
			name:     "URL with a space in it, strict mode",
			strict:   true,
			sitemaps: []string{"https://example.com/site map.xml?v=a b", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{`validate "https://example.com/site map.xml?v=a b": strict mode: URL contains a space`},
		},
		{
			name:     "host in another letter case and on another port, strict mode",
			strict:   true,
			sitemaps: []string{"https://EXAMPLE.com:8443/sitemap.xml"},
			want:     []string{"https://EXAMPLE.com:8443/sitemap.xml"},
		},
		{
			name:     "URL that is too long",
			sitemaps: []string{tooLong, "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{tooLongErr},
		},
		{
			name:     "URL that is too long, strict mode",
			strict:   true,
			sitemaps: []string{tooLong, "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{tooLongErr},
		},
		{
			name:     "URL that cannot be parsed",
			sitemaps: []string{"https://example.com/%zz.xml", "https://example.com/sitemap.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
			wantErrs: []string{`validate "https://example.com/%zz.xml": parse "https://example.com/%zz.xml": invalid URL escape "%zz"`},
		},
		{
			name:   "follow patterns",
			follow: []string{`/sitemap-products`, `\.gz$`},
			sitemaps: []string{
				"https://example.com/sitemap-products.xml",
				"https://example.com/sitemap-blog.xml",
				"https://example.com/archive.xml.gz",
				"https://internal.example/admin.xml",
			},
			want: []string{"https://example.com/sitemap-products.xml", "https://example.com/archive.xml.gz"},
		},
		{
			name:     "follow patterns, strict mode",
			strict:   true,
			follow:   []string{`^https://example\.com/`},
			sitemaps: []string{"https://example.com/sitemap.xml", "https://internal.example/admin.xml"},
			want:     []string{"https://example.com/sitemap.xml"},
		},
		{
			name:     "follow patterns are matched against the resolved URL",
			follow:   []string{`^https://example\.com/maps/`},
			sitemaps: []string{"/maps/news.xml", "/other/news.xml"},
			want:     []string{"https://example.com/maps/news.xml"},
		},
		{
			name:     "a value that is rejected is reported whatever the follow patterns",
			follow:   []string{`\.xml$`},
			sitemaps: []string{"ftp://example.com/sitemap.xml"},
			wantErrs: []string{`validate "ftp://example.com/sitemap.xml": unsupported scheme "ftp"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var robotsTXT strings.Builder
			for _, sitemap := range tt.sitemaps {
				_, _ = fmt.Fprintf(&robotsTXT, "Sitemap: %s\n", sitemap)
			}

			s := New().SetStrict(tt.strict).SetFollow(tt.follow)
			s.parseRobotsTXT(robotsTXT.String())
			got := s.robotsTXTSitemapLocations(robotsTXTURL)

			var gotErrs []string
			for _, err := range s.GetErrors() {
				gotErrs = append(gotErrs, err.Error())
			}
			assertStringSlice(t, "locations", got, tt.want)
			assertStringSlice(t, "errors", gotErrs, tt.wantErrs)
		})
	}
}

// TestS_Parse_RobotsTXT_Follow verifies that the sitemaps a robots.txt names
// pass the checks the sitemaps of a sitemap index pass before a request is
// made for them: a Sitemap line that does not hold a URL to fetch is reported
// and not requested, and neither is a sitemap requested that the follow
// patterns leave out.
func TestS_Parse_RobotsTXT_Follow(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requested = append(requested, "http://"+r.Host+r.URL.Path)
	}

	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = fmt.Fprintf(w, `<urlset><url><loc>http://%s/secret</loc></url></urlset>`, r.Host)
	}))
	defer internal.Close()

	robotsTXT := func(host string) string {
		return fmt.Sprintf("Sitemap: http://%[1]s/sitemap-products.xml\n"+
			"Sitemap: http://%[1]s/sitemap-blog.xml\n"+
			"Sitemap: /sitemap-relative.xml\n"+
			"Sitemap: %[2]s/admin/sitemap.xml\n"+
			"Sitemap: ftp://%[1]s/sitemap.xml\n", host, internal.URL)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = fmt.Fprint(w, robotsTXT(r.Host))
			return
		}
		record(r)
		_, _ = fmt.Fprintf(w, `<urlset><url><loc>http://%s%s/page</loc></url></urlset>`, r.Host, r.URL.Path)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	products, blog, relative := server.URL+"/sitemap-products.xml", server.URL+"/sitemap-blog.xml", server.URL+"/sitemap-relative.xml"
	admin := internal.URL + "/admin/sitemap.xml"
	notHTTP := fmt.Sprintf(`validate "ftp://%s/sitemap.xml": unsupported scheme "ftp"`, host)
	notHTTPStrict := fmt.Sprintf(`validate "ftp://%s/sitemap.xml": strict mode: unsupported scheme "ftp"`, host)
	notAbsolute := `validate "/sitemap-relative.xml": strict mode: unsupported scheme ""`

	tests := []struct {
		name   string
		strict bool
		follow []string
		// wantRequested holds the sitemaps requested, wantURLs the pages found in them.
		wantRequested []string
		wantURLs      []string
		wantErrs      []string
	}{
		{
			name:          "no follow patterns",
			wantRequested: []string{products, blog, relative, admin},
			wantURLs:      []string{products + "/page", blog + "/page", relative + "/page", internal.URL + "/secret"},
			wantErrs:      []string{notHTTP},
		},
		{
			name:          "no follow patterns, strict mode",
			strict:        true,
			wantRequested: []string{products, blog, admin},
			wantURLs:      []string{products + "/page", blog + "/page", internal.URL + "/secret"},
			wantErrs:      []string{notAbsolute, notHTTPStrict},
		},
		{
			name:          "sitemaps of one host only",
			follow:        []string{"^" + regexp.QuoteMeta(server.URL) + "/"},
			wantRequested: []string{products, blog, relative},
			wantURLs:      []string{products + "/page", blog + "/page", relative + "/page"},
			wantErrs:      []string{notHTTP},
		},
		{
			name:          "one sitemap only",
			follow:        []string{`/sitemap-products\.xml$`},
			wantRequested: []string{products},
			wantURLs:      []string{products + "/page"},
			wantErrs:      []string{notHTTP},
		},
		{
			name:          "one sitemap only, strict mode",
			strict:        true,
			follow:        []string{`/sitemap-products\.xml$`},
			wantRequested: []string{products},
			wantURLs:      []string{products + "/page"},
			wantErrs:      []string{notAbsolute, notHTTPStrict},
		},
		{
			name:     "no sitemap matches",
			follow:   []string{`/sitemap-none\.xml$`},
			wantErrs: []string{notHTTP},
		},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			for _, passContent := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, multiThread=%v, passContent=%v", tt.name, multiThread, passContent), func(t *testing.T) {
					mu.Lock()
					requested = nil
					mu.Unlock()

					var content *string
					if passContent {
						content = pointerOfString(robotsTXT(host))
					}
					s := New().SetStrict(tt.strict).SetFollow(tt.follow).SetMultiThread(multiThread)
					requireParse(t, s, server.URL+"/robots.txt", content)

					var gotURLs, gotErrs []string
					for _, u := range s.GetURLs() {
						gotURLs = append(gotURLs, u.Loc)
					}
					for _, err := range s.GetErrors() {
						gotErrs = append(gotErrs, err.Error())
						var validationErr *ValidationError
						if !errors.As(err, &validationErr) {
							t.Errorf("error %q: got %T, want *ValidationError", err, err)
						}
					}
					mu.Lock()
					gotRequested := sortedCopy(requested)
					mu.Unlock()
					assertStringSlice(t, "sitemaps requested", gotRequested, sortedCopy(tt.wantRequested))
					assertStringSlice(t, "URLs", sortedCopy(gotURLs), sortedCopy(tt.wantURLs))
					assertStringSlice(t, "errors", sortedCopy(gotErrs), sortedCopy(tt.wantErrs))
				})
			}
		}
	}
}

// sitemapIndexServer starts a server whose /index.xml lists the given number of
// sitemaps, /sitemap-0.xml and so on, each of which lists the given number of
// pages: /sitemap-0/0 and so on. onSitemap, unless it is nil, is called for
// every sitemap requested before the request is answered.
func sitemapIndexServer(tb testing.TB, sitemaps, pages int, onSitemap func()) *httptest.Server {
	tb.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		if r.URL.Path == "/index.xml" {
			b.WriteString(`<sitemapindex>`)
			for i := 0; i < sitemaps; i++ {
				_, _ = fmt.Fprintf(&b, `<sitemap><loc>%s/sitemap-%d.xml</loc></sitemap>`, server.URL, i)
			}
			b.WriteString(`</sitemapindex>`)
		} else {
			if onSitemap != nil {
				onSitemap()
			}
			b.WriteString(`<urlset>`)
			for i := 0; i < pages; i++ {
				_, _ = fmt.Fprintf(&b, `<url><loc>%s%s/%d</loc></url>`, server.URL, strings.TrimSuffix(r.URL.Path, ".xml"), i)
			}
			b.WriteString(`</urlset>`)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	tb.Cleanup(server.Close)

	return server
}

// heldShare runs fn, and keeps trying the lock of s until fn returns. It
// returns the share of the attempts that found the lock taken.
func heldShare(s *S, fn func()) float64 {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	var attempts, held float64
	for {
		attempts++
		if s.mu.TryLock() {
			s.mu.Unlock()
		} else {
			held++
		}
		select {
		case <-done:
			return held / attempts
		default:
			runtime.Gosched()
		}
	}
}

// TestS_Parse_LockFreeWhileParsing verifies that the lock guarding the results
// is not held while a document is decoded. Decoding is what a Parse call
// spends its time on: with the lock held for it the getters would wait for as
// long as a document takes, and the sitemaps fetched concurrently would still
// be decoded one after the other.
func TestS_Parse_LockFreeWhileParsing(t *testing.T) {
	// Enough pages for decoding them to take up nearly all of the call.
	const pages = 3000

	server := sitemapIndexServer(t, 1, pages, nil)

	var document strings.Builder
	document.WriteString(`<urlset>`)
	for i := 0; i < pages; i++ {
		_, _ = fmt.Fprintf(&document, `<url><loc>https://example.com/%d</loc></url>`, i)
	}
	document.WriteString(`</urlset>`)

	tests := []struct {
		name        string
		url         string
		content     *string
		multiThread bool
	}{
		{"document passed to Parse", "https://example.com/sitemap.xml", pointerOfString(document.String()), true},
		{"sitemap of an index, sequential", server.URL + "/index.xml", nil, false},
		{"sitemap of an index, multi-thread", server.URL + "/index.xml", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New().SetMultiThread(tt.multiThread)

			var err error
			share := heldShare(s, func() {
				_, err = s.Parse(tt.url, tt.content)
			})

			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			assertCounts(t, s, pages, 0)
			// The lock is taken for a moment only, to add to the results what a document
			// yielded. Held for the decoding, it is found taken nearly every time.
			if share > 0.5 {
				t.Errorf("the lock was held %.0f%% of the time a document was parsed", share*100)
			}
		})
	}
}

// TestS_Parse_MaxConcurrency_CoversParsing verifies that a sitemap takes up
// one of the slots set with SetMaxConcurrency until it is parsed, not only
// while it is fetched: with a single slot, a sitemap is requested only when
// everything the sitemaps requested before it list has been collected.
func TestS_Parse_MaxConcurrency_CoversParsing(t *testing.T) {
	const sitemaps, pages = 6, 500

	s := New().SetMaxConcurrency(1)

	var mu sync.Mutex
	var collected []int64
	server := sitemapIndexServer(t, sitemaps, pages, func() {
		mu.Lock()
		defer mu.Unlock()
		collected = append(collected, s.GetURLCount())
	})

	requireParse(t, s, server.URL+"/index.xml", nil)

	assertCounts(t, s, sitemaps*pages, 0)
	mustEqual(t, "sitemaps requested", len(collected), sitemaps)
	for i, got := range collected {
		mustEqual(t, fmt.Sprintf("URLs collected when sitemap request %d arrived", i+1), got, int64(i*pages))
	}
}

// TestS_Parse_MultiThread_DocumentsStayTogether verifies that the URLs of a
// sitemap are added to the results together and in the order the sitemap lists
// them, however the sitemaps parsed concurrently finish.
func TestS_Parse_MultiThread_DocumentsStayTogether(t *testing.T) {
	const sitemaps, pages = 8, 250

	server := sitemapIndexServer(t, sitemaps, pages, nil)

	s := New().SetMaxConcurrency(0)
	requireParse(t, s, server.URL+"/index.xml", nil)

	assertCounts(t, s, sitemaps*pages, 0)
	urls := s.GetURLs()
	seen := make(map[string]struct{})
	for block := 0; block < sitemaps; block++ {
		// The first URL of a block tells which sitemap the block has to be of.
		sitemapURL := strings.TrimSuffix(urls[block*pages].Loc, "/0")
		seen[sitemapURL] = struct{}{}
		for i := 0; i < pages; i++ {
			mustEqual(t, fmt.Sprintf("URL %d of block %d", i, block), urls[block*pages+i].Loc, fmt.Sprintf("%s/%d", sitemapURL, i))
		}
	}
	mustEqual(t, "sitemaps the blocks are of", len(seen), sitemaps)
}

// TestS_Parse_MaxConcurrency_BoundsGoroutines verifies that a sitemap waiting
// for one of the slots set with SetMaxConcurrency does not take up a goroutine
// while it waits: however many sitemaps an index lists, the goroutines there
// are at any time are those of the few sitemaps being worked on.
func TestS_Parse_MaxConcurrency_BoundsGoroutines(t *testing.T) {
	const sitemaps = 400

	var mu sync.Mutex
	peak := 0
	server := sitemapIndexServer(t, sitemaps, 1, func() {
		goroutines := runtime.NumGoroutine()
		mu.Lock()
		defer mu.Unlock()
		peak = max(peak, goroutines)
	})

	s := New().SetMaxConcurrency(2)
	before := runtime.NumGoroutine()
	requireParse(t, s, server.URL+"/index.xml", nil)

	assertCounts(t, s, sitemaps, 0)
	// A sitemap being worked on has a goroutine of the parser, and a handful of the HTTP
	// client and the server. A goroutine for every sitemap listed would be hundreds of them.
	if started := peak - before; started > sitemaps/4 {
		t.Errorf("%d goroutines were started for %d sitemaps fetched 2 at a time", started, sitemaps)
	}
}

// TestS_ParseContext_CancelledWhileWaitingForSlot verifies that a call that is
// cancelled while sitemaps wait for a slot does not go on to fetch them, and
// records nothing for the sitemaps that were still to come.
func TestS_ParseContext_CancelledWhileWaitingForSlot(t *testing.T) {
	const sitemaps = 50

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requested atomic.Int64
	server := sitemapIndexServer(t, sitemaps, 1, func() {
		requested.Add(1)
		// The sitemap requested first has the only slot. The call is cancelled while it
		// holds it, with all the other sitemaps yet to be fetched.
		cancel()
	})
	url := server.URL + "/index.xml"

	s := New().SetMaxConcurrency(1)
	_, err := s.ParseContext(ctx, url, nil)

	mustEqual(t, "sitemaps requested", requested.Load(), 1)
	// The request that was cancelled is reported, and so may be the one of a sitemap that
	// got the slot it gave up just as the wait for it was cancelled.
	if cutShort := requireCutShort(t, s, err, url, context.Canceled); cutShort > 2 {
		t.Errorf("expected the errors of 2 requests at most, got %d: %v", cutShort, s.GetErrors())
	}
}

// requireCutShort verifies what a call that was cut short reports. err, the
// error the call returned, has to be a *ParseError that names url, the URL the
// call was made for, and holds cause, the error of the context. The call has
// to have recorded that very error, once, and nothing else but the
// *NetworkError of every request that was cut short. requireCutShort returns
// the number of those.
func requireCutShort(t *testing.T, s *S, err error, url string, cause error) int {
	t.Helper()

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected a *ParseError to be returned, got %T: %v", err, err)
	}
	mustEqual(t, "URL of the error returned", parseErr.URL, url)
	if parseErr.Err != cause {
		t.Errorf("expected the error returned to hold %q, got %T: %v", cause, parseErr.Err, parseErr.Err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected the error returned to match %q, got %v", cause, err)
	}

	errs := s.GetErrors()
	if len(errs) == 0 || errs[len(errs)-1] != err {
		t.Fatalf("expected the error returned to be the last one recorded, got %v", errs)
	}
	for _, recorded := range errs[:len(errs)-1] {
		var networkErr *NetworkError
		if !errors.As(recorded, &networkErr) {
			t.Errorf("expected the *NetworkError of a request, got %T: %v", recorded, recorded)
			continue
		}
		if !errors.Is(recorded, cause) {
			t.Errorf("expected the error of the request to match %q, got %v", cause, recorded)
		}
	}
	mustEqual(t, "GetErrorsCount", s.GetErrorsCount(), int64(len(errs)))

	return len(errs) - 1
}

// TestS_ParseContext_CutShort verifies that a call that is cut short reports
// it the same way whether the sitemaps are fetched concurrently or one at a
// time, and whether it was cancelled or ran out of time: with one error for
// the call, and none for the sitemaps it did not get to.
func TestS_ParseContext_CutShort(t *testing.T) {
	const sitemaps, limit = 50, 4

	for _, multiThread := range []bool{false, true} {
		// A request that is under way is cut short and reported: one of them when the
		// sitemaps are fetched one at a time, as many as there are slots otherwise, and
		// one more if a sitemap got a slot just as the wait for it was cut short.
		maxCutShort, maxRequested := 1, int64(1)
		if multiThread {
			maxCutShort, maxRequested = limit+1, limit
		}

		t.Run(fmt.Sprintf("cancelled, multiThread=%v", multiThread), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var requested atomic.Int64
			server := sitemapIndexServer(t, sitemaps, 1, func() {
				requested.Add(1)
				cancel()
			})
			url := server.URL + "/index.xml"

			s := New().SetMultiThread(multiThread).SetMaxConcurrency(limit)
			_, err := s.ParseContext(ctx, url, nil)

			if cutShort := requireCutShort(t, s, err, url, context.Canceled); cutShort > maxCutShort {
				t.Errorf("expected the errors of %d requests at most, got %d: %v", maxCutShort, cutShort, s.GetErrors())
			}
			if got := requested.Load(); got < 1 || got > maxRequested {
				t.Errorf("expected 1 to %d sitemaps to be requested, got %d", maxRequested, got)
			}
		})

		t.Run(fmt.Sprintf("out of time, multiThread=%v", multiThread), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// No sitemap is served for as long as the call runs.
			served := make(chan struct{})
			defer close(served)
			var requested atomic.Int64
			server := sitemapIndexServer(t, sitemaps, 1, func() {
				requested.Add(1)
				<-served
			})
			url := server.URL + "/index.xml"

			s := New().SetMultiThread(multiThread).SetMaxConcurrency(limit).SetFetchTimeout(30)
			_, err := s.ParseContext(ctx, url, nil)

			cutShort := requireCutShort(t, s, err, url, context.DeadlineExceeded)
			if cutShort < 1 || cutShort > maxCutShort {
				t.Errorf("expected the errors of 1 to %d requests, got %d: %v", maxCutShort, cutShort, s.GetErrors())
			}
			if got := requested.Load(); got < 1 || got > maxRequested {
				t.Errorf("expected 1 to %d sitemaps to be requested, got %d", maxRequested, got)
			}
			mustEqual(t, "GetURLCount", s.GetURLCount(), 0)
		})

		t.Run(fmt.Sprintf("cancelled beforehand, multiThread=%v", multiThread), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			var requested atomic.Int64
			server := sitemapIndexServer(t, sitemaps, 1, func() { requested.Add(1) })

			documents := map[string]string{
				"/index.xml":  fmt.Sprintf(`<sitemapindex><sitemap><loc>%[1]s/sitemap-0.xml</loc></sitemap><sitemap><loc>%[1]s/sitemap-1.xml</loc></sitemap></sitemapindex>`, server.URL),
				"/robots.txt": fmt.Sprintf("Sitemap: %[1]s/sitemap-0.xml\nSitemap: %[1]s/sitemap-1.xml\n", server.URL),
				// The document lists no sitemap. It is parsed, and what it lists is kept, yet
				// the call was not to be carried out any more.
				"/sitemap.xml": fmt.Sprintf(`<urlset><url><loc>%s/page</loc></url></urlset>`, server.URL),
			}
			for path, content := range documents {
				url := server.URL + path

				s := New().SetMultiThread(multiThread).SetMaxConcurrency(limit)
				_, err := s.ParseContext(ctx, url, &content)

				if cutShort := requireCutShort(t, s, err, url, context.Canceled); cutShort != 0 {
					t.Errorf("%s: expected no other error, got %v", path, s.GetErrors())
				}
				wantURLs := int64(0)
				if path == "/sitemap.xml" {
					wantURLs = 1
				}
				mustEqual(t, path+": GetURLCount", s.GetURLCount(), wantURLs)
			}
			mustEqual(t, "sitemaps requested", requested.Load(), 0)
		})

		// When it is the request for the URL the call was made for that is cut short, there
		// is nothing to tell apart: the error of the request is the error of the call.
		t.Run(fmt.Sprintf("cancelled beforehand, document to be fetched, multiThread=%v", multiThread), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			server := sitemapIndexServer(t, sitemaps, 1, nil)
			url := server.URL + "/index.xml"

			s := New().SetMultiThread(multiThread)
			_, err := s.ParseContext(ctx, url, nil)

			var networkErr *NetworkError
			if !errors.As(err, &networkErr) {
				t.Fatalf("expected a *NetworkError to be returned, got %T: %v", err, err)
			}
			mustEqual(t, "URL of the error returned", networkErr.URL, url)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected the error returned to match %q, got %v", context.Canceled, err)
			}
			if errs := s.GetErrors(); len(errs) != 1 || errs[0] != err {
				t.Errorf("expected the error returned to be the only one recorded, got %v", errs)
			}
		})
	}
}

// TestS_ParseContext_CutShort_Redirect verifies that the error of a call that
// was cut short names the URL the call was made for, also when the document
// was served from another one.
func TestS_ParseContext_CutShort_Redirect(t *testing.T) {
	for _, multiThread := range []bool{false, true} {
		t.Run(fmt.Sprintf("multiThread=%v", multiThread), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			movedURL, _ := movedAndTarget(t, func(_, targetURL string) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/index.xml" {
						_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/sitemap.xml</loc></sitemap></sitemapindex>`, targetURL)
						return
					}
					cancel()
					_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s/page</loc></url></urlset>`, targetURL)
				}
			})
			url := movedURL + "/index.xml"

			s := New().SetMultiThread(multiThread)
			_, err := s.ParseContext(ctx, url, nil)

			requireCutShort(t, s, err, url, context.Canceled)
		})
	}
}

// unparsableDocuments are documents that cannot be parsed, by the path they
// are served at, and unparsableErrors the error each of them is reported with.
// Each one is reported once: gzip content that cannot be unzipped is not
// parsed, so it is not reported for its format in addition.
var (
	unparsableDocuments = map[string]string{
		"/page.html":            `<html><body>Not a sitemap</body></html>`,
		"/empty.xml":            ``,
		"/truncated.xml":        `<urlset><url><loc>https://example.com/page</loc></url>`,
		"/truncated-index.xml":  `<sitemapindex><sitemap><loc>https://example.com/sitemap.xml</loc>`,
		"/truncated-rss.xml":    `<rss><channel><item><link>https://example.com/page</link>`,
		"/truncated-atom.xml":   `<feed><entry><link href="https://example.com/page"/>`,
		"/corrupt.xml.gz":       "\x1f\x8b\x08 not gzip",
		"/text-without-url.txt": "# Nothing to see here\nexample.com/page\n",
	}
	unparsableErrors = map[string][]string{
		"/page.html":            {`unrecognized sitemap format (root element: "html")`},
		"/empty.xml":            {`sitemap content is empty`},
		"/truncated.xml":        {`XML syntax error on line 1: unexpected EOF`},
		"/truncated-index.xml":  {`XML syntax error on line 1: unexpected EOF`},
		"/truncated-rss.xml":    {`XML syntax error on line 1: unexpected EOF`},
		"/truncated-atom.xml":   {`XML syntax error on line 1: unexpected EOF`},
		"/corrupt.xml.gz":       {`gzip decompression failed: unexpected EOF`},
		"/text-without-url.txt": {`unrecognized sitemap format (root element: "")`},
	}
)

// unparsableServer starts a server that serves unparsableDocuments, and next
// to them:
//
//   - /pages.xml, a sitemap that lists one page;
//   - /index.xml and /robots.txt, each of which lists every document that
//     cannot be parsed, /pages.xml, and /missing.xml, which is not found;
//   - /no-loc.xml, a sitemap of two entries, one of which is not valid.
func unparsableServer(t *testing.T) *httptest.Server {
	t.Helper()

	listed := []string{"/pages.xml", "/missing.xml"}
	for path := range unparsableDocuments {
		listed = append(listed, path)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if content, ok := unparsableDocuments[r.URL.Path]; ok {
			_, _ = w.Write([]byte(content))
			return
		}
		switch r.URL.Path {
		case "/pages.xml":
			_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s/page</loc></url></urlset>`, server.URL)
		case "/no-loc.xml":
			_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s/page</loc></url><url><lastmod>2024-01-15</lastmod></url></urlset>`, server.URL)
		case "/index.xml":
			_, _ = fmt.Fprint(w, `<sitemapindex>`)
			for _, path := range listed {
				_, _ = fmt.Fprintf(w, `<sitemap><loc>%s%s</loc></sitemap>`, server.URL, path)
			}
			_, _ = fmt.Fprint(w, `</sitemapindex>`)
		case "/robots.txt":
			for _, path := range listed {
				_, _ = fmt.Fprintf(w, "Sitemap: %s%s\n", server.URL, path)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// TestS_Parse_DocumentError verifies that a call fails if the document it is
// made for cannot be parsed, whether the document is fetched or passed in, and
// that the error it returns is the one it records about the document.
func TestS_Parse_DocumentError(t *testing.T) {
	server := unparsableServer(t)

	for path, content := range unparsableDocuments {
		url := server.URL + path

		var wantErrs []string
		for _, message := range unparsableErrors[path] {
			wantErrs = append(wantErrs, fmt.Sprintf("parse %q: %s", url, message))
		}

		for _, multiThread := range []bool{false, true} {
			for name, urlContent := range map[string]*string{"fetched": nil, "passed in": &content} {
				t.Run(fmt.Sprintf("%s, %s, multiThread=%v", path, name, multiThread), func(t *testing.T) {
					s := New().SetMultiThread(multiThread)
					_, err := s.Parse(url, urlContent)

					var parseErr *ParseError
					if !errors.As(err, &parseErr) {
						t.Fatalf("expected a *ParseError to be returned, got %T: %v", err, err)
					}
					mustEqual(t, "URL of the error returned", parseErr.URL, url)

					errs := s.GetErrors()
					var gotErrs []string
					for _, recorded := range errs {
						gotErrs = append(gotErrs, recorded.Error())
					}
					assertStringSlice(t, "errors", gotErrs, wantErrs)
					// Not an error that reads the same, but the one that is recorded: of
					// several, the one that tells why the document could not be parsed.
					if len(errs) == 0 || errs[0] != err {
						t.Errorf("expected the error returned to be the first one recorded, got %v", err)
					}
				})
			}
		}
	}
}

// TestS_Parse_DocumentError_ListedSitemap verifies that it is the document a
// call is made for alone that fails the call. A sitemap it lists that cannot
// be parsed, or cannot be fetched, is reported in the error list, and so is an
// entry that is not valid; the call succeeds.
func TestS_Parse_DocumentError_ListedSitemap(t *testing.T) {
	server := unparsableServer(t)

	var wantListed []string
	for path, messages := range unparsableErrors {
		for _, message := range messages {
			wantListed = append(wantListed, fmt.Sprintf("parse %q: %s", server.URL+path, message))
		}
	}
	wantListed = append(wantListed, fmt.Sprintf("fetch %q: received HTTP status 404", server.URL+"/missing.xml"))

	tests := []struct {
		path     string
		wantErrs []string
	}{
		{"/index.xml", wantListed},
		{"/robots.txt", wantListed},
		{"/no-loc.xml", []string{fmt.Sprintf("validate %q: <loc> of an entry is empty or missing", server.URL+"/no-loc.xml")}},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, multiThread=%v", tt.path, multiThread), func(t *testing.T) {
				s := New().SetMultiThread(multiThread)
				requireParse(t, s, server.URL+tt.path, nil)

				var gotErrs []string
				for _, err := range s.GetErrors() {
					gotErrs = append(gotErrs, err.Error())
				}
				assertStringSlice(t, "errors", sortedCopy(gotErrs), sortedCopy(tt.wantErrs))

				var gotURLs []string
				for _, u := range s.GetURLs() {
					gotURLs = append(gotURLs, u.Loc)
				}
				assertStringSlice(t, "URLs", gotURLs, []string{server.URL + "/page"})
			})
		}
	}
}

// TestS_Parse_MaxDepth_NamesDocument verifies that the error of the depth
// limit names the sitemap index whose sitemaps are not followed, by the URL it
// was served from, and that there is one for every such sitemap index.
func TestS_Parse_MaxDepth_NamesDocument(t *testing.T) {
	// index.xml -> pages.xml (one URL)
	//           -> nested.xml, which has moved -> deep.xml (one URL)
	//           -> other.xml -> deeper.xml -> deepest.xml (one URL)
	movedURL, targetURL := movedAndTarget(t, func(movedURL, targetURL string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/index.xml":
				_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%[1]s/pages.xml</loc></sitemap><sitemap><loc>%[2]s/nested.xml</loc></sitemap><sitemap><loc>%[1]s/other.xml</loc></sitemap></sitemapindex>`, targetURL, movedURL)
			case "/nested.xml":
				_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/deep.xml</loc></sitemap></sitemapindex>`, targetURL)
			case "/other.xml":
				_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/deeper.xml</loc></sitemap></sitemapindex>`, targetURL)
			case "/deeper.xml":
				_, _ = fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/deepest.xml</loc></sitemap></sitemapindex>`, targetURL)
			default:
				_, _ = fmt.Fprintf(w, `<urlset><url><loc>%s%s/page</loc></url></urlset>`, targetURL, r.URL.Path)
			}
		}
	})

	tests := []struct {
		maxDepth    int
		wantURLs    int64
		notFollowed []string
	}{
		{1, 1, []string{targetURL + "/nested.xml", targetURL + "/other.xml"}},
		{2, 2, []string{targetURL + "/deeper.xml"}},
		{3, 3, nil},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("maxDepth=%d, multiThread=%v", tt.maxDepth, multiThread), func(t *testing.T) {
				s := New().SetMaxDepth(tt.maxDepth).SetMultiThread(multiThread)
				// Reaching the limit does not fail the call.
				requireParse(t, s, movedURL+"/index.xml", nil)

				mustEqual(t, "GetURLCount", s.GetURLCount(), tt.wantURLs)
				requireDepthLimitErrors(t, s.GetErrors(), tt.maxDepth, tt.notFollowed...)
			})
		}
	}
}

// heapInUse returns the number of bytes on the heap that are still in use
// after a garbage collection.
func heapInUse() int64 {
	// Twice: what a sync.Pool holds is only given up by the second collection.
	runtime.GC()
	runtime.GC()

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return int64(stats.HeapAlloc)
}

// TestS_Parse_ContentNotKept verifies that the content of a document is given
// up once the document is parsed: neither the instance holds on to it, nor do
// the URLs and the errors collected from it. The documents are almost nothing
// but padding, so an instance that keeps much after a call keeps the content.
func TestS_Parse_ContentNotKept(t *testing.T) {
	const padding = 1 << 20

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A document is put together for every request, so that it is the parser alone
		// that can keep it.
		comments := strings.Repeat("#"+strings.Repeat("x", 1022)+"\n", padding/1024)
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = fmt.Fprintf(w, "%sSitemap: %s/sitemap.xml\n%s", comments, server.URL, comments)
		case "/sitemap.txt":
			// The line that is not a valid URL is kept in the error about it.
			_, _ = fmt.Fprintf(w, "%s%s/page-1\n%s%s/page-2\nhttp://[::1/page-3\n", comments, server.URL, comments, server.URL)
		default:
			_, _ = fmt.Fprintf(w, "<urlset><url><loc>%s/page-1</loc></url><!-- %s --><url><loc>%s/page-2</loc></url></urlset>", server.URL, strings.Repeat("x", padding), server.URL)
		}
	}))
	defer server.Close()

	tests := []struct {
		name     string
		path     string
		strict   bool
		wantErrs int64
	}{
		{"urlset", "/sitemap.xml", false, 0},
		{"text sitemap", "/sitemap.txt", false, 1},
		// Strict mode takes a URL as it stands in the document.
		{"text sitemap, strict", "/sitemap.txt", true, 1},
		{"robots.txt", "/robots.txt", false, 0},
		{"robots.txt, strict", "/robots.txt", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New().SetStrict(tt.strict)

			before := heapInUse()
			requireParse(t, s, server.URL+tt.path, nil)
			kept := heapInUse() - before

			assertCounts(t, s, 2, tt.wantErrs)
			if kept > padding/4 {
				t.Errorf("%d bytes are kept after parsing a document of some %d bytes", kept, padding)
			}
			// The instance is alive up to here, with everything it holds.
			runtime.KeepAlive(s)
		})
	}
}

// siteServer is a test server for a site whose robots.txt files and sitemap
// indexes list the sitemaps given in lists, by path: a path that ends with
// "/robots.txt" is served as a robots.txt, any other path in lists as a
// sitemap index. Every other path is a sitemap of as many pages as pages
// gives for it, 2 if it gives none, except for the ones that start with
// "/missing", which are not found.
type siteServer struct {
	*httptest.Server
	lists map[string][]string
	pages map[string]int
	// onRequest, unless it is nil, is called for every request before it is
	// answered.
	onRequest func(path string)

	mu        sync.Mutex
	requested []string
}

func newSiteServer(t *testing.T, lists map[string][]string, pages map[string]int) *siteServer {
	t.Helper()

	site := &siteServer{lists: lists, pages: pages}
	site.Server = httptest.NewServer(http.HandlerFunc(site.serve))
	t.Cleanup(site.Close)

	return site
}

func (site *siteServer) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	site.mu.Lock()
	site.requested = append(site.requested, path)
	site.mu.Unlock()
	if site.onRequest != nil {
		site.onRequest(path)
	}

	listed, lists := site.lists[path]
	switch {
	case lists && strings.HasSuffix(path, "/robots.txt"):
		for _, sitemap := range listed {
			_, _ = fmt.Fprintf(w, "Sitemap: %s%s\n", site.URL, sitemap)
		}
	case lists:
		_, _ = fmt.Fprint(w, `<sitemapindex>`)
		for _, sitemap := range listed {
			_, _ = fmt.Fprintf(w, `<sitemap><loc>%s%s</loc></sitemap>`, site.URL, sitemap)
		}
		_, _ = fmt.Fprint(w, `</sitemapindex>`)
	case strings.HasPrefix(path, "/missing"):
		http.NotFound(w, r)
	default:
		_, _ = fmt.Fprint(w, `<urlset>`)
		for _, page := range site.pagesOf(path) {
			_, _ = fmt.Fprintf(w, `<url><loc>%s</loc></url>`, page)
		}
		_, _ = fmt.Fprint(w, `</urlset>`)
	}
}

// pagesOf returns the pages the sitemap at path lists.
func (site *siteServer) pagesOf(path string) []string {
	count, ok := site.pages[path]
	if !ok {
		count = 2
	}

	var pages []string
	for i := 1; i <= count; i++ {
		pages = append(pages, fmt.Sprintf("%s%s/%d", site.URL, path, i))
	}
	return pages
}

// pagesOfAll returns the pages the sitemaps at paths list, in that order.
func (site *siteServer) pagesOfAll(paths ...string) []string {
	var pages []string
	for _, path := range paths {
		pages = append(pages, site.pagesOf(path)...)
	}
	return pages
}

// fetched returns the paths requested since the last call, in the order they
// were requested.
func (site *siteServer) fetched() []string {
	site.mu.Lock()
	defer site.mu.Unlock()

	requested := site.requested
	site.requested = nil
	return requested
}

// locsOf returns the locations of the URLs s has collected, in a slice that is
// never nil.
func locsOf(s *S) []string {
	locs := []string{}
	for _, u := range s.GetURLs() {
		locs = append(locs, u.Loc)
	}
	return locs
}

// errorsOf returns the texts of the errors s has recorded.
func errorsOf(s *S) []string {
	texts := []string{}
	for _, err := range s.GetErrors() {
		texts = append(texts, err.Error())
	}
	return texts
}

// limitErrors returns the errors among those of s that tell a limit of the
// call for url was reached, by what they say, and the other errors.
func limitErrors(t *testing.T, s *S, url string) (limits []string, others []string) {
	t.Helper()

	limits, others = []string{}, []string{}
	for _, err := range s.GetErrors() {
		var parseErr *ParseError
		if errors.As(err, &parseErr) && strings.HasPrefix(parseErr.Err.Error(), "limit of ") {
			mustEqual(t, "URL of the limit error", parseErr.URL, url)
			limits = append(limits, parseErr.Err.Error())
			continue
		}
		others = append(others, err.Error())
	}
	return limits, others
}

// limitTestLists is the site most tests of the limits of a call parse:
//
//	/robots.txt -> /index-a.xml -> /a1.xml, /a2.xml, /a3.xml
//	            -> /index-b.xml -> /b1.xml
//	                            -> /index-c.xml -> /c1.xml, /c2.xml
//	                            -> /b2.xml
//
// which makes 10 sitemaps of 14 pages. /repeated/robots.txt lists two of the
// sitemaps more than once, /missing/robots.txt two that are not found.
var limitTestLists = map[string][]string{
	"/robots.txt":          {"/index-a.xml", "/index-b.xml"},
	"/index-a.xml":         {"/a1.xml", "/a2.xml", "/a3.xml"},
	"/index-b.xml":         {"/b1.xml", "/index-c.xml", "/b2.xml"},
	"/index-c.xml":         {"/c1.xml", "/c2.xml"},
	"/repeated/robots.txt": {"/a1.xml", "/a1.xml", "/a2.xml", "/a1.xml", "/a2.xml"},
	"/missing/robots.txt":  {"/missing-1.xml", "/missing-2.xml", "/a1.xml"},
}

// TestS_Parse_MaxSitemaps verifies that a call fetches no more sitemaps than
// the limit set with SetMaxSitemaps allows, on all levels together, and that
// it reports the limit once if a sitemap had to be left out for it.
func TestS_Parse_MaxSitemaps(t *testing.T) {
	site := newSiteServer(t, limitTestLists, nil)
	// The order in which the sitemaps are fetched one at a time.
	all := []string{"/index-a.xml", "/a1.xml", "/a2.xml", "/a3.xml", "/index-b.xml", "/b1.xml", "/index-c.xml", "/c1.xml", "/c2.xml", "/b2.xml"}
	missing := func(path string) string {
		return fmt.Sprintf("fetch %q: received HTTP status 404", site.URL+path)
	}

	tests := []struct {
		name  string
		path  string
		limit int
		// fetched are the sitemaps that are fetched when they are fetched one at a time.
		fetched []string
		leftOut bool
		others  []string
	}{
		{name: "no limit", path: "/robots.txt", limit: 0, fetched: all},
		{name: "more than there are", path: "/robots.txt", limit: 11, fetched: all},
		{name: "as many as there are", path: "/robots.txt", limit: 10, fetched: all},
		{name: "one less than there are", path: "/robots.txt", limit: 9, fetched: all[:9], leftOut: true},
		{name: "sitemaps of several levels", path: "/robots.txt", limit: 6, fetched: all[:6], leftOut: true},
		{name: "part of a sitemap index", path: "/robots.txt", limit: 3, fetched: all[:3], leftOut: true},
		{name: "one", path: "/robots.txt", limit: 1, fetched: all[:1], leftOut: true},
		{name: "sitemap index, part of it", path: "/index-a.xml", limit: 2, fetched: []string{"/a1.xml", "/a2.xml"}, leftOut: true},
		{name: "sitemap index, all of it", path: "/index-a.xml", limit: 3, fetched: []string{"/a1.xml", "/a2.xml", "/a3.xml"}},
		{name: "the document of the call does not count", path: "/a1.xml", limit: 1, fetched: []string{}},
		{name: "a sitemap listed again does not count", path: "/repeated/robots.txt", limit: 2, fetched: []string{"/a1.xml", "/a2.xml"}},
		{name: "a sitemap listed again is not left out", path: "/repeated/robots.txt", limit: 1, fetched: []string{"/a1.xml"}, leftOut: true},
		{
			name: "a sitemap that is not found counts", path: "/missing/robots.txt", limit: 2,
			fetched: []string{"/missing-1.xml", "/missing-2.xml"}, leftOut: true,
			others: []string{missing("/missing-1.xml"), missing("/missing-2.xml")},
		},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, multiThread=%v", tt.name, multiThread), func(t *testing.T) {
				url := site.URL + tt.path
				s := New().SetMaxSitemaps(tt.limit).SetMultiThread(multiThread)
				// The limit is one of the call, so the second call has to do what the first did.
				for call := 1; call <= 2; call++ {
					site.fetched()
					// A limit that is reached does not fail the call.
					requireParse(t, s, url, nil)

					// The first request is the one for the document of the call.
					fetched := site.fetched()[1:]
					if multiThread {
						// Which sitemaps are fetched depends on timing, how many does not.
						mustEqual(t, "sitemaps fetched", len(fetched), len(tt.fetched))
					} else {
						assertStringSlice(t, "sitemaps fetched", fetched, tt.fetched)
						var sitemaps []string
						for _, path := range tt.fetched {
							if _, lists := site.lists[path]; !lists && !strings.HasPrefix(path, "/missing") {
								sitemaps = append(sitemaps, path)
							}
						}
						if len(tt.fetched) == 0 {
							sitemaps = []string{tt.path}
						}
						assertStringSlice(t, "URLs", locsOf(s), append([]string{}, site.pagesOfAll(sitemaps...)...))
					}
					for i, path := range fetched {
						for _, other := range fetched[:i] {
							if path == other {
								t.Errorf("%s was fetched more than once: %v", path, fetched)
							}
						}
					}

					limits, others := limitErrors(t, s, url)
					wantLimits := []string{}
					if tt.leftOut {
						wantLimits = append(wantLimits, fmt.Sprintf("limit of %d sitemaps reached", tt.limit))
					}
					assertStringSlice(t, "limit errors", limits, wantLimits)
					assertStringSlice(t, "other errors", sortedCopy(others), sortedCopy(tt.others))
				}
			})
		}
	}
}

// TestS_Parse_MaxSitemaps_PassedContent verifies that the limit applies to a
// document that is passed in like to one that is fetched.
func TestS_Parse_MaxSitemaps_PassedContent(t *testing.T) {
	site := newSiteServer(t, limitTestLists, nil)

	documents := map[string]string{
		"/robots.txt": fmt.Sprintf("Sitemap: %[1]s/a1.xml\nSitemap: %[1]s/a2.xml\nSitemap: %[1]s/a3.xml\n", site.URL),
		"/index.xml":  fmt.Sprintf(`<sitemapindex><sitemap><loc>%[1]s/a1.xml</loc></sitemap><sitemap><loc>%[1]s/a2.xml</loc></sitemap><sitemap><loc>%[1]s/a3.xml</loc></sitemap></sitemapindex>`, site.URL),
	}
	for path, content := range documents {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, multiThread=%v", path, multiThread), func(t *testing.T) {
				url := site.URL + path
				s := New().SetMaxSitemaps(2).SetMultiThread(multiThread)
				site.fetched()
				requireParse(t, s, url, &content)

				mustEqual(t, "sitemaps fetched", len(site.fetched()), 2)
				mustEqual(t, "GetURLCount", s.GetURLCount(), 4)
				limits, others := limitErrors(t, s, url)
				assertStringSlice(t, "limit errors", limits, []string{"limit of 2 sitemaps reached"})
				assertStringSlice(t, "other errors", others, []string{})
			})
		}
	}
}

// documentsListing returns documents that list locs, one in each of the
// formats that list pages, by the path they are located at.
func documentsListing(locs []string) map[string]string {
	var urlset, text, rss, atom strings.Builder
	for _, loc := range locs {
		_, _ = fmt.Fprintf(&urlset, "<url><loc>%s</loc></url>", loc)
		_, _ = fmt.Fprintf(&text, "%s\n", loc)
		_, _ = fmt.Fprintf(&rss, "<item><link>%s</link></item>", loc)
		_, _ = fmt.Fprintf(&atom, `<entry><link href="%s"/></entry>`, loc)
	}

	return map[string]string{
		"/sitemap.xml": "<urlset>" + urlset.String() + "</urlset>",
		"/sitemap.txt": text.String(),
		"/rss.xml":     "<rss><channel>" + rss.String() + "</channel></rss>",
		"/atom.xml":    "<feed>" + atom.String() + "</feed>",
	}
}

// TestS_Parse_MaxURLs_Document verifies that a call collects no more URLs
// than the limit set with SetMaxURLs allows, whatever the format of the
// document, that the ones it collects are the first ones the document lists,
// and that it reports the limit once if a URL had to be left out for it.
func TestS_Parse_MaxURLs_Document(t *testing.T) {
	var pages []string
	for i := 1; i <= 5; i++ {
		pages = append(pages, fmt.Sprintf("https://example.com/page-%d", i))
	}

	for path, content := range documentsListing(pages) {
		for _, limit := range []int{0, 1, 2, 4, 5, 6, 100} {
			for _, strict := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, limit=%d, strict=%v", path, limit, strict), func(t *testing.T) {
					url := "https://example.com" + path
					s := New().SetMaxURLs(limit).SetStrict(strict)
					requireParse(t, s, url, &content)

					want, wantLimits := pages, []string{}
					if limit > 0 && limit < len(pages) {
						want = pages[:limit]
						wantLimits = append(wantLimits, fmt.Sprintf("limit of %d URLs reached", limit))
					}
					assertStringSlice(t, "URLs", locsOf(s), want)
					mustEqual(t, "GetURLCount", s.GetURLCount(), int64(len(want)))
					limits, others := limitErrors(t, s, url)
					assertStringSlice(t, "limit errors", limits, wantLimits)
					assertStringSlice(t, "other errors", others, []string{})
				})
			}
		}
	}
}

// TestS_Parse_MaxURLs_Entries verifies what counts towards the limit set with
// SetMaxURLs, and what becomes of the entries a document lists once the limit
// is reached.
func TestS_Parse_MaxURLs_Entries(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	invalid := func(loc string) string {
		return fmt.Sprintf("validate %q: unsupported scheme \"ftp\"", loc)
	}

	// Only three of the entries yield a URL: one is not valid, and one is left out by
	// the pattern set with SetRules.
	mixed := `<urlset>` +
		`<url><loc>https://example.com/kept-1</loc></url>` +
		`<url><loc>ftp://example.com/kept-invalid</loc></url>` +
		`<url><loc>https://example.com/other</loc></url>` +
		`<url><loc>https://example.com/kept-2</loc></url>` +
		`<url><loc>https://example.com/kept-3</loc></url>` +
		`</urlset>`
	// The document ends before its root element does.
	truncated := `<urlset>` +
		`<url><loc>https://example.com/kept-1</loc></url>` +
		`<url><loc>https://example.com/kept-2</loc></url>` +
		`<url><loc>https://example.com/kept-3</loc></url>`

	tests := []struct {
		name       string
		content    string
		limit      int
		wantURLs   []string
		wantLimits []string
		wantOthers []string
		// wantErr is the error the call returns, if it returns one.
		wantErr string
	}{
		{
			name: "entries that yield no URL do not count", content: mixed, limit: 3,
			wantURLs:   []string{"https://example.com/kept-1", "https://example.com/kept-2", "https://example.com/kept-3"},
			wantLimits: []string{},
			wantOthers: []string{invalid("ftp://example.com/kept-invalid")},
		},
		{
			name: "entries that yield no URL do not count, limit reached", content: mixed, limit: 2,
			wantURLs:   []string{"https://example.com/kept-1", "https://example.com/kept-2"},
			wantLimits: []string{"limit of 2 URLs reached"},
			wantOthers: []string{invalid("ftp://example.com/kept-invalid")},
		},
		{
			// The document yields nothing, so nothing of it was left out for the limit.
			name: "document that cannot be parsed", content: truncated, limit: 2,
			wantURLs:   []string{},
			wantLimits: []string{},
			wantOthers: []string{fmt.Sprintf("parse %q: XML syntax error on line 1: unexpected EOF", url)},
			wantErr:    fmt.Sprintf("parse %q: XML syntax error on line 1: unexpected EOF", url),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New().SetMaxURLs(tt.limit).SetRules([]string{`/kept-`})
			_, err := s.Parse(url, &tt.content)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected parse error: %v", err)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("expected parse error %q, got %v", tt.wantErr, err)
			}

			assertStringSlice(t, "URLs", locsOf(s), tt.wantURLs)
			limits, others := limitErrors(t, s, url)
			assertStringSlice(t, "limit errors", limits, tt.wantLimits)
			assertStringSlice(t, "other errors", others, tt.wantOthers)
		})
	}
}

// TestS_Parse_MaxURLs_EntriesNotRead verifies that, whatever the format of the
// document, the entries that come once the limit set with SetMaxURLs is
// reached are left out unread: they yield no errors either. The limit is
// reported then, as nothing tells that the URLs collected are all there are.
func TestS_Parse_MaxURLs_EntriesNotRead(t *testing.T) {
	// The entries that are not valid come last. They are not valid in any of the formats,
	// nor in either mode: their URLs are too long.
	valid := []string{"https://example.com/page-1", "https://example.com/page-2"}
	invalid := []string{
		"https://example.com/page-3/" + strings.Repeat("a", maxLocLength),
		"https://example.com/page-4/" + strings.Repeat("a", maxLocLength),
	}
	var invalidErrs []string
	for _, loc := range invalid {
		invalidErrs = append(invalidErrs, fmt.Sprintf("validate %q: URL exceeds maximum length of %d characters (%d)", loc, maxLocLength, len(loc)))
	}

	for path, content := range documentsListing(append(append([]string{}, valid...), invalid...)) {
		for _, strict := range []bool{false, true} {
			url := "https://example.com" + path

			t.Run(fmt.Sprintf("%s, strict=%v, room left", path, strict), func(t *testing.T) {
				s := New().SetMaxURLs(3).SetStrict(strict)
				requireParse(t, s, url, &content)

				assertStringSlice(t, "URLs", locsOf(s), valid)
				limits, others := limitErrors(t, s, url)
				assertStringSlice(t, "limit errors", limits, []string{})
				assertStringSlice(t, "other errors", others, invalidErrs)
			})

			t.Run(fmt.Sprintf("%s, strict=%v, no room left", path, strict), func(t *testing.T) {
				s := New().SetMaxURLs(2).SetStrict(strict)
				requireParse(t, s, url, &content)

				assertStringSlice(t, "URLs", locsOf(s), valid)
				limits, others := limitErrors(t, s, url)
				assertStringSlice(t, "limit errors", limits, []string{"limit of 2 URLs reached"})
				assertStringSlice(t, "other errors", others, []string{})
			})
		}
	}
}

// TestS_Parse_MaxURLs_SitemapNotParsed verifies that a sitemap that cannot be
// parsed, and so yields nothing but its error, is not taken for one the limit
// set with SetMaxURLs left something out of.
func TestS_Parse_MaxURLs_SitemapNotParsed(t *testing.T) {
	// The sitemap lists more pages than the limit allows, and ends before its root
	// element does.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<urlset>`+
			`<url><loc>https://example.com/page-1</loc></url>`+
			`<url><loc>https://example.com/page-2</loc></url>`+
			`<url><loc>https://example.com/page-3</loc></url>`)
	}))
	defer server.Close()

	url := server.URL + "/index.xml"
	index := fmt.Sprintf(`<sitemapindex><sitemap><loc>%s/sitemap.xml</loc></sitemap></sitemapindex>`, server.URL)

	for _, multiThread := range []bool{false, true} {
		t.Run(fmt.Sprintf("multiThread=%v", multiThread), func(t *testing.T) {
			s := New().SetMaxURLs(2).SetMultiThread(multiThread)
			requireParse(t, s, url, &index)

			assertStringSlice(t, "URLs", locsOf(s), []string{})
			limits, others := limitErrors(t, s, url)
			assertStringSlice(t, "limit errors", limits, []string{})
			assertStringSlice(t, "other errors", others, []string{
				fmt.Sprintf("parse %q: XML syntax error on line 1: unexpected EOF", server.URL+"/sitemap.xml"),
			})
		})
	}
}

// TestS_Parse_MaxURLs_Sitemaps verifies that the limit set with SetMaxURLs is
// one of the call: of all the sitemaps together, the sitemaps that are left
// when it is reached not being fetched any more.
func TestS_Parse_MaxURLs_Sitemaps(t *testing.T) {
	site := newSiteServer(t, map[string][]string{
		"/robots.txt": {"/index.xml"},
		"/index.xml":  {"/s1.xml", "/s2.xml", "/s3.xml"},
	}, map[string]int{"/s1.xml": 4, "/s2.xml": 4, "/s3.xml": 4})
	all := site.pagesOfAll("/s1.xml", "/s2.xml", "/s3.xml")

	tests := []struct {
		name  string
		path  string
		limit int
		// fetched are the sitemaps that are fetched when they are fetched one at a time.
		fetched  []string
		wantURLs []string
		leftOut  bool
	}{
		{name: "no limit", path: "/index.xml", limit: 0, fetched: []string{"/s1.xml", "/s2.xml", "/s3.xml"}, wantURLs: all},
		{name: "more than there are", path: "/index.xml", limit: 13, fetched: []string{"/s1.xml", "/s2.xml", "/s3.xml"}, wantURLs: all},
		{name: "as many as there are", path: "/index.xml", limit: 12, fetched: []string{"/s1.xml", "/s2.xml", "/s3.xml"}, wantURLs: all},
		{name: "one less than there are", path: "/index.xml", limit: 11, fetched: []string{"/s1.xml", "/s2.xml", "/s3.xml"}, wantURLs: all[:11], leftOut: true},
		{name: "reached within a sitemap", path: "/index.xml", limit: 6, fetched: []string{"/s1.xml", "/s2.xml"}, wantURLs: all[:6], leftOut: true},
		// Nothing of the second sitemap is left out, but the third one is not fetched.
		{name: "reached at the end of a sitemap", path: "/index.xml", limit: 8, fetched: []string{"/s1.xml", "/s2.xml"}, wantURLs: all[:8], leftOut: true},
		{name: "one", path: "/index.xml", limit: 1, fetched: []string{"/s1.xml"}, wantURLs: all[:1], leftOut: true},
		{name: "robots.txt", path: "/robots.txt", limit: 6, fetched: []string{"/index.xml", "/s1.xml", "/s2.xml"}, wantURLs: all[:6], leftOut: true},
		{name: "sitemap, all of it", path: "/s1.xml", limit: 4, fetched: []string{}, wantURLs: all[:4]},
		{name: "sitemap, part of it", path: "/s1.xml", limit: 3, fetched: []string{}, wantURLs: all[:3], leftOut: true},
	}

	for _, tt := range tests {
		for _, multiThread := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, multiThread=%v", tt.name, multiThread), func(t *testing.T) {
				url := site.URL + tt.path
				s := New().SetMaxURLs(tt.limit).SetMultiThread(multiThread)
				for call := 1; call <= 2; call++ {
					site.fetched()
					requireParse(t, s, url, nil)

					fetched := site.fetched()[1:]
					if multiThread {
						// Which URLs are collected depends on timing, how many does not. The
						// sitemaps may all have been requested before the limit was reached.
						mustEqual(t, "GetURLCount", s.GetURLCount(), int64(len(tt.wantURLs)))
						if len(fetched) < len(tt.fetched) {
							t.Errorf("expected %d sitemaps at least to be fetched, got %v", len(tt.fetched), fetched)
						}
					} else {
						assertStringSlice(t, "sitemaps fetched", fetched, tt.fetched)
						assertStringSlice(t, "URLs", locsOf(s), tt.wantURLs)
					}

					limits, others := limitErrors(t, s, url)
					wantLimits := []string{}
					if tt.leftOut {
						wantLimits = append(wantLimits, fmt.Sprintf("limit of %d URLs reached", tt.limit))
					}
					assertStringSlice(t, "limit errors", limits, wantLimits)
					assertStringSlice(t, "other errors", others, []string{})
				}
			})
		}
	}
}

// TestS_Parse_MaxSitemaps_MaxURLs verifies that the two limits of a call are
// reported side by side when a sitemap had to be left out for the one and a
// URL for the other.
func TestS_Parse_MaxSitemaps_MaxURLs(t *testing.T) {
	site := newSiteServer(t, map[string][]string{"/index.xml": {"/s1.xml", "/s2.xml"}}, map[string]int{"/s1.xml": 4})

	for _, multiThread := range []bool{false, true} {
		t.Run(fmt.Sprintf("multiThread=%v", multiThread), func(t *testing.T) {
			url := site.URL + "/index.xml"
			s := New().SetMaxSitemaps(1).SetMaxURLs(3).SetMultiThread(multiThread)
			site.fetched()
			requireParse(t, s, url, nil)

			assertStringSlice(t, "sitemaps fetched", site.fetched()[1:], []string{"/s1.xml"})
			assertStringSlice(t, "URLs", locsOf(s), site.pagesOf("/s1.xml")[:3])
			// Nothing else is recorded, and the two errors are the last ones.
			var errs []string
			for _, err := range s.GetErrors() {
				errs = append(errs, err.Error())
			}
			assertStringSlice(t, "errors", errs, []string{
				fmt.Sprintf("parse %q: limit of 1 sitemaps reached", url),
				fmt.Sprintf("parse %q: limit of 3 URLs reached", url),
			})
		})
	}
}

// TestS_Parse_Limits_SetWhileParsing verifies that the limits of a call are
// the ones set when the call starts: a limit set while the call runs applies
// to the next call.
func TestS_Parse_Limits_SetWhileParsing(t *testing.T) {
	site := newSiteServer(t, map[string][]string{"/index.xml": {"/s1.xml", "/s2.xml", "/s3.xml"}}, nil)
	url := site.URL + "/index.xml"

	s := New().SetMultiThread(false)
	site.onRequest = func(path string) {
		if path == "/s1.xml" {
			s.SetMaxSitemaps(2).SetMaxURLs(3)
		}
	}

	requireParse(t, s, url, nil)
	assertCounts(t, s, 6, 0)

	requireParse(t, s, url, nil)
	assertStringSlice(t, "URLs", locsOf(s), site.pagesOfAll("/s1.xml", "/s2.xml")[:3])
	limits, others := limitErrors(t, s, url)
	assertStringSlice(t, "limit errors", limits, []string{"limit of 2 sitemaps reached", "limit of 3 URLs reached"})
	assertStringSlice(t, "other errors", others, []string{})

	// What a call left out is no concern of the next one.
	site.onRequest = nil
	requireParse(t, s.SetMaxSitemaps(0).SetMaxURLs(0), url, nil)
	assertCounts(t, s, 6, 0)
}

// cancelOnClose is a response body that cancels a context when it is closed:
// when the response has been read.
type cancelOnClose struct {
	io.Reader
	cancel context.CancelFunc
}

func (body cancelOnClose) Close() error {
	body.cancel()
	return nil
}

// TestS_ParseContext_Limit_CutShort verifies that a call that is cut short
// reports the limit it reached until then as well, and that the error it
// returns, the one that tells it was cut short, remains the last one.
func TestS_ParseContext_Limit_CutShort(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	const content = `<urlset><url><loc>https://example.com/page-1</loc></url><url><loc>https://example.com/page-2</loc></url></urlset>`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The context is cancelled when the document has been read, so the call is cut short
	// with the document at hand.
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       cancelOnClose{Reader: strings.NewReader(content), cancel: cancel},
			Request:    req,
		}, nil
	})}

	s := New().SetHTTPClient(client).SetMaxURLs(1)
	_, err := s.ParseContext(ctx, url, nil)

	assertStringSlice(t, "URLs", locsOf(s), []string{"https://example.com/page-1"})
	errs := s.GetErrors()
	var texts []string
	for _, recorded := range errs {
		texts = append(texts, recorded.Error())
	}
	assertStringSlice(t, "errors", texts, []string{
		fmt.Sprintf("parse %q: limit of 1 URLs reached", url),
		fmt.Sprintf("parse %q: context canceled", url),
	})
	if len(errs) == 2 && err != errs[1] {
		t.Errorf("expected the error that tells the call was cut short to be returned, got %v", err)
	}
}

// endlessSiteServer starts a server for a site whose sitemaps never end: every
// sitemap index lists a sitemap of the given number of pages, and three sitemap
// indexes that were not listed before. It returns the server and the counter
// of the requests it has answered.
func endlessSiteServer(t *testing.T, pages int) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var requests, listed atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasPrefix(r.URL.Path, "/pages-") {
			_, _ = fmt.Fprint(w, `<urlset>`)
			for i := 0; i < pages; i++ {
				_, _ = fmt.Fprintf(w, `<url><loc>%s%s/%d</loc></url>`, server.URL, r.URL.Path, i)
			}
			_, _ = fmt.Fprint(w, `</urlset>`)
			return
		}
		_, _ = fmt.Fprint(w, `<sitemapindex>`)
		_, _ = fmt.Fprintf(w, `<sitemap><loc>%s/pages-%d.xml</loc></sitemap>`, server.URL, listed.Add(1))
		for i := 0; i < 3; i++ {
			_, _ = fmt.Fprintf(w, `<sitemap><loc>%s/index-%d.xml</loc></sitemap>`, server.URL, listed.Add(1))
		}
		_, _ = fmt.Fprint(w, `</sitemapindex>`)
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

// TestS_Parse_Limits_EndlessSite verifies that each of the two limits ends a
// call for a site that lists sitemaps without end, a call without a context
// that would end it otherwise.
func TestS_Parse_Limits_EndlessSite(t *testing.T) {
	for _, multiThread := range []bool{false, true} {
		t.Run(fmt.Sprintf("SetMaxSitemaps, multiThread=%v", multiThread), func(t *testing.T) {
			server, requests := endlessSiteServer(t, 10)
			url := server.URL + "/index-0.xml"

			s := New().SetMaxSitemaps(200).SetMaxDepth(1000).SetMultiThread(multiThread)
			requireParse(t, s, url, nil)

			// The document of the call, and the sitemaps the limit allows.
			mustEqual(t, "requests", requests.Load(), 201)
			limits, others := limitErrors(t, s, url)
			assertStringSlice(t, "limit errors", limits, []string{"limit of 200 sitemaps reached"})
			assertStringSlice(t, "other errors", others, []string{})
		})

		t.Run(fmt.Sprintf("SetMaxURLs, multiThread=%v", multiThread), func(t *testing.T) {
			server, requests := endlessSiteServer(t, 10)
			url := server.URL + "/index-0.xml"

			s := New().SetMaxSitemaps(0).SetMaxURLs(500).SetMaxDepth(1000).SetMultiThread(multiThread)
			requireParse(t, s, url, nil)

			mustEqual(t, "GetURLCount", s.GetURLCount(), 500)
			// 50 sitemaps hold that many pages. The sitemap indexes that list them, and
			// the sitemaps that were under way when the limit was reached, come on top.
			if got := requests.Load(); got > 2000 {
				t.Errorf("expected the call to end soon after the limit is reached, it sent %d requests", got)
			}
			limits, others := limitErrors(t, s, url)
			assertStringSlice(t, "limit errors", limits, []string{"limit of 500 URLs reached"})
			assertStringSlice(t, "other errors", others, []string{})
		})
	}
}

// TestS_newDocument_addDocument verifies how the room a call has for URLs is
// shared between documents that are processed at the same time: each may
// collect as many URLs as there is room for when it is begun, and what there is
// no room for any more when it is added is given up.
func TestS_newDocument_addDocument(t *testing.T) {
	const url = "https://example.com/sitemap.xml"
	content := `<urlset>` +
		`<url><loc>https://example.com/page-1</loc></url>` +
		`<url><loc>https://example.com/page-2</loc></url>` +
		`<url><loc>https://example.com/page-3</loc></url>` +
		`<url><loc>https://example.com/page-4</loc></url>` +
		`</urlset>`
	pages := []string{"https://example.com/page-1", "https://example.com/page-2", "https://example.com/page-3", "https://example.com/page-4"}

	t.Run("documents begun before either is added", func(t *testing.T) {
		s := New()
		s.limits.maxURLs = 5

		first, second := s.newDocument(), s.newDocument()
		mustEqual(t, "room of the first document", first.limits.maxURLs, 5)
		mustEqual(t, "room of the second document", second.limits.maxURLs, 5)
		first.parse(url, content)
		second.parse(url, content)
		tail := second.urls[1:]

		s.addDocument(first)
		assertStringSlice(t, "URLs", locsOf(s), pages)
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, false)

		s.addDocument(second)
		assertStringSlice(t, "URLs", locsOf(s), append(append([]string{}, pages...), pages[0]))
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, true)
		// What was left out is not held on to by the URLs that were kept.
		for i, u := range tail {
			if u.Loc != "" {
				t.Errorf("expected the URL left out at %d to be given up, got %q", i, u.Loc)
			}
		}
	})

	t.Run("document begun after the other is added", func(t *testing.T) {
		s := New()
		s.limits.maxURLs = 5

		first := s.newDocument()
		first.parse(url, content)
		s.addDocument(first)

		second := s.newDocument()
		mustEqual(t, "room of the second document", second.limits.maxURLs, 1)
		second.parse(url, content)
		mustEqual(t, "URLs of the second document", len(second.urls), 1)
		s.addDocument(second)
		assertStringSlice(t, "URLs", locsOf(s), append(append([]string{}, pages...), pages[0]))
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, true)
	})

	t.Run("no room left", func(t *testing.T) {
		s := New()
		s.limits.maxURLs = 4

		first := s.newDocument()
		first.parse(url, content)
		s.addDocument(first)
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, false)

		// The call has no use for a document it cannot collect a URL of.
		if document := s.newDocument(); document != nil {
			t.Fatal("expected no document to be begun")
		}
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, true)

		s.limits.urlsLeftOut = false
		locations, err := s.parseDocument(url, `<sitemapindex><sitemap><loc>https://example.com/sitemap-2.xml</loc></sitemap></sitemapindex>`)
		if locations != nil || err != nil {
			t.Errorf("expected the document not to be parsed, got %v, %v", locations, err)
		}
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, true)
		assertCounts(t, s, 4, 0)
	})

	t.Run("no limit", func(t *testing.T) {
		s := New()

		first, second := s.newDocument(), s.newDocument()
		mustEqual(t, "room of a document", first.limits.maxURLs, 0)
		first.parse(url, content)
		second.parse(url, content)
		s.addDocument(first)
		s.addDocument(second)
		assertStringSlice(t, "URLs", locsOf(s), append(append([]string{}, pages...), pages...))
		mustEqual(t, "URLs left out", s.limits.urlsLeftOut, false)
	})
}

func configsEqual(c1, c2 config) bool {
	return c1.fetchTimeout == c2.fetchTimeout &&
		c1.userAgent == c2.userAgent &&
		c1.maxResponseSize == c2.maxResponseSize &&
		c1.maxDepth == c2.maxDepth &&
		c1.maxConcurrency == c2.maxConcurrency &&
		c1.maxSitemaps == c2.maxSitemaps &&
		c1.maxURLs == c2.maxURLs &&
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

func pointerOfLastModTime(lmt LastModTime) *LastModTime {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

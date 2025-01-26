package sitemap

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp/syntax"
	"sort"
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
				userAgent:    "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
				fetchTimeout: 3,
				multiThread:  true,
				follow:       []string{},
				rules:        []string{},
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
		timeout uint8
	}{
		{
			name:    "PositiveTimeout",
			timeout: 5,
		},
		{
			name:    "ZeroTimeout",
			timeout: 0,
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
			name:                 "invalid url",
			url:                  "invalid_url",
			multiThread:          true,
			follow:               []string{},
			rules:                []string{},
			err:                  pointerOfString("Get \"invalid_url\": unsupported protocol scheme \"\""),
			mainURLContent:       pointerOfString(""),
			robotsTxtSitemapURLs: nil,
			sitemapLocations:     nil,
			urls:                 nil,
			errs: []error{
				&url.Error{
					Op:  "Get",
					URL: "invalid_url",
					Err: errors.New("unsupported protocol scheme \"\""),
				},
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqYearly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqYearly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-07", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqNever),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-08", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-09", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-10", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-11", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-12", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqYearly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqYearly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-04", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 0, 0, 0, 0, timeLocationUTC)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqWeekly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-05", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqMonthly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-06", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqYearly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqAlways),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-alpha-02", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
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
					ChangeFreq: pointerOfURLChangeFreq(changeFreqHourly),
					Priority:   pointerOfFloat32(0.5),
				},
				{
					Loc:        fmt.Sprintf("%s/page-03", server.URL),
					LastMod:    pointerOfLastModTime(lastModTime{time.Date(2024, time.February, 12, 12, 34, 56, 0, timeLocationCET)}),
					ChangeFreq: pointerOfURLChangeFreq(changeFreqDaily),
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
			if sitemap.mainURL != test.url {
				t.Fatalf("Expected URL to be %s, but got %s", test.url, sitemap.mainURL)
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
				return &S{
					mainURL: fmt.Sprintf("%s/example", server.URL),
				}
			},
			attrURLContent: pointerOfString("URL Content"),
			wantURLContent: "URL Content",
			wantErr:        nil,
		},
		{
			name: "setContent_without_urlContent",
			setup: func() *S {
				return &S{
					mainURL: fmt.Sprintf("%s/example", server.URL),
				}
			},
			attrURLContent: nil,
			wantURLContent: "example content\n",
			wantErr:        nil,
		},
		{
			name: "setContent_without_urlContent_with_invalid_mainURL",
			setup: func() *S {
				return &S{
					mainURL: fmt.Sprintf("%s/404", server.URL),
				}
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()
			s.parseRobotsTXT(test.input)

			if len(s.robotsTxtSitemapURLs) != test.output {
				t.Errorf("Input %s: expected %d, got %d", test.input, test.output, len(s.robotsTxtSitemapURLs))
			}
		})
	}
}

func TestS_fetch(t *testing.T) {
	server := testServer()
	defer server.Close()

	s := S{cfg: config{fetchTimeout: 3}}
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
			fields:  fields{config{fetchTimeout: 0}},
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
			s := &S{cfg: config{userAgent: "test-agent", fetchTimeout: 3}, errs: []error{}}
			s.parseAndFetchUrlsMultiThread(test.locations)

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
			s := &S{cfg: config{userAgent: "test-agent", fetchTimeout: 3}, errs: []error{}}
			s.parseAndFetchUrlsSequential(test.locations)

			if len(s.urls) != int(test.urlsCount) {
				t.Errorf("expected %d, got %d", test.urlsCount, len(s.urls))
			}

			if len(s.errs) != int(test.errsCount) {
				t.Errorf("expected %d, got %d", test.errsCount, len(s.errs))
			}
		})
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

func TestS_unzip(t *testing.T) {
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
			s := New()

			uncompressed, err := s.unzip(test.input)

			if (err != nil) != test.hasError {
				t.Errorf("expected %v, got %v", test.hasError, err)
			}

			if !bytes.Equal(uncompressed, test.output) {
				t.Errorf("expected %v, got %v", test.output, uncompressed)
			}

		})
	}
}

func TestS_zip(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		output   []byte
		hasError bool
	}{
		{
			name:     "Valid content",
			input:    []byte("hello world"),
			output:   gzipByte("hello world"),
			hasError: false,
		},
		{
			name:     "Empty content",
			input:    []byte(""),
			output:   gzipByte(""),
			hasError: false,
		},
		{
			name:     "Nil content",
			input:    nil,
			output:   gzipByte(""),
			hasError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := New()

			compressed, err := s.zip(test.input)

			if (err != nil) != test.hasError {
				t.Errorf("expected %v, got %v", test.hasError, err)
			}

			if !bytes.Equal(compressed, test.output) {
				t.Errorf("expected %v, got %v", test.output, compressed)
			}

		})
	}
}

func configsEqual(c1, c2 config) bool {
	return c1.fetchTimeout == c2.fetchTimeout &&
		c1.userAgent == c2.userAgent &&
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

func pointerOfURLChangeFreq(changeFreq urlChangeFreq) *urlChangeFreq {
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

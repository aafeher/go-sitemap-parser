package sitemap

import (
	"bytes"
	"compress/gzip"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzBaseURL is the sitemap URL that fuzzed content is treated as having been
// fetched from. It is deliberately short and valid so that any invariant
// violation observed during fuzzing originates from the fuzzed content rather
// than from the base URL.
const fuzzBaseURL = "https://example.com/sitemap.xml"

// addFileSeeds adds every file in test/ matching pattern to the fuzz corpus.
// Seeding from the real fixtures gives the fuzzer valid structures to mutate,
// which reaches the deeper parser branches far sooner than random input would.
func addFileSeeds(f *testing.F, pattern string, add func(*testing.F, []byte)) {
	f.Helper()

	paths, err := filepath.Glob(filepath.Join("test", pattern))
	if err != nil {
		f.Fatalf("globbing test/%s: %v", pattern, err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("reading seed %s: %v", path, err)
		}
		add(f, data)
	}
}

// assertLocInvariants checks the guarantees resolveAndValidateLoc is documented
// to provide for every location that reaches the caller: the scheme is http or
// https, and the URL is within the sitemaps.org length limit.
func assertLocInvariants(t *testing.T, mode string, locs []string) {
	t.Helper()

	for _, loc := range locs {
		if len(loc) > maxLocLength {
			t.Fatalf("%s mode: accepted URL of %d characters, limit is %d: %q",
				mode, len(loc), maxLocLength, loc)
		}
		parsed, err := neturl.Parse(loc)
		if err != nil {
			t.Fatalf("%s mode: accepted unparsable URL %q: %v", mode, loc, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			t.Fatalf("%s mode: accepted URL with scheme %q: %q", mode, parsed.Scheme, loc)
		}
	}
}

// collectLocs runs parse over content and returns every location derived from
// it: the sitemap locations returned for a sitemap index, plus the URLs
// collected into s.urls. The base URL itself is not included, so the result
// depends only on the fuzzed input.
func collectLocs(strict bool, content string) []string {
	s := New().SetStrict(strict)

	locs := append([]string(nil), s.parse(fuzzBaseURL, content)...)
	for _, u := range s.urls {
		locs = append(locs, u.Loc)
	}

	return locs
}

// FuzzParse exercises the full format-dispatch path — sitemap index, urlset,
// RSS, Atom and plain text — with untrusted content. Every location the parser
// hands back must satisfy the documented URL invariants in both tolerant and
// strict mode, and parsing must be deterministic.
func FuzzParse(f *testing.F) {
	seeds := []string{
		`<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>https://example.com/sitemap-1.xml</loc><lastmod>2024-01-01</lastmod></sitemap></sitemapindex>`,
		`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://example.com/a</loc><priority>0.5</priority><lastmod>2024-01-02T03:04Z</lastmod></url></urlset>`,
		`<?xml version="1.0"?><rss version="2.0"><channel><item><link>https://example.com/rss-item</link></item></channel></rss>`,
		`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="https://example.com/atom-entry"/></entry></feed>`,
		"https://example.com/one\nhttps://example.com/two\n# comment\n",
		`<urlset><url><loc>/relative/path</loc></url></urlset>`,
		`<urlset><url><loc>` + strings.Repeat("a", maxLocLength+100) + `</loc></url></urlset>`,
		`<urlset><url><loc>javascript:alert(1)</loc></url></urlset>`,
		`<urlset><url><loc>   https://example.com/padded   </loc></url></urlset>`,
		"\ufeff<urlset><url><loc>https://example.com/bom</loc></url></urlset>",
		`<urlset><url><loc>https://example.com/img</loc><image:image xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"><image:loc>https://example.com/i.jpg</image:loc></image:image></url></urlset>`,
		`<sitemapindex><sitemap><loc>`,
		"",
		"not xml at all",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	addFileSeeds(f, "*.xml", func(f *testing.F, data []byte) { f.Add(string(data)) })
	addFileSeeds(f, "*.txt", func(f *testing.F, data []byte) { f.Add(string(data)) })

	f.Fuzz(func(t *testing.T, content string) {
		for _, strict := range []bool{false, true} {
			mode := "tolerant"
			if strict {
				mode = "strict"
			}

			locs := collectLocs(strict, content)
			assertLocInvariants(t, mode, locs)

			// Parsing the same bytes twice must produce the same result.
			if again := collectLocs(strict, content); !slicesEqual(locs, again) {
				t.Fatalf("%s mode: parse is not deterministic: %q vs %q", mode, locs, again)
			}
		}
	})
}

// FuzzDetectRootElement checks that root-element detection never panics on
// malformed XML and only ever reports a well-formed XML local name.
func FuzzDetectRootElement(f *testing.F) {
	seeds := []string{
		`<urlset></urlset>`,
		`<?xml version="1.0"?><!-- comment --><sitemapindex/>`,
		`<!DOCTYPE html><html><body>hi</body></html>`,
		"<<<<<<",
		"<a:b xmlns:a='urn:x'/>",
		"",
		"plain text",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	addFileSeeds(f, "*.xml", func(f *testing.F, data []byte) { f.Add(string(data)) })

	f.Fuzz(func(t *testing.T, content string) {
		root := detectRootElement(content)
		if root == "" {
			return
		}
		if strings.ContainsAny(root, "<>/ \t\r\n") {
			t.Fatalf("detectRootElement returned a malformed name %q", root)
		}
	})
}

// FuzzUnzip checks that gzip handling survives arbitrary bytes, and that any
// data the package compresses can be read back unchanged.
func FuzzUnzip(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("not gzip"))
	f.Add([]byte("\x1f\x8b\x08"))            // gzip magic, truncated
	f.Add([]byte("\x1f\x8b\x08\x00garbage")) // gzip magic, corrupt body
	addFileSeeds(f, "*.gz", func(f *testing.F, data []byte) { f.Add(data) })

	f.Fuzz(func(t *testing.T, data []byte) {
		// Arbitrary input: unzip must fail cleanly rather than panic.
		_, _ = unzip(data)

		// Round trip: whatever we compress must come back byte-for-byte.
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(data); err != nil {
			t.Fatalf("compressing seed: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing gzip writer: %v", err)
		}

		out, err := unzip(buf.Bytes())
		if err != nil {
			t.Fatalf("unzip rejected data this package compressed: %v", err)
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("gzip round trip altered the payload: got %d bytes, want %d", len(out), len(data))
		}

		// checkAndUnzipContent must agree with unzip on well-formed input.
		s := New()
		if got := s.checkAndUnzipContent(fuzzBaseURL, buf.Bytes()); !bytes.Equal(got, data) {
			t.Fatalf("checkAndUnzipContent returned %d bytes, want %d", len(got), len(data))
		}
	})
}

// FuzzParseRobotsTXT checks the Sitemap: directive extraction against arbitrary
// robots.txt content. Extracted values must be trimmed, non-empty, and free of
// the inline comments the parser is responsible for stripping.
func FuzzParseRobotsTXT(f *testing.F) {
	seeds := []string{
		"Sitemap: https://example.com/sitemap.xml\n",
		"User-agent: *\nDisallow: /private\nSitemap: https://example.com/a.xml\nSitemap: https://example.com/b.xml\n",
		"  sitemap:   https://example.com/indented.xml   # trailing comment\n",
		"SITEMAP:https://example.com/upper.xml\r\n",
		"\ufeffSitemap: https://example.com/bom.xml\n",
		"# Sitemap: https://example.com/commented-out.xml\n",
		"Sitemap:\n",
		"Sitemap: #only-a-comment\n",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		s := New()
		s.parseRobotsTXT(content)

		for _, url := range s.robotsTxtSitemapURLs {
			if url == "" {
				t.Fatal("parseRobotsTXT recorded an empty sitemap URL")
			}
			if url != strings.TrimSpace(url) {
				t.Fatalf("parseRobotsTXT recorded an untrimmed sitemap URL %q", url)
			}
			if strings.Contains(url, "#") {
				t.Fatalf("parseRobotsTXT left an inline comment in %q", url)
			}
		}
	})
}

// slicesEqual reports whether two string slices hold the same values in the
// same order. Used to assert that parsing is deterministic.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

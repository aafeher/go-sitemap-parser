package sitemap

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fuzzBaseURL is the sitemap URL that fuzzed content is treated as having been
// fetched from. It is deliberately short and valid so that any invariant
// violation observed during fuzzing originates from the fuzzed content rather
// than from the base URL.
const fuzzBaseURL = "https://example.com/sitemap.xml"

// fuzzRobotsTXTURL is the URL that fuzzed robots.txt content is treated as
// having been fetched from.
const fuzzRobotsTXTURL = "https://example.com/robots.txt"

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

// offlineClient is an HTTP client that fails every request without sending it,
// so that no request leaves the process whatever the fuzzed content lists.
var offlineClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
})}

// assertReturnedError checks the error Parse returns for content against the
// errors it records. The call must fail if, and only if, the document could not
// be parsed, and the error it returns must be the first *ParseError recorded
// then: the very error, not one that reads the same.
func assertReturnedError(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(offlineClient)
	_, err := s.Parse(fuzzBaseURL, &content)

	// The sitemaps a sitemap index lists are not fetched, so every *ParseError
	// there is concerns the document itself.
	var want error
	for _, recorded := range s.GetErrors() {
		var parseErr *ParseError
		if errors.As(recorded, &parseErr) {
			want = recorded
			break
		}
	}
	if err != want {
		t.Fatalf("%s mode: Parse returned %v, the first *ParseError recorded is %v", mode, err, want)
	}
}

// assertURLLimit checks that a limit on the URLs of a call takes nothing but
// the end off what the call collects: the URLs are the first ones it collects
// without a limit, and the limit is reported whenever there are more of them.
func assertURLLimit(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	parse := func(limit int) *S {
		s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(offlineClient).SetMaxURLs(limit)
		_, _ = s.Parse(fuzzBaseURL, &content)
		return s
	}

	all := parse(0).GetURLs()
	limit := len(all)/2 + 1
	limited := parse(limit)

	want := all
	if len(all) > limit {
		want = all[:limit]
	}
	if got := limited.GetURLs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mode: with a limit of %d URLs, expected the first %d of %d URLs, got %d: %v",
			mode, limit, len(want), len(all), len(got), got)
	}

	reported := false
	for _, err := range limited.GetErrors() {
		var parseErr *ParseError
		if errors.As(err, &parseErr) && parseErr.URL == fuzzBaseURL && strings.HasSuffix(parseErr.Err.Error(), " URLs reached") {
			reported = true
		}
	}
	switch {
	case len(all) > limit && !reported:
		t.Fatalf("%s mode: %d of %d URLs were left out without the limit being reported", mode, len(all)-limit, len(all))
	case len(all) < limit && reported:
		t.Fatalf("%s mode: the limit of %d URLs was reported with %d URLs", mode, limit, len(all))
	}
}

// FuzzParse exercises the full format-dispatch path — sitemap index, urlset,
// RSS, Atom and plain text — with untrusted content. Every location the parser
// hands back must satisfy the documented URL invariants in both tolerant and
// strict mode, parsing must be deterministic, a byte order mark put before the
// document must change nothing, the call must fail exactly when the document
// cannot be parsed, and a limit on the URLs must leave the first ones as they
// are.
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
		"\ufeffhttps://example.com/bom-one\nhttps://example.com/bom-two\n",
		"<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><urlset><url><loc>https://example.com/caf\xe9</loc></url></urlset>",
		"<?xml version=\"1.0\" encoding=\"windows-1250\"?><rss><channel><item><link>https://example.com/t\xfbr\xf5</link></item></channel></rss>",
		`<?xml version="1.0" encoding="IBM437"?><sitemapindex><sitemap><loc>https://example.com/sitemap-1.xml</loc></sitemap></sitemapindex>`,
		`<urlset><url><loc>https://example.com/a</loc><lastmod>2024-01-15 10:30:00</lastmod><priority>0,5</priority></url></urlset>`,
		`<urlset xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"><url><loc>https://example.com/v</loc><video:video><video:thumbnail_loc>https://example.com/t.jpg</video:thumbnail_loc><video:duration>1:30</video:duration></video:video></url></urlset>`,
		`<urlset><url><loc>https://example.com/a?b=1&c=2</loc><lastmod>2024-01-15</url></urlset>`,
		`<urlset><url><loc>https://example.com/img</loc><image:image xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"><image:loc>https://example.com/i.jpg</image:loc></image:image></url></urlset>`,
		`<urlset><url><loc></loc></url><url><lastmod>2024-01-15</lastmod></url><url><loc> </loc></url></urlset>`,
		`<sitemapindex><sitemap><loc></loc></sitemap><sitemap></sitemap></sitemapindex>`,
		`<rss><channel><item><title>no link</title></item><item><link> </link></item></channel></rss>`,
		`<feed><entry><title>no link</title></entry><entry><link href=" "/></entry></feed>`,
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

			// A byte order mark the document begins with is no part of it. A
			// document that begins with one already is left as it is: the second
			// mark would not be one.
			if !strings.HasPrefix(content, utf8BOM) {
				if marked := collectLocs(strict, utf8BOM+content); !slicesEqual(locs, marked) {
					t.Fatalf("%s mode: a leading byte order mark changes the result: %q vs %q", mode, locs, marked)
				}
			}

			assertReturnedError(t, mode, strict, content)
			assertURLLimit(t, mode, strict, content)
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
		"<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><!-- caf\xe9 --><urlset/>",
		`<?xml version="1.0" encoding="IBM437"?><feed/>`,
		`<urlset version=1 a&b>`,
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

// fuzzUnzipLimit is the decompressed-size limit FuzzUnzip uses to check that
// unzip enforces its cap. It is small enough that mutated seeds regularly land
// on both sides of it.
const fuzzUnzipLimit = 64

// FuzzUnzip checks that gzip handling survives arbitrary bytes, that any data
// the package compresses can be read back unchanged, and that the
// decompressed-size limit is enforced.
func FuzzUnzip(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("not gzip"))
	f.Add([]byte("\x1f\x8b\x08"))            // gzip magic, truncated
	f.Add([]byte("\x1f\x8b\x08\x00garbage")) // gzip magic, corrupt body
	addFileSeeds(f, "*.gz", func(f *testing.F, data []byte) { f.Add(data) })

	f.Fuzz(func(t *testing.T, data []byte) {
		// Arbitrary input: unzip must fail cleanly rather than panic, and must
		// never hand back more than the limit allows.
		if out, err := unzip(string(data), fuzzUnzipLimit); err == nil && len(out) > fuzzUnzipLimit {
			t.Fatalf("unzip returned %d bytes, limit is %d", len(out), fuzzUnzipLimit)
		}

		// Round trip: whatever we compress must come back byte-for-byte.
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(data); err != nil {
			t.Fatalf("compressing seed: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing gzip writer: %v", err)
		}

		out, err := unzip(buf.String(), defaultMaxResponseSize)
		if err != nil {
			t.Fatalf("unzip rejected data this package compressed: %v", err)
		}
		if out != string(data) {
			t.Fatalf("gzip round trip altered the payload: got %d bytes, want %d", len(out), len(data))
		}

		// Size limit: a payload that fits must come back intact, and one that
		// does not must be rejected without returning any data.
		limited, err := unzip(buf.String(), fuzzUnzipLimit)
		if len(data) <= fuzzUnzipLimit {
			if err != nil || limited != string(data) {
				t.Fatalf("unzip rejected or altered a %d-byte payload within the %d-byte limit: %v", len(data), fuzzUnzipLimit, err)
			}
		} else if err == nil || limited != "" {
			t.Fatalf("unzip accepted a %d-byte payload over the %d-byte limit (returned %d bytes, err %v)", len(data), fuzzUnzipLimit, len(limited), err)
		}

		// checkAndUnzipContent must agree with unzip on well-formed input.
		s := New()
		if got := s.checkAndUnzipContent(fuzzBaseURL, buf.String()); got != string(data) {
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
		"Sitemap: /relative.xml\nSitemap: maps/relative.xml\nSitemap: //cdn.example.net/sitemap.xml\n",
		"Sitemap: ftp://example.com/sitemap.xml\nSitemap: file:///etc/passwd\nSitemap: javascript:alert(1)\n",
		"Sitemap: https://example.com/" + strings.Repeat("a", maxLocLength+100) + "\n",
		"Sitemap: https://example.com/%zz.xml\n",
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

		// Whatever the Sitemap lines hold, the sitemaps that end up being
		// fetched satisfy the URL invariants in both modes, and deciding on
		// them is deterministic.
		for _, strict := range []bool{false, true} {
			mode := "tolerant"
			if strict {
				mode = "strict"
			}

			locs := robotsTXTLocs(strict, content)
			assertLocInvariants(t, mode, locs)

			if again := robotsTXTLocs(strict, content); !slicesEqual(locs, again) {
				t.Fatalf("%s mode: result is not deterministic: %q vs %q", mode, locs, again)
			}
		}
	})
}

// robotsTXTLocs returns the sitemaps that are fetched of the ones the given
// robots.txt content names.
func robotsTXTLocs(strict bool, content string) []string {
	s := New().SetStrict(strict)
	s.parseRobotsTXT(content)

	return s.robotsTXTSitemapLocations(fuzzRobotsTXTURL)
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

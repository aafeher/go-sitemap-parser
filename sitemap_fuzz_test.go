package sitemap

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/net/idna"
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
// to provide for every location that reaches the caller: it is a URL that can
// be parsed, the scheme is http or https, it names a host, it is within the
// sitemaps.org length limit, and it holds neither a space nor a control
// character, so that it can be requested as it is.
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
		if parsed.Hostname() == "" {
			t.Fatalf("%s mode: accepted URL without a host: %q", mode, loc)
		}
		if strings.ContainsFunc(loc, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
			t.Fatalf("%s mode: accepted URL with a space or a control character in it: %q", mode, loc)
		}
	}
}

// assertASCII checks what tolerant mode guarantees on top of that: the URL is
// in ASCII, whatever is not being percent-encoded, and so is the name of the
// host when it is read from the URL. A name that is not is one that has no
// ASCII form, which IDNA is asked about here independently of the parser, or
// one that is not UTF-8 or has the replacement character in it.
func assertASCII(t *testing.T, locs []string) {
	t.Helper()

	for _, loc := range locs {
		if strings.ContainsFunc(loc, func(r rune) bool { return r > 0x7f }) {
			t.Fatalf("tolerant mode: URL %q is not in ASCII", loc)
		}
		parsed, err := neturl.Parse(loc)
		if err != nil {
			t.Fatalf("tolerant mode: accepted unparsable URL %q: %v", loc, err)
		}
		host := parsed.Hostname()
		if strings.ContainsFunc(host, func(r rune) bool { return r > 0x7f }) && !strings.ContainsRune(host, utf8.RuneError) {
			if ascii, err := idna.Lookup.ToASCII(host); err == nil && ascii != "" {
				t.Fatalf("tolerant mode: host of %q is not in its ASCII form %q", loc, ascii)
			}
		}
	}
}

// assertOnSitemapHost checks what strict mode guarantees on top of that for the
// locations a sitemap lists: each is on the protocol, the host and the port of
// fuzzBaseURL. The letter case of the host makes no difference, and neither
// does it whether the default port of HTTPS is named. A host that is not
// written in ASCII is that of fuzzBaseURL if it is the name IDNA looks it up
// by, which is asked here independently of the parser.
func assertOnSitemapHost(t *testing.T, locs []string) {
	t.Helper()

	for _, loc := range locs {
		parsed, err := neturl.Parse(loc)
		if err != nil {
			t.Fatalf("strict mode: accepted unparsable URL %q: %v", loc, err)
		}
		if parsed.Scheme != "https" {
			t.Fatalf("strict mode: accepted URL of another protocol than the sitemap: %q", loc)
		}
		host := parsed.Hostname()
		if ascii, err := idna.Lookup.ToASCII(host); err == nil && ascii != "" {
			host = ascii
		}
		if !strings.EqualFold(host, "example.com") {
			t.Fatalf("strict mode: accepted URL on another host than the sitemap: %q", loc)
		}
		if port := parsed.Port(); port != "" && port != "443" {
			t.Fatalf("strict mode: accepted URL on another port than the sitemap: %q", loc)
		}
	}
}

// assertNoExcludedCharacter checks what strict mode guarantees for every URL it
// hands back on top of that: the URL holds none of the printable ASCII
// characters that have no place in a URL, no square bracket but the two around
// an IPv6 address, and no percent sign in its query that begins no
// percent-encoding.
func assertNoExcludedCharacter(t *testing.T, locs []string) {
	t.Helper()

	for _, loc := range locs {
		if i := strings.IndexAny(loc, "\"<>\\^`{|}"); i >= 0 {
			t.Fatalf("strict mode: accepted URL with the character %q in it: %q", loc[i], loc)
		}
		if !bracketsInPlace(loc) {
			t.Fatalf("strict mode: accepted URL with a square bracket that is not around an IPv6 address: %q", loc)
		}
		if !queryEscaped(loc) {
			t.Fatalf("strict mode: accepted URL with a percent sign in its query that begins no percent-encoding: %q", loc)
		}
	}
}

// bracketsInPlace tells whether loc, a URL net/url reads, has its square
// brackets where a URL may hold them, which is decided here independently of
// the parser: it has none, or it has one of each and its host is written in
// them. net/url reads only an IPv6 address as a host that begins with one.
func bracketsInPlace(loc string) bool {
	opening, closing := strings.Count(loc, "["), strings.Count(loc, "]")
	if opening+closing == 0 {
		return true
	}
	parsed, err := neturl.Parse(loc)
	return err == nil && opening == 1 && closing == 1 && strings.HasPrefix(parsed.Host, "[")
}

// queryEscaped tells whether every percent sign in the query of loc, a URL
// net/url reads, begins a percent-encoding, which net/url is asked about.
func queryEscaped(loc string) bool {
	parsed, err := neturl.Parse(loc)
	if err != nil {
		return false
	}
	_, err = neturl.QueryUnescape(parsed.RawQuery)
	return err == nil
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

// assertValidationErrorLimit checks that a limit on the validation errors of
// a call takes nothing off the call but validation errors: the URLs and the
// sitemaps are those of a call without the limit, the call fails with what it
// fails with then, and the errors are those it records without the limit less
// the validation errors there is no room for. The limit is the last error
// whenever one of them was left out, and is reported at no other time.
func assertValidationErrorLimit(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	parse := func(limit int) (*S, error) {
		s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(offlineClient).SetMaxValidationErrors(limit)
		_, err := s.Parse(fuzzBaseURL, &content)
		return s, err
	}
	isValidationError := func(err error) bool {
		_, ok := err.(*ValidationError)
		return ok
	}

	all, allErr := parse(0)
	count := 0
	for _, err := range all.GetErrors() {
		if isValidationError(err) {
			count++
		}
	}
	limit := count/2 + 1
	limited, limitedErr := parse(limit)

	if got, want := limited.GetURLs(), all.GetURLs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mode: a limit of %d validation errors changes the URLs: got %v, want %v", mode, limit, got, want)
	}
	if got, want := limited.GetSitemaps(), all.GetSitemaps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mode: a limit of %d validation errors changes the sitemaps: got %v, want %v", mode, limit, got, want)
	}
	if (allErr == nil) != (limitedErr == nil) || allErr != nil && allErr.Error() != limitedErr.Error() {
		t.Fatalf("%s mode: a limit of %d validation errors changes the error returned: got %v, want %v", mode, limit, limitedErr, allErr)
	}

	want := []string{}
	recorded := 0
	for _, err := range all.GetErrors() {
		if errors.Is(err, ErrMaxValidationErrors) {
			t.Fatalf("%s mode: the limit on the validation errors was reported without a limit: %v", mode, err)
		}
		if isValidationError(err) {
			if recorded == limit {
				continue
			}
			recorded++
		}
		want = append(want, err.Error())
	}

	got := []string{}
	reported := 0
	for _, err := range limited.GetErrors() {
		if errors.Is(err, ErrMaxValidationErrors) {
			reported++
			continue
		}
		got = append(got, err.Error())
	}
	if !slicesEqual(got, want) {
		t.Fatalf("%s mode: with a limit of %d of %d validation errors, expected the errors %q, got %q", mode, limit, count, want, got)
	}

	switch errs := limited.GetErrors(); {
	case count > limit && reported != 1:
		t.Fatalf("%s mode: %d of %d validation errors were left out, and the limit was reported %d times", mode, count-limit, count, reported)
	case count > limit && !errors.Is(errs[len(errs)-1], ErrMaxValidationErrors):
		t.Fatalf("%s mode: the limit of %d validation errors is not the last error: %v", mode, limit, errs)
	case count <= limit && reported != 0:
		t.Fatalf("%s mode: the limit of %d validation errors was reported with %d of them", mode, limit, count)
	}
}

// unique returns the values of list in the order they are in it, each of them
// once.
func unique(list []string) []string {
	values := []string{}
	for _, value := range list {
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	return values
}

// assertSitemapsListed checks the sitemaps a call lists for content. With no
// pattern set with SetFollow they are the sitemaps the call is to fetch: each
// of them once, in the order the document lists them. A limit on the sitemaps
// takes nothing but the end off them, and is reported whenever there are more
// of them and at no other time.
func assertSitemapsListed(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	parse := func(limit int) *S {
		s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(offlineClient).SetMaxSitemaps(limit)
		_, _ = s.Parse(fuzzBaseURL, &content)
		return s
	}

	toFetch, _ := New().SetStrict(strict).parseDocument(fuzzBaseURL, content)
	unlimited := parse(0)
	all := listedLocs(unlimited)
	if want := unique(toFetch); !slicesEqual(all, want) {
		t.Fatalf("%s mode: the sitemaps listed are %q, the ones to fetch are %q", mode, all, want)
	}
	if count := unlimited.GetSitemapCount(); count != int64(len(all)) {
		t.Fatalf("%s mode: GetSitemapCount returns %d for %d sitemaps", mode, count, len(all))
	}

	limit := len(all)/2 + 1
	limited := parse(limit)

	want := all
	if len(all) > limit {
		want = all[:limit]
	}
	if got := listedLocs(limited); !slicesEqual(got, want) {
		t.Fatalf("%s mode: with a limit of %d sitemaps, expected the first %d of %d sitemaps, got %d: %q",
			mode, limit, len(want), len(all), len(got), got)
	}

	reported := false
	for _, err := range limited.GetErrors() {
		var parseErr *ParseError
		if errors.As(err, &parseErr) && parseErr.URL == fuzzBaseURL && strings.HasSuffix(parseErr.Err.Error(), " sitemaps reached") {
			reported = true
		}
	}
	if reported != (len(all) > limit) {
		t.Fatalf("%s mode: with a limit of %d sitemaps and %d sitemaps listed, the limit being reported is %v", mode, limit, len(all), reported)
	}
}

// assertInvalidDates checks that a date which cannot be parsed costs nothing
// but itself where it is the date of an entry of a feed or of a sitemap index,
// in either mode: the page of the entry is returned, without a date, and the
// sitemap of the entry is listed and is one of those to fetch.
func assertInvalidDates(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	root := detectRootElement(content)
	if root != "rss" && root != "feed" && root != "sitemapindex" {
		return
	}

	s := New().SetStrict(strict)
	toFetch := s.parse(fuzzBaseURL, content)
	listed := listedLocs(s)
	for _, err := range s.errs {
		// In these documents nothing but a date is reported as an invalid value.
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || !strings.HasPrefix(validationErr.Err.Error(), "invalid <") {
			continue
		}
		loc := validationErr.URL
		if root == "sitemapindex" {
			if !slices.Contains(listed, loc) || !slices.Contains(toFetch, loc) {
				t.Fatalf("%s mode: %v, and the sitemap is not among the ones listed, %q, or the ones to fetch, %q", mode, err, listed, toFetch)
			}
			continue
		}
		if !slices.ContainsFunc(s.urls, func(u URL) bool { return u.Loc == loc && u.LastMod == nil }) {
			t.Fatalf("%s mode: %v, and the page is not returned without a date: %v", mode, err, s.urls)
		}
	}
}

// assertTextValues checks the text values of the URLs collected from content:
// none of them has whitespace around it, a change frequency that is set names
// one, tolerant mode holds a change frequency of the protocol the way its
// constant spells it, and strict mode holds no other one.
// The location is left out: assertLocInvariants checks that it holds no space
// at all.
func assertTextValues(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	s := New().SetStrict(strict)
	s.parse(fuzzBaseURL, content)
	for _, u := range s.urls {
		for path, value := range textValues(u) {
			if path == "Loc" {
				continue
			}
			if value != strings.TrimSpace(value) {
				t.Fatalf("%s mode: %s of %q has whitespace around it: %q", mode, path, u.Loc, value)
			}
		}
		if u.ChangeFreq == nil {
			continue
		}
		if *u.ChangeFreq == "" {
			t.Fatalf("%s mode: the change frequency of %q is set, but empty", mode, u.Loc)
		}
		if canonical := canonicalChangeFreq(*u.ChangeFreq); !strict && canonical != *u.ChangeFreq {
			t.Fatalf("%s mode: the change frequency of %q is %q, not %q", mode, u.Loc, *u.ChangeFreq, canonical)
		}
		if strict && !slices.Contains(changeFreqs[:], *u.ChangeFreq) {
			t.Fatalf("%s mode: the change frequency of %q is %q, which is no value of the protocol", mode, u.Loc, *u.ChangeFreq)
		}
	}
}

// shortExtensionURL is the length up to which assertExtensionURLs takes a URL
// of an extension to be one tolerant mode has to percent-encode. A longer one
// may be left as it is for exceeding maxLocLength when encoded: a byte takes
// three characters percent-encoded, and the Punycode form of a host can be
// several times as long as the name it is the form of.
const shortExtensionURL = maxLocLength / 16

// assertExtensionURLs checks the URLs of the extensions of the URLs tolerant
// mode collects from content: the ones of the images, of the videos and of the
// alternate links. An absolute HTTP or HTTPS URL that names a host comes back
// the way the location does, in ASCII and without a space or a control
// character in it. A URL that holds one of these is therefore no such URL: it
// cannot be parsed, it is relative or of another scheme, it names no host, or
// it is too long to be encoded. Which of them it is, is decided here
// independently of the parser.
func assertExtensionURLs(t *testing.T, content string) {
	t.Helper()

	s := New()
	s.parse(fuzzBaseURL, content)
	for _, u := range s.urls {
		for _, extensionURL := range extensionURLsOf(u) {
			if !strings.ContainsFunc(extensionURL, func(r rune) bool { return r <= ' ' || r >= 0x7f }) {
				continue
			}
			parsed, err := neturl.Parse(extensionURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
				continue
			}
			if len(extensionURL) > shortExtensionURL {
				continue
			}
			t.Fatalf("tolerant mode: URL %q of an extension of %q is not percent-encoded", extensionURL, u.Loc)
		}
	}
}

// assertStrictExtensionURLs checks the URLs of the extensions of the URLs strict
// mode collects from content. The URL an entry is known by, the location of an
// image, the thumbnail of a video and the target of an alternate link, is what
// a location is, except that it may be on any host. Each of the other URLs of
// an image and of a video is one too, is not there, or is named by an error.
func assertStrictExtensionURLs(t *testing.T, content string) {
	t.Helper()

	s := New().SetStrict(true)
	s.parse(fuzzBaseURL, content)
	reported := map[string]bool{}
	for _, err := range s.errs {
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			reported[validationErr.URL] = true
		}
	}
	// isURL tells whether url is what strict mode requires a URL to be, which
	// is decided here independently of the parser.
	isURL := func(url string) bool {
		parsed, err := neturl.Parse(url)
		return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" &&
			len(url) <= maxLocLength && !strings.ContainsFunc(url, func(r rune) bool { return r <= ' ' || r == 0x7f }) &&
			!strings.ContainsAny(url, "\"<>\\^`{|}") && bracketsInPlace(url) && queryEscaped(url)
	}
	for _, u := range s.urls {
		var known, others []string
		for _, image := range u.Images {
			known = append(known, image.Loc)
			others = append(others, image.License)
		}
		for _, video := range u.Videos {
			known = append(known, video.ThumbnailLoc)
			others = append(others, video.ContentLoc, video.PlayerLoc)
			if video.Uploader != nil {
				others = append(others, video.Uploader.Info)
			}
		}
		for _, link := range u.Hreflangs {
			known = append(known, link.Href)
		}
		assertLocInvariants(t, "strict", known)
		assertNoExcludedCharacter(t, known)
		for _, other := range others {
			if other != "" && !isURL(other) && !reported[other] {
				t.Fatalf("strict mode: URL %q of an extension of %q is no URL and is not reported", other, u.Loc)
			}
		}
	}
}

// assertResultCopies checks what a call hands out of its results. The iterator
// over the URLs must yield the URLs GetURLs returns, and the one over the
// errors the very errors GetErrors returns. Neither of the slices may have
// room to spare, which an append would write to. And nothing that is done to
// the URLs the iterator and GetRandomURLs yield may reach the URLs of the
// instance: they are copies down to what they point to.
func assertResultCopies(t *testing.T, mode string, strict bool, content string) {
	t.Helper()

	parse := func() *S {
		s := New().SetStrict(strict).SetMultiThread(false).SetHTTPClient(offlineClient)
		_, _ = s.Parse(fuzzBaseURL, &content)
		return s
	}
	s, want := parse(), parse().GetURLs()

	urls, errs := s.GetURLs(), s.GetErrors()
	if cap(urls) != len(urls) || cap(errs) != len(errs) {
		t.Fatalf("%s mode: expected slices without room to spare, got %d of %d URLs and %d of %d errors",
			mode, len(urls), cap(urls), len(errs), cap(errs))
	}

	// Nothing is collected of no URLs, where GetURLs returns an empty slice.
	collected := slices.Collect(s.URLs())
	if len(collected) != len(urls) || (len(urls) > 0 && !reflect.DeepEqual(collected, urls)) {
		t.Fatalf("%s mode: the iterator yields %v, GetURLs returns %v", mode, collected, urls)
	}
	if yielded := slices.Collect(s.Errors()); !slices.Equal(yielded, errs) {
		t.Fatalf("%s mode: the iterator yields %v, GetErrors returns %v", mode, yielded, errs)
	}

	scribbleURLs(collected)
	scribbleURLs(s.GetRandomURLs(len(urls)))
	if got := s.GetURLs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mode: what was done to copies of the URLs changed the URLs of the instance: %v, want %v", mode, got, want)
	}
}

// FuzzParse exercises the full format-dispatch path — sitemap index, urlset,
// RSS, Atom and plain text — with untrusted content. Every location the parser
// hands back must satisfy the documented URL invariants in both tolerant and
// strict mode, in strict mode it must be on the host of the sitemap as well
// and hold no character that has no place in a URL where it stands, parsing must be
// deterministic, a byte order mark put before the
// document must change nothing, no text value may have whitespace around it,
// tolerant mode must percent-encode the URLs of the extensions that it can,
// strict mode must accept or report each of them,
// the call must fail exactly when the document cannot be parsed, a limit
// on the URLs must leave the first ones as they are, the sitemaps the call
// lists must be the ones it is to fetch, up to the limit on them, a date of
// a feed or of a sitemap index that cannot be parsed must cost only itself, and
// the URLs the call hands out as copies must share nothing with the ones it
// keeps.
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
		`<urlset><url><loc>https://example.com/a</loc><changefreq> Daily </changefreq></url><url><loc>https://example.com/b</loc><changefreq> </changefreq></url></urlset>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:news="http://www.google.com/schemas/sitemap-news/0.9" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1" xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://example.com/p</loc><image:image><image:loc> https://example.com/i.jpg </image:loc><image:title> t </image:title></image:image><news:news><news:publication><news:name> n </news:name><news:language> en </news:language></news:publication><news:title> t </news:title></news:news><video:video><video:thumbnail_loc> https://example.com/t.jpg </video:thumbnail_loc><video:title> t </video:title><video:restriction relationship=" allow "> IE GB </video:restriction><video:uploader info=" https://example.com/u "> u </video:uploader><video:tag> one </video:tag></video:video><xhtml:link rel=" alternate " hreflang=" de " href=" https://example.com/de "/></url></urlset>`,
		`<feed><entry><link rel=" alternate " href=" https://example.com/atom-padded "/></entry></feed>`,
		`<urlset><url><loc>https://EXAMPLE.com:443/a b?c=d e#f g</loc><changefreq>Daily</changefreq></url><url><loc>https://example.com:8443/other-port</loc><changefreq>sometimes</changefreq><priority>1.5</priority></url><url><loc>https://Example.COM:/empty-port?q=a #</loc><changefreq>never</changefreq></url></urlset>`,
		`<sitemapindex><sitemap><loc>https://example.com:443/site map.xml?v=a b</loc></sitemap><sitemap><loc>https://EXAMPLE.COM/upper.xml</loc></sitemap><sitemap><loc>https://www.example.com/other-host.xml</loc></sitemap></sitemapindex>`,
		"https://Example.com/text one\nhttps://example.com:443/text?q=two three #\nhttps://example.com:80/http-port\n",
		"https://example.com/fragment#a\x1db\nhttps://example.com/fragment#\x7f\n",
		"<urlset><url><loc>https://example.com/fragment#a&#9;b</loc></url><url><loc>https://example.com/fragment#a\nb</loc></url></urlset>",
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
		`<urlset><url><loc>//cdn.example.net/host</loc></url><url><loc>//</loc></url><url><loc>//?no=host</loc></url><url><loc>///no-host</loc></url><url><loc>//:8080/port-only</loc></url><url><loc>//user@/user-only</loc></url><url><loc>//::</loc></url><url><loc>//example.com:80:80/two-ports</loc></url></urlset>`,
		`<urlset><url><loc>https:///no-host</loc></url><url><loc>https://:8080/port-only</loc></url><url><loc>https://:</loc></url><url><loc>https:no-slash</loc></url><url><loc>https:/one-slash</loc></url><url><loc>https:</loc></url></urlset>`,
		`<sitemapindex><sitemap><loc>//</loc></sitemap><sitemap><loc>//::1/sitemap.xml</loc></sitemap><sitemap><loc>https://:443/sitemap.xml</loc></sitemap><sitemap><loc>https:sitemap.xml</loc></sitemap></sitemapindex>`,
		`<rss><channel><item><link>//</link></item><item><link>//::</link></item><item><link>https:///no-host</link></item></channel></rss>`,
		`<feed><entry><link href="//"/></entry><entry><link href="//:8080/port-only"/></entry><entry><link href="https://:"/></entry></feed>`,
		"https:///no-host\nhttps://:8080/port-only\nhttps://\nhttps://user@/user-only\nhttps://example.com/host\n",
		`<urlset><url><loc>https://bücher.example/a</loc></url><url><loc>https://BÜCHER.example:8443/b</loc></url><url><loc>https://xn--bcher-kva.example/c</loc></url><url><loc>https://b%C3%BCcher.example/d</loc></url><url><loc>//bücher.example/e</loc></url><url><loc>https://user@bücher.example:/f g</loc></url></urlset>`,
		"<urlset><url><loc>https://\uff45\uff58\uff41\uff4d\uff50\uff4c\uff45.com/a</loc></url><url><loc>https://example\u3002com/b</loc></url><url><loc>https://\uff25xample.com:443/c</loc></url><url><loc>https://exa\uff0fmple.com/d</loc></url><url><loc>https://example.com\u00ad/e</loc></url></urlset>",
		"<urlset><url><loc>https://bücher_x.example/a</loc></url><url><loc>https://-bücher.example/b</loc></url><url><loc>https://b%FFcher.example/c</loc></url><url><loc>https://b%EF%BF%BDcher.example/f</loc></url><url><loc>https://\u00ad/d</loc></url><url><loc>https://[fe80::1%25zöne]/e</loc></url></urlset>",
		`<sitemapindex><sitemap><loc>https://bücher.example/sitemap-1.xml</loc></sitemap><sitemap><loc>https://xn--bcher-kva.example/sitemap-1.xml</loc></sitemap><sitemap><loc>https://例え.テスト/sitemap-2.xml</loc></sitemap></sitemapindex>`,
		"https://bücher.example/text\nhttps://faß.example/text\nhttps://bu\u0308cher.example/text\n",
		`<urlset><url><loc>https://example.com/search?q=café au lait#é</loc></url><url><loc>?q=例え</loc></url><url><loc>https://example.com/caf%C3%A9?q=caf%C3%A9 é</loc></url></urlset>`,
		"https://example.com/text?q=café\nhttps://example.com/text?q=\xff\xfe\n",
		`<sitemapindex><sitemap><loc>https://example.com/sitemap.xml?city=münchen</loc></sitemap><sitemap><loc>https://example.com/sitemap.xml?city=m%C3%BCnchen</loc></sitemap></sitemapindex>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1" xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://example.com/p</loc><image:image><image:loc>https://cdn.example.net/a b.jpg?alt=café</image:loc><image:license>https://bücher.example/licence é</image:license></image:image><video:video><video:thumbnail_loc>https://BÜCHER.example:8443/t h.jpg</video:thumbnail_loc><video:content_loc>https://cdn.example.net/v.mp4#a&#9;b</video:content_loc><video:player_loc>https://cdn.example.net/player?v=é</video:player_loc><video:uploader info="https://example.com/u ser">u</video:uploader></video:video><xhtml:link rel="alternate" hreflang="de" href="https://example.com/de/über uns"/></url></urlset>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1" xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://example.com/p</loc><image:image><image:loc>/images/a b.jpg</image:loc><image:license>Creative Commons, café</image:license></image:image><image:image><image:loc>ftp://cdn.example.net/a b.jpg</image:loc></image:image><image:image><image:loc>https:///a b.jpg</image:loc></image:image><image:image><image:loc>https://cdn.example.net/100%-cotton shirt.jpg</image:loc></image:image><video:video><video:thumbnail_loc>//cdn.example.net/t h.jpg</video:thumbnail_loc><video:content_loc>data:video/mp4;base64,AAAA BBBB</video:content_loc><video:player_loc>https://cdn example.net/player</video:player_loc></video:video><xhtml:link rel="alternate" hreflang="de" href="https://bücher_x.example/de é"/></url></urlset>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"><url><loc>https://example.com/p</loc><image:image><image:loc>https://cdn.example.net/a b.jpg?alt=` + strings.Repeat("c", maxLocLength-37) + `</image:loc></image:image></url></urlset>`,
		"<urlset><url><loc>https://example.com/a|b</loc></url><url><loc>https://example.com/search?q={a}&amp;r=&lt;b&gt;</loc></url><url><loc>https://example.com/c#&quot;d&quot;</loc></url><url><loc>https://example.com/e\\f^g`h</loc></url><url><loc>https://example.com/i%7Cj?k[]=1</loc></url></urlset>",
		"https://example.com/text|one\nhttps://example.com/text?q=^two\nhttps://exa\"mple.com/three\nhttps://example.com/four#`\n",
		`<sitemapindex><sitemap><loc>https://example.com/sitemap|1.xml</loc></sitemap><sitemap><loc>https://example.com/sitemap.xml?tags=a|b</loc></sitemap></sitemapindex>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1" xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://example.com/p</loc><image:image><image:loc>https://cdn.example.net/a|b.jpg</image:loc></image:image><image:image><image:loc>https://cdn.example.net/c.jpg</image:loc><image:license>CC BY 4.0</image:license></image:image><video:video><video:thumbnail_loc>https://cdn.example.net/t.jpg</video:thumbnail_loc><video:content_loc>ftp://cdn.example.net/v.mp4</video:content_loc><video:player_loc>/player?v={id}</video:player_loc><video:uploader info="uploaders/u">u</video:uploader></video:video><video:video><video:thumbnail_loc>https://cdn.example.net/{t}.jpg</video:thumbnail_loc></video:video><xhtml:link rel="alternate" hreflang="de" href="https://example.com/de/^"/></url></urlset>`,
		`<rss><channel><pubDate>Sat, 13 Jan 2024 08:00:00 GMT</pubDate><item><link>https://example.com/a</link><pubDate>Mon, 15 Jan 2024 10:30:00 +0100</pubDate></item><item><link>https://example.com/b</link><pubDate> 5 Jan 24 10:30 est </pubDate></item><item><link>https://example.com/c</link><pubDate>2024-01-15T10:30:00Z</pubDate></item><item><link>https://example.com/c</link><pubDate/></item><item><link>https://www.example.com/d</link><pubDate>yesterday</pubDate></item><item><pubDate>no link</pubDate></item></channel></rss>`,
		`<feed><updated>2024-01-13T08:00:00Z</updated><entry><link href="https://example.com/a"/><updated>2024-01-15T10:30:00+01:00</updated></entry><entry><link href="https://example.com/b"/><updated> 2024-01 </updated></entry><entry><link href="https://example.com/c"/><updated>Mon, 15 Jan 2024 10:30:00 GMT</updated></entry><entry><link href="https://example.com/c"/><updated/></entry><entry><link href="https://www.example.com/d"/><updated>yesterday</updated></entry><entry><updated>no link</updated></entry></feed>`,
		`<sitemapindex><sitemap><loc>https://example.com/a.xml</loc><lastmod>2024-01-15T10:30:00+01:00</lastmod></sitemap><sitemap><loc>https://example.com/b.xml</loc><lastmod>2024-01-15 10:30:00</lastmod></sitemap><sitemap><loc>https://example.com/a.xml</loc><lastmod>2024-01-16</lastmod></sitemap><sitemap><loc>https://example.com/b.xml</loc><lastmod/></sitemap><sitemap><loc>https://www.example.com/c.xml</loc><lastmod>yesterday</lastmod></sitemap><sitemap><lastmod>no location</lastmod></sitemap></sitemapindex>`,
		`<rss xmlns:atom="http://www.w3.org/2005/Atom" xmlns:x="urn:x"><channel><item><link>https://example.com/a</link><atom:link rel="alternate" href="https://example.com/a.atom"/><x:pubDate>yesterday</x:pubDate></item><item><x:link>https://example.com/other</x:link><pubDate>Mon, 15 Jan 2024 10:30:00 GMT</pubDate></item></channel></rss>`,
		`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:x="urn:x"><entry><x:link href="https://example.com/other"/><link href="https://example.com/a"/><x:updated>never</x:updated></entry><a:entry xmlns:a="urn:a"><link href="https://example.com/b"/><a:link href="https://example.com/c"/></a:entry></feed>`,
		`<urlset><url><loc>https://example.com/a[1]</loc></url><url><loc>https://example.com/search?id[]=1&amp;q=100%</loc></url><url><loc>https://example.com/page#[top]</loc></url><url><loc>https://example.com/b%5B1%5D?q=100%25&amp;r=%4</loc></url><url><loc>https://exa]mple.com/c</loc></url></urlset>`,
		"https://example.com/text[1]\nhttps://example.com/text?q=%zz\nhttps://[2001:db8::1]/text[2]\nhttps://[fe80::1%25en]0]/text\n",
		`<sitemapindex><sitemap><loc>https://example.com/sitemap.xml?ids[]=1</loc></sitemap><sitemap><loc>https://example.com/sitemap.xml?zoom=100%</loc></sitemap></sitemapindex>`,
		`<urlset xmlns:image="http://www.google.com/schemas/sitemap-image/1.1" xmlns:video="http://www.google.com/schemas/sitemap-video/1.1" xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://example.com/p</loc><image:image><image:loc>https://[2001:db8::1]:8443/a.jpg</image:loc><image:license>https://[2001:db8::1]/licence[1]</image:license></image:image><image:image><image:loc>https://cdn.example.net/b.jpg?size[]=1</image:loc></image:image><video:video><video:thumbnail_loc>https://cdn.example.net/t.jpg?zoom=100%</video:thumbnail_loc></video:video><video:video><video:thumbnail_loc>https://cdn.example.net/t.jpg</video:thumbnail_loc><video:content_loc>https://cdn.example.net/v.mp4?q=%4</video:content_loc><video:view_count>-1</video:view_count></video:video><xhtml:link rel="alternate" hreflang="de" href="https://example.com/de?id]=1"/></url></urlset>`,
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
			if strict {
				assertOnSitemapHost(t, locs)
				assertNoExcludedCharacter(t, locs)
			} else {
				assertASCII(t, locs)
			}

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

			assertTextValues(t, mode, strict, content)
			if strict {
				assertStrictExtensionURLs(t, content)
			} else {
				assertExtensionURLs(t, content)
			}
			assertReturnedError(t, mode, strict, content)
			assertURLLimit(t, mode, strict, content)
			assertValidationErrorLimit(t, mode, strict, content)
			assertSitemapsListed(t, mode, strict, content)
			assertInvalidDates(t, mode, strict, content)
			assertResultCopies(t, mode, strict, content)
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

// escaped returns text as it is written in the content of an element, or in
// the value of an attribute, of an XML document.
func escaped(text string) string {
	var content strings.Builder
	_ = xml.EscapeText(&content, []byte(text))
	return content.String()
}

// FuzzFeedNamespaces checks that an element of another namespace changes
// nothing about what an item of a feed is read as: an item of an RSS feed and
// an entry of an Atom feed yield the page, the date and the errors that they
// yield without such an element, in both modes, whatever the element holds,
// whether it stands before or after the element of the feed that has its name,
// and whether or not the item has that element at all. That goes for the
// elements an item takes its date from when it lacks the one of its format: a
// <date> that is not the one of Dublin Core, and a <modified> and a
// <published> that are not those of Atom.
func FuzzFeedNamespaces(f *testing.F) {
	const rssDate, atomDate = "Mon, 15 Jan 2024 10:30:00 GMT", "2024-01-15T10:30:00Z"
	for places := range uint8(16) {
		f.Add("https://example.com/a", rssDate, "https://example.com/other", "Tue, 16 Jan 2024 10:30:00 GMT", places)
		f.Add("https://example.com/a", atomDate, "", "", places)
		f.Add("/a b", "yesterday", "https://example.com/other", atomDate, places)
	}
	f.Add("", "", "https://example.com/other", rssDate, uint8(0))
	f.Add(" https://example.com/\u00e9 ", " "+atomDate+" ", "ftp://example.com/<other>", "\"&'", uint8(3))

	f.Fuzz(func(t *testing.T, link, date, otherLink, otherDate string, places uint8) {
		link, date, otherLink, otherDate = escaped(link), escaped(date), escaped(otherLink), escaped(otherDate)
		// item returns the elements of an item: those of the feed, own, unless the item is to
		// have none, each with the one of another namespace before it or after it.
		item := func(ownLink, otherLinks, ownDate, otherDates string) (plain, mixed string) {
			if places&4 != 0 {
				ownLink = ""
			}
			if places&8 != 0 {
				ownDate = ""
			}
			mixedLinks, mixedDates := ownLink+otherLinks, ownDate+otherDates
			if places&1 != 0 {
				mixedLinks = otherLinks + ownLink
			}
			if places&2 != 0 {
				mixedDates = otherDates + ownDate
			}
			return ownLink + ownDate, mixedLinks + mixedDates
		}
		feeds := []struct {
			name, feed   string
			plain, mixed string
		}{
			{name: "RSS", feed: `<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:x="urn:example:x"><channel><item>%s</item></channel></rss>`},
			{name: "Atom", feed: `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:x="urn:example:x"><entry>%s</entry></feed>`},
		}
		feeds[0].plain, feeds[0].mixed = item(
			`<link>`+link+`</link>`,
			`<x:link>`+otherLink+`</x:link><atom:link rel="alternate" href="`+otherLink+`"/>`,
			`<pubDate>`+date+`</pubDate>`,
			`<x:pubDate>`+otherDate+`</x:pubDate><x:date>`+otherDate+`</x:date><date>`+otherDate+`</date><atom:published>`+otherDate+`</atom:published>`,
		)
		feeds[1].plain, feeds[1].mixed = item(
			`<link href="`+link+`"/>`,
			`<x:link href="`+otherLink+`"/><link xmlns="" rel="alternate" href="`+otherLink+`"/>`,
			`<updated>`+date+`</updated>`,
			`<x:updated>`+otherDate+`</x:updated><updated xmlns="">`+otherDate+`</updated>`+
				`<x:modified>`+otherDate+`</x:modified><modified xmlns="">`+otherDate+`</modified><x:published>`+otherDate+`</x:published><published xmlns="">`+otherDate+`</published>`,
		)

		for _, feed := range feeds {
			for _, strict := range []bool{false, true} {
				read := func(elements string) (urls, errs []string) {
					s := New().SetStrict(strict)
					s.parse(fuzzBaseURL, strings.Replace(feed.feed, "%s", elements, 1))
					return datedLocs(s), errorsOf(s)
				}
				urls, errs := read(feed.plain)
				mixedURLs, mixedErrs := read(feed.mixed)
				if !slicesEqual(mixedURLs, urls) || !slicesEqual(mixedErrs, errs) {
					t.Fatalf("%s, strict=%v: %q is read as %q with the errors %q, and %q as %q with the errors %q",
						feed.name, strict, feed.plain, urls, errs, feed.mixed, mixedURLs, mixedErrs)
				}
			}
		}
	})
}

// fuzzUnzipLimit is the decompressed-size limit FuzzUnzip uses to check that
// unzip enforces its cap. It is small enough that mutated seeds regularly land
// on both sides of it.
const fuzzUnzipLimit = 64

// FuzzStrictURL checks what strict mode takes for a URL of an extension against
// arbitrary text. A URL it accepts must satisfy the URL invariants and hold no
// character that has no place in a URL where it stands. What it rejects it must
// reject with a *ValidationError that names the URL, and the two that tell the
// place of a square bracket and of a percent sign must agree with what is
// decided here independently of them.
func FuzzStrictURL(f *testing.F) {
	seeds := []string{
		"https://example.com/a?b=c#d",
		"http://user:secret@example.com:8080/a%20b?c=%7C#%5B",
		"https://example.com/a[1]?b[]=1&c=[2]#[3]",
		"https://example.com/a]b",
		"https://exa]mple.com/",
		"https://[2001:db8::1]:8443/a",
		"https://[2001:db8::1]/a[1]",
		"https://[2001:db8::1]/]",
		"https://[fe80::1%25eth0]/a",
		"https://[fe80::1%25en]0]/",
		"https://user@[::1]?b=[",
		"https://example.com/search?q=100%",
		"https://example.com/search?q=%4",
		"https://example.com/search?q=%4G&r=%41",
		"https://example.com/search?q=100%25#100%",
		"https://example.com/100%?q=1",
		"https://example.com/a|b[c]%",
		"https://example.com/caf\u00e9?q=%\u00e9",
		"/a[b]?c=%",
		"ftp://example.com/a[b]",
		"https:///a[b]",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, rawURL string) {
		err := extensionURLError(rawURL, true)
		if err == nil {
			assertLocInvariants(t, "strict", []string{rawURL})
			assertNoExcludedCharacter(t, []string{rawURL})
			return
		}
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.URL != rawURL {
			t.Fatalf("strict mode: %q is rejected with %#v, which is no *ValidationError that names it", rawURL, err)
		}

		// A URL is turned down over a square bracket exactly if it has one out of
		// place, and over its query exactly if a percent sign in it begins no
		// percent-encoding. What else is wrong with a URL is reported first.
		parsed, parseErr := neturl.Parse(rawURL)
		if parseErr != nil {
			return
		}
		message := validationErr.Err.Error()
		if strings.Contains(message, "the character '['") || strings.Contains(message, "the character ']'") {
			if bracketsInPlace(rawURL) {
				t.Fatalf("strict mode: %q is rejected with %q, but its square brackets are around an IPv6 address", rawURL, message)
			}
		}
		if strings.Contains(message, "in the query") {
			if queryEscaped(rawURL) {
				t.Fatalf("strict mode: %q is rejected with %q, but its query is percent-encoded", rawURL, message)
			}
			var escapeErr neturl.EscapeError
			if _, unescapeErr := neturl.QueryUnescape(parsed.RawQuery); !errors.As(err, &escapeErr) || escapeErr != unescapeErr {
				t.Fatalf("strict mode: %q is rejected with %q, net/url has %v for its query", rawURL, message, unescapeErr)
			}
		}
	})
}

// FuzzUnzip checks that gzip handling survives arbitrary bytes, that any data
// the package compresses can be read back unchanged, and that the
// decompressed-size limit is enforced.
func FuzzUnzip(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("not gzip"))
	f.Add([]byte("\x1f\x8b\x08"))            // gzip magic, truncated
	f.Add([]byte("\x1f\x8b\x08\x00garbage")) // gzip magic, corrupt body
	// Several members, with and without something after them, and a member cut short.
	members := append(gzipByte("https://example.com/one\n"), gzipByte("https://example.com/two\n")...)
	f.Add(members)
	f.Add(append(members[:len(members):len(members)], '\n'))
	f.Add(append(members[:len(members):len(members)], gzipByte("")...))
	f.Add(members[:len(members)-5])
	f.Add(append(members[:len(members):len(members)], "\x1f\x8b\x08"...))
	addFileSeeds(f, "*.gz", func(f *testing.F, data []byte) { f.Add(data) })

	f.Fuzz(func(t *testing.T, data []byte) {
		// Arbitrary input: unzip must fail cleanly rather than panic, must never
		// hand back more than the limit allows, and no data together with an error.
		out, err := unzip(string(data), fuzzUnzipLimit)
		if err == nil && len(out) > fuzzUnzipLimit {
			t.Fatalf("unzip returned %d bytes, limit is %d", len(out), fuzzUnzipLimit)
		}
		if err != nil && out != "" {
			t.Fatalf("unzip returned %d bytes together with an error: %v", len(out), err)
		}
		// The limit is what the error is about if, and only if, content that can
		// be read to its end is over it.
		if full, fullErr := unzip(string(data), defaultMaxResponseSize); fullErr == nil {
			if over := len(full) > fuzzUnzipLimit; errors.Is(err, ErrMaxResponseSize) != over {
				t.Fatalf("unzip returned %v for %d bytes with a limit of %d", err, len(full), fuzzUnzipLimit)
			}
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

		out, err = unzip(buf.String(), defaultMaxResponseSize)
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
		} else if !errors.Is(err, ErrMaxResponseSize) {
			t.Fatalf("unzip rejected a %d-byte payload over the %d-byte limit with an error that is not about the limit: %v", len(data), fuzzUnzipLimit, err)
		}

		// Members: data compressed in two members, cut wherever its length puts
		// the cut, must come back as one, whether or not a newline follows them.
		cut := len(data) / 3
		members := string(gzipByte(string(data[:cut]))) + string(gzipByte(string(data[cut:])))
		for _, trailing := range []string{"", "\n"} {
			out, err = unzip(members+trailing, defaultMaxResponseSize)
			if err != nil {
				t.Fatalf("unzip rejected two members this package compressed: %v", err)
			}
			if out != string(data) {
				t.Fatalf("two members came back altered: got %d bytes, want %d", len(out), len(data))
			}
		}
		// A member that is cut short must not pass for the end of the content.
		if _, err = unzip(members[:len(members)-1], defaultMaxResponseSize); err == nil {
			t.Fatal("unzip accepted content whose last member is cut short")
		} else if errors.Is(err, ErrMaxResponseSize) {
			t.Fatalf("unzip took a member that is cut short for content over the limit: %v", err)
		}

		// checkAndUnzipContent must agree with unzip on well-formed input.
		s := New()
		if got, ok := s.checkAndUnzipContent(fuzzBaseURL, buf.String()); !ok || got != string(data) {
			t.Fatalf("checkAndUnzipContent returned %d bytes and %v, want %d", len(got), ok, len(data))
		}
	})
}

// pieces is a source that hands out its content in pieces of the given sizes, one
// after the other and over again, whatever it is asked for.
type pieces struct {
	content []byte
	sizes   []byte
	reads   int
	// read is the number of bytes that were read from the source.
	read int
}

func (p *pieces) Read(b []byte) (int, error) {
	if p.read == len(p.content) {
		return 0, io.EOF
	}
	n := len(b)
	if len(p.sizes) > 0 {
		n = min(n, int(p.sizes[p.reads%len(p.sizes)])+1)
	}
	p.reads++
	n = copy(b[:n], p.content[p.read:])
	p.read += n
	return n, nil
}

// FuzzReadAtMost reads content that comes in pieces of arbitrary sizes, with an
// arbitrary limit and an arbitrary size the content is said to have. The content
// must come back as it is if it fits the limit and not at all if it does not,
// the source must be read one byte past the limit at most and not after its end,
// and none of that may depend on the size the content is said to have.
func FuzzReadAtMost(f *testing.F) {
	const content = "0123456789"
	for _, limit := range []int64{-1, 0, 4, 9, 10, 11, math.MaxInt64} {
		for _, size := range []int64{math.MinInt64, -1, 0, 1, 9, 10, 11, 1000, math.MaxInt64} {
			f.Add([]byte(content), limit, size, []byte(nil))
			f.Add([]byte(content), limit, size, []byte{0, 2})
		}
	}
	f.Add([]byte(nil), int64(0), int64(10), []byte{0})
	// Content of more than two of the buffers it is read into.
	long := bytes.Repeat([]byte(content), 7000)
	f.Add(long, int64(len(long)), int64(-1), []byte{255, 0, 100})
	f.Add(long, int64(len(long)), int64(len(long)), []byte(nil))
	f.Add(long, int64(len(long))-1, int64(len(long)/2), []byte(nil))
	f.Add(long, int64(math.MaxInt64), int64(len(long)/3), []byte{255})
	f.Add(long, int64(math.MaxInt64), int64(2*len(long)), []byte{255})

	f.Fuzz(func(t *testing.T, data []byte, limit, size int64, sizes []byte) {
		// A string is allocated for a size that is believed. A megabyte for every
		// input is as much as a run can spare.
		if size > 1024*1024 && size <= limit && size <= presizeLimit {
			t.Skip()
		}
		source := &pieces{content: data, sizes: sizes}

		// A source that is read after it reported its end fails the read.
		got, exceeded, err := readAtMost(&endsOnce{source: source}, limit, size)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := int64(len(data)) > limit; exceeded != want {
			t.Fatalf("%d bytes with a limit of %d: exceeded is %t", len(data), limit, exceeded)
		}
		if exceeded {
			if got != "" {
				t.Fatalf("%d bytes were returned of content that exceeds the limit", len(got))
			}
			if want := max(limit+1, 0); int64(source.read) != want {
				t.Fatalf("%d bytes were read with a limit of %d", source.read, limit)
			}
		} else if got != string(data) {
			t.Fatalf("content came back altered: got %d bytes, want %d", len(got), len(data))
		}

		// The size is not to be relied on, and changes nothing.
		unsized, unsizedExceeded, err := readAtMost(&pieces{content: data, sizes: sizes}, limit, -1)
		if err != nil {
			t.Fatalf("unexpected error without a size: %v", err)
		}
		if unsized != got || unsizedExceeded != exceeded {
			t.Fatalf("said to be %d bytes: %d bytes and %t, without a size: %d bytes and %t", size, len(got), exceeded, len(unsized), unsizedExceeded)
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
		"Sitemap: https://example.com/site map.xml?v=a b\nSitemap: https://EXAMPLE.com:8443/other-port.xml\n",
		"Sitemap: //\nSitemap: ///no-host.xml\nSitemap: //:8080/port-only.xml\nSitemap: //::\nSitemap: //example.com:80:80/two-ports.xml\n",
		"Sitemap: https:///no-host.xml\nSitemap: https://:8080/port-only.xml\nSitemap: https:no-slash.xml\nSitemap: https://user@/user-only.xml\n",
		"Sitemap: https://bücher.example/sitemap.xml\nSitemap: https://xn--bcher-kva.example:8443/sitemap.xml\nSitemap: //BÜCHER.example/sitemap.xml\nSitemap: https://bücher_x.example/sitemap.xml\n",
		"Sitemap: https://example.com/sitemap.xml?city=münchen\nSitemap: /sitemap.xml?city=m%C3%BCnchen&q=café au lait\n",
		"Sitemap: https://example.com/sitemap|1.xml\nSitemap: https://example.com/sitemap.xml?tags={a}^\nSitemap: https://cdn.example.net/<sitemap>.xml\n",
		"Sitemap: https://example.com/sitemap.xml?ids[]=1\nSitemap: https://cdn.example.net/sitemap.xml?zoom=100%\nSitemap: https://[2001:db8::1]/sitemap.xml\nSitemap: https://[2001:db8::1]/sitemap[1].xml\n",
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
			if strict {
				assertNoExcludedCharacter(t, locs)
			} else {
				assertASCII(t, locs)
			}

			if again := robotsTXTLocs(strict, content); !slicesEqual(locs, again) {
				t.Fatalf("%s mode: result is not deterministic: %q vs %q", mode, locs, again)
			}

			// With no pattern set with SetFollow, the sitemaps that are listed
			// are the ones that are fetched, each of them once.
			if listed, want := robotsTXTListed(strict, content), unique(locs); !slicesEqual(listed, want) {
				t.Fatalf("%s mode: the sitemaps listed are %q, the ones to fetch are %q", mode, listed, want)
			}
		}
	})
}

// FuzzNamesRobotsTXT checks what tells a robots.txt from a sitemap, the URL
// passed to Parse, against arbitrary URLs. A URL that is taken for that of a
// robots.txt can be parsed and has a path that ends in "robots.txt", and
// neither a query, nor a fragment, nor the case of its letters makes a URL
// one or keeps it from being one.
func FuzzNamesRobotsTXT(f *testing.F) {
	seeds := []string{
		"https://example.com/robots.txt",
		"https://example.com/robots.txt?v=2#x",
		"https://example.com/ROBOTS.TXT",
		"https://example.com/sub/Robots.txt",
		"https://example.com/robots%2Etxt",
		"https://example.com/a%2Frobots.txt",
		"https://example.com/robots.txt/",
		"https://example.com/sitemap.xml?from=/robots.txt",
		"https://example.com/sitemap.xml#/robots.txt",
		"https://robots.txt",
		"https://example.com/robot\xc5\xbf.txt",
		"https://example.com/%zz/robots.txt",
		"/robots.txt",
		"robots.txt",
		"mailto:robots.txt",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, url string) {
		got := namesRobotsTXT(url)
		if got {
			parsed, err := neturl.Parse(url)
			if err != nil {
				t.Fatalf("namesRobotsTXT(%q) is true for a URL that cannot be parsed: %v", url, err)
			}
			if path := strings.ToLower(parsed.Path); path != "robots.txt" && !strings.HasSuffix(path, "/robots.txt") {
				t.Fatalf("namesRobotsTXT(%q) is true for the path %q", url, parsed.Path)
			}
		}

		// The path is what precedes the query and the fragment.
		path, _, _ := strings.Cut(url, "#")
		path, _, _ = strings.Cut(path, "?")
		want := namesRobotsTXT(path)
		for _, suffix := range []string{"?v=2", "#x", "?v=2#x", "?from=/sitemap.xml", "#/sitemap.xml"} {
			if got := namesRobotsTXT(path + suffix); got != want {
				t.Fatalf("namesRobotsTXT(%q) = %v, but namesRobotsTXT(%q) = %v", path+suffix, got, path, want)
			}
		}
		upper := []byte(path)
		for i, c := range upper {
			if 'a' <= c && c <= 'z' {
				upper[i] = c - 'a' + 'A'
			}
		}
		if got := namesRobotsTXT(string(upper)); got != want {
			t.Fatalf("namesRobotsTXT(%q) = %v, but namesRobotsTXT(%q) = %v", upper, got, path, want)
		}
	})
}

// FuzzParseRFC822Time checks the parser of the date of an RSS item against
// arbitrary text. A text that is read as a date is one: written out in the
// format of the RFC, it is read as the same time again. Neither the whitespace
// around a text nor the case of its letters makes it a date or keeps it from
// being one, and a text that is none yields no date.
func FuzzParseRFC822Time(f *testing.F) {
	seeds := []string{
		"Mon, 15 Jan 2024 10:30:45 GMT",
		"Mon, 15 Jan 2024 10:30 +0100",
		"15 Jan 24 10:30:45 est",
		"5 Jan 69 10:30 Z",
		"Mon, 15 Jan 2024 10:30:45.5 -0830",
		"Mon, 31 Dec 9999 23:59:59 -0100",
		"Sat, 1 Jan 0000 00:00:00 +0100",
		"Mon, 15 Jan 2024 10:30:45 CET",
		"Mon, 15 Jan 2024 10:30:45 e\xc5\xbft",
		"Mon,\t15 Jan  2024\n10:30:45 PDT",
		"2024-01-15T10:30:45Z",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		got, err := parseRFC822Time(text)
		if err != nil && !got.IsZero() {
			t.Fatalf("parseRFC822Time(%q) fails with %v and returns the date %v", text, err, got.Time)
		}
		if err == nil {
			written := got.Format("Mon, 02 Jan 2006 15:04:05 -0700")
			again, err := parseRFC822Time(written)
			if err != nil {
				t.Fatalf("parseRFC822Time(%q) = %v, but %q, the same time written out, is not read: %v", text, got.Time, written, err)
			}
			if again.Unix() != got.Unix() {
				t.Fatalf("parseRFC822Time(%q) = %v, but %q, the same time written out, is read as %v", text, got.Time, written, again.Time)
			}
		}

		upper := []byte(text)
		for i, c := range upper {
			if 'a' <= c && c <= 'z' {
				upper[i] = c - 'a' + 'A'
			}
		}
		for _, other := range []string{" \t" + text + "\n", string(upper)} {
			same, otherErr := parseRFC822Time(other)
			if (otherErr == nil) != (err == nil) {
				t.Fatalf("parseRFC822Time(%q) fails with %v, but parseRFC822Time(%q) with %v", text, err, other, otherErr)
			}
			if !same.Equal(got.Time) {
				t.Fatalf("parseRFC822Time(%q) = %v, but parseRFC822Time(%q) = %v", text, got.Time, other, same.Time)
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

// robotsTXTListed returns the sitemaps that are listed of the ones the given
// robots.txt content names.
func robotsTXTListed(strict bool, content string) []string {
	s := New().SetStrict(strict)
	s.parseRobotsTXT(content)
	s.robotsTXTSitemapLocations(fuzzRobotsTXTURL)

	return listedLocs(s)
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

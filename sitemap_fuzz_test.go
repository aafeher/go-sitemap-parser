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
// characters that have no place in a URL.
func assertNoExcludedCharacter(t *testing.T, locs []string) {
	t.Helper()

	for _, loc := range locs {
		if i := strings.IndexAny(loc, "\"<>\\^`{|}"); i >= 0 {
			t.Fatalf("strict mode: accepted URL with the character %q in it: %q", loc[i], loc)
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
			!strings.ContainsAny(url, "\"<>\\^`{|}")
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

// FuzzParse exercises the full format-dispatch path — sitemap index, urlset,
// RSS, Atom and plain text — with untrusted content. Every location the parser
// hands back must satisfy the documented URL invariants in both tolerant and
// strict mode, in strict mode it must be on the host of the sitemap as well
// and hold no character that has no place in a URL, parsing must be
// deterministic, a byte order mark put before the
// document must change nothing, no text value may have whitespace around it,
// tolerant mode must percent-encode the URLs of the extensions that it can,
// strict mode must accept or report each of them,
// the call must fail exactly when the document cannot be parsed, and a limit
// on the URLs must leave the first ones as they are.
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
		}

		// checkAndUnzipContent must agree with unzip on well-formed input.
		s := New()
		if got, ok := s.checkAndUnzipContent(fuzzBaseURL, buf.String()); !ok || got != string(data) {
			t.Fatalf("checkAndUnzipContent returned %d bytes and %v, want %d", len(got), ok, len(data))
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

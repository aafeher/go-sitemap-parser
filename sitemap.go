package sitemap

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"
)

type (

	// S is a structure that holds various data related to processing URLs.
	// It contains a cfg field of type `config`, which stores configuration settings.
	// The mainURL field of type string represents the main URL being processed.
	// The robotsTxtSitemapURLs field is a slice of strings that contains the URLs present in the robots.txt file's sitemap directive.
	// The sitemapLocations field is a slice of strings that represents the locations of the sitemap files.
	// The urls field is a slice of URL structs that stores the URLs to be processed.
	// The errs field is a slice of errors that holds any encountered errors during processing.
	S struct {
		parseMu              sync.Mutex
		mu                   sync.Mutex
		cfg                  config
		mainURL              string
		robotsTxtSitemapURLs []string
		sitemapLocations     []string
		// fetchedURLs tracks sitemap URLs already fetched in the current Parse
		// call to prevent duplicate HTTP requests when the same URL is referenced
		// from multiple sitemap indexes or robots.txt directives.
		fetchedURLs map[string]struct{}
		urls        []URL
		errs        []error
		// sem is a per-Parse-call semaphore that bounds the number of
		// sitemaps being fetched or parsed at the same time when
		// cfg.maxConcurrency > 0.
		// It is created at the start of ParseContext and is nil when
		// concurrency is unlimited.
		sem chan struct{}
		// limits holds the limits of the current Parse call on the sitemaps it
		// fetches and the URLs it collects, and whether they were reached.
		limits callLimits
	}

	// callLimits are the limits of a Parse call on what it fetches and collects. They are the
	// values set with SetMaxSitemaps and SetMaxURLs as they stand when the call starts, so
	// that one limit applies to the whole call; 0 means no limit.
	// sitemapsLeftOut and urlsLeftOut tell that a limit was not only reached, but kept the
	// call from fetching a sitemap or from collecting a URL: the results are not complete.
	callLimits struct {
		maxSitemaps     int
		maxURLs         int
		sitemapsLeftOut bool
		urlsLeftOut     bool
	}

	// config is a structure that holds configuration settings.
	// It contains a userAgent field of type string, which represents the User-Agent header value for HTTP requests.
	// The fetchTimeout field of type uint16 represents the timeout value (in seconds) for fetching data.
	// The multiThread field of type bool determines whether to use multi-threading for fetching URLs.
	// The follow field is a slice of strings that contains regular expressions to match URLs to follow.
	// The followRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the follow field.
	// The rules field is a slice of strings that contains regular expressions to match URLs to include.
	// The rulesRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the rules field.
	config struct {
		userAgent       string
		fetchTimeout    uint16
		maxResponseSize int64
		maxDepth        int
		maxConcurrency  int
		maxSitemaps     int
		maxURLs         int
		multiThread     bool
		strict          bool
		httpClient      *http.Client
		follow          []string
		followRegexes   []*regexp.Regexp
		rules           []string
		rulesRegexes    []*regexp.Regexp
	}

	// sitemapIndex is a structure of <sitemapindex>
	sitemapIndex struct {
		XMLName xml.Name `xml:"sitemapindex"`
		Sitemap []struct {
			Loc     string  `xml:"loc"`
			LastMod *string `xml:"lastmod"`
		} `xml:"sitemap"`
	}

	// urlEntry is the form in which a <url> element is decoded: a URL whose elements holding a
	// number or a date are read as text first. Decoded straight into its type, the first such
	// value that cannot be parsed would fail the whole document; read as text, it costs only
	// itself. The fields below take these elements over from the fields of the embedded
	// structure that name the same element, and convert fills those in from the text.
	urlEntry struct {
		URL
		LastModText  *string      `xml:"lastmod"`
		PriorityText *string      `xml:"priority"`
		NewsEntry    *newsEntry   `xml:"http://www.google.com/schemas/sitemap-news/0.9 news"`
		VideoEntries []videoEntry `xml:"http://www.google.com/schemas/sitemap-video/1.1 video"`
		// invalid lists the elements of the entry whose content could not be parsed.
		invalid []invalidValue
		// invalidOwn reports whether an element of the <url> itself, rather than of one of its
		// extensions, is among them.
		invalidOwn bool
	}

	// newsEntry is the form in which a <news:news> element is decoded, see urlEntry.
	newsEntry struct {
		News
		PublicationDateText *string `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication_date"`
	}

	// videoEntry is the form in which a <video:video> element is decoded, see urlEntry.
	videoEntry struct {
		Video
		DurationText        *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 duration"`
		ExpirationDateText  *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 expiration_date"`
		RatingText          *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 rating"`
		ViewCountText       *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 view_count"`
		PublicationDateText *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 publication_date"`
	}

	// invalidValue is an element whose content could not be parsed.
	invalidValue struct {
		// element names the element the way error messages do, e.g. "<lastmod>".
		element string
		text    string
	}

	// rss is a structure of <rss> for RSS 2.0 feeds.
	rss struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Item []struct {
				Link string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
	}

	// atom is a structure of <feed> for Atom 1.0 feeds.
	atom struct {
		XMLName xml.Name `xml:"feed"`
		Entry   []struct {
			Link []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}

	// Image is a structure of <image:image> in <url>, per the Google Image Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/image-sitemaps
	Image struct {
		Loc         string `xml:"http://www.google.com/schemas/sitemap-image/1.1 loc"`
		Title       string `xml:"http://www.google.com/schemas/sitemap-image/1.1 title"`
		Caption     string `xml:"http://www.google.com/schemas/sitemap-image/1.1 caption"`
		GeoLocation string `xml:"http://www.google.com/schemas/sitemap-image/1.1 geo_location"`
		License     string `xml:"http://www.google.com/schemas/sitemap-image/1.1 license"`
	}

	// VideoRestriction is a structure of <video:restriction> in <video:video>.
	// It captures the element text and the required "relationship" attribute.
	VideoRestriction struct {
		Relationship string `xml:"relationship,attr"`
		Value        string `xml:",chardata"`
	}

	// VideoPlatform is a structure of <video:platform> in <video:video>.
	// It captures the element text and the required "relationship" attribute.
	VideoPlatform struct {
		Relationship string `xml:"relationship,attr"`
		Value        string `xml:",chardata"`
	}

	// VideoUploader is a structure of <video:uploader> in <video:video>.
	// It captures the uploader name and the optional "info" URL attribute.
	VideoUploader struct {
		Info  string `xml:"info,attr"`
		Value string `xml:",chardata"`
	}

	// Video is a structure of <video:video> in <url>, per the Google Video Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/video-sitemaps
	Video struct {
		ThumbnailLoc         string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 thumbnail_loc"`
		Title                string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 title"`
		Description          string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 description"`
		ContentLoc           string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 content_loc"`
		PlayerLoc            string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 player_loc"`
		Duration             *int              `xml:"http://www.google.com/schemas/sitemap-video/1.1 duration"`
		ExpirationDate       *LastModTime      `xml:"http://www.google.com/schemas/sitemap-video/1.1 expiration_date"`
		Rating               *float32          `xml:"http://www.google.com/schemas/sitemap-video/1.1 rating"`
		ViewCount            *int              `xml:"http://www.google.com/schemas/sitemap-video/1.1 view_count"`
		PublicationDate      *LastModTime      `xml:"http://www.google.com/schemas/sitemap-video/1.1 publication_date"`
		FamilyFriendly       string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 family_friendly"`
		Restriction          *VideoRestriction `xml:"http://www.google.com/schemas/sitemap-video/1.1 restriction"`
		Platform             *VideoPlatform    `xml:"http://www.google.com/schemas/sitemap-video/1.1 platform"`
		RequiresSubscription string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 requires_subscription"`
		Uploader             *VideoUploader    `xml:"http://www.google.com/schemas/sitemap-video/1.1 uploader"`
		Live                 string            `xml:"http://www.google.com/schemas/sitemap-video/1.1 live"`
		Tags                 []string          `xml:"http://www.google.com/schemas/sitemap-video/1.1 tag"`
	}

	// NewsPublication is a structure of <news:publication> in <news:news>.
	NewsPublication struct {
		Name     string `xml:"http://www.google.com/schemas/sitemap-news/0.9 name"`
		Language string `xml:"http://www.google.com/schemas/sitemap-news/0.9 language"`
	}

	// News is a structure of <news:news> in <url>, per the Google News Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/news-sitemap
	News struct {
		Publication     NewsPublication `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication"`
		PublicationDate *LastModTime    `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication_date"`
		Title           string          `xml:"http://www.google.com/schemas/sitemap-news/0.9 title"`
	}

	// AlternateLink represents an alternate version of a page (hreflang)
	// per the XHTML standard used in sitemaps.
	// Reference: https://developers.google.com/search/docs/specialty/international/localized-versions#sitemap
	AlternateLink struct {
		Rel      string `xml:"rel,attr"`
		Hreflang string `xml:"hreflang,attr"`
		Href     string `xml:"href,attr"`
	}

	// URL is a structure of <url> in <urlset>
	URL struct {
		Loc        string          `xml:"loc"`
		LastMod    *LastModTime    `xml:"lastmod"`
		ChangeFreq *URLChangeFreq  `xml:"changefreq"`
		Priority   *float32        `xml:"priority"`
		Images     []Image         `xml:"http://www.google.com/schemas/sitemap-image/1.1 image"`
		News       *News           `xml:"http://www.google.com/schemas/sitemap-news/0.9 news"`
		Videos     []Video         `xml:"http://www.google.com/schemas/sitemap-video/1.1 video"`
		Hreflangs  []AlternateLink `xml:"http://www.w3.org/1999/xhtml link"`
	}

	LastModTime struct {
		time.Time
	}

	// URLChangeFreq represents the frequency at which a URL should be crawled and indexed.
	// Possible values are: "always", "hourly", "daily", "weekly", "monthly", "yearly", and "never".
	URLChangeFreq string
)

const (
	// ChangeFreqAlways represents the "always" value for URLChangeFreq.
	ChangeFreqAlways URLChangeFreq = "always"

	// ChangeFreqHourly represents the "hourly" value for URLChangeFreq.
	ChangeFreqHourly URLChangeFreq = "hourly"

	// ChangeFreqDaily represents the "daily" value for URLChangeFreq.
	ChangeFreqDaily URLChangeFreq = "daily"

	// ChangeFreqWeekly represents the "weekly" value for URLChangeFreq.
	ChangeFreqWeekly URLChangeFreq = "weekly"

	// ChangeFreqMonthly represents the "monthly" value for URLChangeFreq.
	ChangeFreqMonthly URLChangeFreq = "monthly"

	// ChangeFreqYearly represents the "yearly" value for URLChangeFreq.
	ChangeFreqYearly URLChangeFreq = "yearly"

	// ChangeFreqNever represents the "never" value for URLChangeFreq.
	ChangeFreqNever URLChangeFreq = "never"
)

// New creates a new instance of the S structure.
// It initializes the structure with default configuration values
// and returns a pointer to the created instance.
func New() *S {
	s := &S{}

	s.setConfigDefaults()

	return s
}

// setConfigDefaults sets the default configuration values for the S structure.
// It initializes the cfg field with the default values for userAgent and fetchTimeout.
// The default userAgent is "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
// the default fetchTimeout is 3 seconds and multi-thread flag is true.
// The follow and rules fields are empty slices.
// This method does not return any value.
func (s *S) setConfigDefaults() {
	s.cfg = config{
		userAgent:       "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
		fetchTimeout:    3,
		maxResponseSize: defaultMaxResponseSize,
		maxDepth:        10,
		maxConcurrency:  defaultMaxConcurrency,
		maxSitemaps:     defaultMaxSitemaps,
		maxURLs:         defaultMaxURLs,
		multiThread:     true,
		follow:          []string{},
		rules:           []string{},
	}
}

// SetUserAgent sets the user agent for the Sitemap Parser.
// The user agent is used for making HTTP requests when parsing and fetching URLs.
// It should be a string representing the user agent header value.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetUserAgent(userAgent string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.userAgent = userAgent

	return s
}

// SetFetchTimeout sets the fetch timeout for the Sitemap Parser.
// The fetch timeout determines how long the parser will wait for an HTTP request to complete.
// It should be specified in seconds as a uint16 value and must be greater than 0.
// Invalid values are ignored and a *ConfigError is recorded; a later call with a valid
// value clears it.
// Note: when a custom HTTP client is set via SetHTTPClient, this value has no effect.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFetchTimeout(fetchTimeout uint16) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("fetchTimeout")
	if fetchTimeout == 0 {
		s.errs = append(s.errs, &ConfigError{Field: "fetchTimeout", Err: fmt.Errorf("must be greater than 0, got %d", fetchTimeout)})
		return s
	}
	s.cfg.fetchTimeout = fetchTimeout

	return s
}

// SetMultiThread sets the multi-threading for the Sitemap Parser.
// The multi-threading flag determines whether the parser should fetch and parse the sitemaps concurrently using goroutines.
// When it is off, the sitemaps are fetched one at a time and in the order they are listed, those
// a robots.txt names as well as those of a sitemap index, and SetMaxConcurrency has no effect.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMultiThread(multiThread bool) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.multiThread = multiThread

	return s
}

// SetMaxResponseSize sets the maximum allowed HTTP response size in bytes.
// Responses exceeding this limit are rejected with a *NetworkError.
// The same limit caps the decompressed size of gzip-compressed content, whether it was
// fetched or supplied through the urlContent argument of Parse; content that expands
// beyond it is rejected with a *ParseError.
// The default is 50 MB, matching the sitemaps.org protocol limit.
// The value must be greater than 0; invalid values are ignored and a *ConfigError is recorded.
// A later call with a valid value clears it.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxResponseSize(maxResponseSize int64) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxResponseSize")
	if maxResponseSize <= 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxResponseSize", Err: fmt.Errorf("must be greater than 0, got %d", maxResponseSize)})
		return s
	}
	s.cfg.maxResponseSize = maxResponseSize

	return s
}

// SetMaxDepth sets the maximum recursion depth for following sitemap indexes.
// A sitemap index may reference other sitemap indexes; this limits how many levels deep
// the parser will follow. The default is 10.
// The value must be greater than 0; invalid values are ignored and a *ConfigError is recorded.
// A later call with a valid value clears it.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxDepth(maxDepth int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxDepth")
	if maxDepth <= 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxDepth", Err: fmt.Errorf("must be greater than 0, got %d", maxDepth)})
		return s
	}
	s.cfg.maxDepth = maxDepth

	return s
}

// SetMaxConcurrency sets the maximum number of sitemaps that are worked on at the
// same time when multi-threaded parsing is enabled. The default is 16. A value of 0 means
// unlimited concurrency. A positive value caps the number of sitemaps that are being
// fetched or parsed at any one time across the recursive sitemap-index traversal:
// a sitemap counts from the moment its request is sent until its content is parsed.
// That bounds the connections in use and the documents held in memory, which is
// recommended for very large sitemap indexes to avoid goroutine and connection blow-up.
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxConcurrency(maxConcurrency int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxConcurrency")
	if maxConcurrency < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxConcurrency", Err: fmt.Errorf("must be >= 0, got %d", maxConcurrency)})
		return s
	}
	s.cfg.maxConcurrency = maxConcurrency

	return s
}

// SetMaxSitemaps sets the maximum number of sitemaps a Parse or ParseContext call fetches.
// The default is 50,000, which is as many sitemaps as a sitemap index may list according to
// the sitemaps.org protocol. A value of 0 means no limit.
//
// The sitemaps a robots.txt or a sitemap index lists count, on every level together: each one
// the call requests, whether or not the request succeeds. A sitemap that is listed more than
// once is requested and counted once. The document the call is made for does not count.
//
// Once the limit is reached, the sitemaps that are left are not fetched, and a *ParseError
// naming the URL the call was made for is recorded, once for the call. The call does not
// fail: what the sitemaps fetched until then yielded is returned. With multi-threading off,
// the sitemaps are fetched in the order they are listed, so it is the first ones that are
// fetched; with multi-threading on, which ones are depends on how fast the server answers.
// Nothing is recorded if no sitemap had to be left out.
//
// The limit bounds the number of requests a document can make the parser send. Without it, a
// server can keep a call busy for as long as it likes by listing sitemaps that list ever
// further sitemaps.
//
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it. The value that applies to a call is the one set when the call starts.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxSitemaps(maxSitemaps int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxSitemaps")
	if maxSitemaps < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxSitemaps", Err: fmt.Errorf("must be >= 0, got %d", maxSitemaps)})
		return s
	}
	s.cfg.maxSitemaps = maxSitemaps

	return s
}

// SetMaxURLs sets the maximum number of URLs a Parse or ParseContext call collects.
// The default is 10,000,000. A value of 0 means no limit.
//
// The URLs that GetURLs returns count. An entry that is not valid does not, and neither does
// a URL that the patterns set with SetRules leave out.
//
// Once the limit is reached, the call stops collecting: what is left of the document that
// reached the limit is left out unread, so it adds no errors either, the sitemaps that are
// left are not fetched, and a *ParseError naming the URL the call was made for is recorded,
// once for the call. The call does not fail: the URLs collected until then are returned. With
// multi-threading off, they are the first ones in the order the sitemaps list them; with
// multi-threading on, which ones they are depends on how fast the server answers.
// Nothing is recorded if nothing had to be left out.
//
// The limit bounds the memory the URLs of a call take up, whatever the documents list. The
// default is a last resort rather than a tight bound: ten million URLs take up gigabytes.
// Set a lower limit for documents that are not trusted.
//
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it. The value that applies to a call is the one set when the call starts.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxURLs(maxURLs int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxURLs")
	if maxURLs < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxURLs", Err: fmt.Errorf("must be >= 0, got %d", maxURLs)})
		return s
	}
	s.cfg.maxURLs = maxURLs

	return s
}

// SetFollow sets the follow patterns using the provided list of regex strings and compiles them into regex objects.
// When patterns are set, only the sitemaps whose URL matches one of them are fetched, whether a
// sitemap index lists them or a robots.txt names them. The URL passed to Parse is always fetched.
// Patterns longer than maxRegexPatternLength characters are rejected with a *ConfigError.
// Any errors encountered during compilation are recorded as *ConfigError values.
// Each call replaces both the patterns and the errors recorded by the previous call.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFollow(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("follow")
	s.cfg.follow = regexes
	s.cfg.followRegexes = nil
	for _, followPattern := range s.cfg.follow {
		if len(followPattern) > maxRegexPatternLength {
			s.errs = append(s.errs, &ConfigError{Field: "follow", Err: fmt.Errorf("pattern exceeds maximum length of %d characters (%d)", maxRegexPatternLength, len(followPattern))})
			continue
		}
		re, err := regexp.Compile(followPattern)
		if err != nil {
			s.errs = append(s.errs, &ConfigError{Field: "follow", Err: err})
			continue
		}
		s.cfg.followRegexes = append(s.cfg.followRegexes, re)
	}

	return s
}

// SetRules sets the rules patterns using the provided list of regex strings and compiles them into regex objects.
// Patterns longer than maxRegexPatternLength characters are rejected with a *ConfigError.
// Any errors encountered during compilation are recorded as *ConfigError values.
// Each call replaces both the patterns and the errors recorded by the previous call.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetRules(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("rules")
	s.cfg.rules = regexes
	s.cfg.rulesRegexes = nil
	for _, rulePattern := range s.cfg.rules {
		if len(rulePattern) > maxRegexPatternLength {
			s.errs = append(s.errs, &ConfigError{Field: "rules", Err: fmt.Errorf("pattern exceeds maximum length of %d characters (%d)", maxRegexPatternLength, len(rulePattern))})
			continue
		}
		re, err := regexp.Compile(rulePattern)
		if err != nil {
			s.errs = append(s.errs, &ConfigError{Field: "rules", Err: err})
			continue
		}
		s.cfg.rulesRegexes = append(s.cfg.rulesRegexes, re)
	}
	return s
}

// SetHTTPClient sets a custom HTTP client for the Sitemap Parser.
// When set, the provided client is used for all HTTP requests instead of the
// internally created default client. This allows callers to configure custom
// transports, proxies, TLS settings, authentication, or timeout strategies.
// When a custom client is provided, SetFetchTimeout has no effect; the
// client's own Timeout field controls the request deadline.
// Pass nil to reset to the default client behaviour.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetHTTPClient(client *http.Client) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.httpClient = client

	return s
}

// SetStrict enables or disables strict mode for URL validation.
// In strict mode, all URLs in sitemap <loc> elements must be absolute HTTP(S) URLs
// on the same host and protocol as the sitemap file, and must not exceed 2048 characters,
// as required by the sitemaps.org specification.
// In tolerant mode (default), relative URLs are resolved against the parent sitemap URL.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetStrict(strict bool) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.strict = strict

	return s
}

// clearConfigErrors drops the configuration errors recorded for field, so that every setter
// call replaces the outcome of the previous call for the same setting instead of leaving a
// stale error behind. Must be called with s.mu held.
func (s *S) clearConfigErrors(field string) {
	s.errs = filterErrors(s.errs, func(err error) bool {
		cfgErr, ok := err.(*ConfigError)
		return !ok || cfgErr.Field != field
	})
}

// isConfigError reports whether err was recorded by a configuration setter.
func isConfigError(err error) bool {
	_, ok := err.(*ConfigError)
	return ok
}

// filterErrors returns the errors for which keep reports true, in their original order.
// It builds a new slice instead of filtering in place: GetErrors hands the backing array of
// the error list out to callers, and that copy must not change underneath them.
func filterErrors(errs []error, keep func(error) bool) []error {
	var kept []error
	for _, err := range errs {
		if keep(err) {
			kept = append(kept, err)
		}
	}
	return kept
}

// GetUserAgent returns the current user agent string used for HTTP requests.
func (s *S) GetUserAgent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.userAgent
}

// GetFetchTimeout returns the current fetch timeout in seconds.
func (s *S) GetFetchTimeout() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.fetchTimeout
}

// GetMultiThread returns whether multi-threaded fetching and parsing is enabled.
func (s *S) GetMultiThread() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.multiThread
}

// GetMaxResponseSize returns the maximum allowed HTTP response size in bytes.
func (s *S) GetMaxResponseSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxResponseSize
}

// GetMaxDepth returns the maximum recursion depth for following sitemap indexes.
func (s *S) GetMaxDepth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxDepth
}

// GetMaxConcurrency returns the maximum number of sitemaps fetched or parsed at the same time.
// A value of 0 means unlimited concurrency.
func (s *S) GetMaxConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxConcurrency
}

// GetMaxSitemaps returns the maximum number of sitemaps a Parse or ParseContext call fetches.
// A value of 0 means no limit.
func (s *S) GetMaxSitemaps() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxSitemaps
}

// GetMaxURLs returns the maximum number of URLs a Parse or ParseContext call collects.
// A value of 0 means no limit.
func (s *S) GetMaxURLs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxURLs
}

// GetFollow returns a copy of the current follow regex pattern strings.
func (s *S) GetFollow() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.cfg.follow))
	copy(result, s.cfg.follow)
	return result
}

// GetRules returns a copy of the current URL filter regex pattern strings.
func (s *S) GetRules() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.cfg.rules))
	copy(result, s.cfg.rules)
	return result
}

// GetHTTPClient returns the custom HTTP client, or nil if the default client behaviour is used.
func (s *S) GetHTTPClient() *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.httpClient
}

// GetStrict returns whether strict URL validation mode is enabled.
func (s *S) GetStrict() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.strict
}

// Parse is a method of the S structure. It parses the given URL and its content.
//
// Parse is a backward-compatible wrapper around ParseContext that uses
// context.Background(). For new code, prefer ParseContext so that callers
// can propagate cancellation and deadlines.
//
// An instance can be reused: every call first discards the URLs and errors
// collected by the previous call. Configuration errors recorded by the Set*
// methods are the exception. They persist across calls, and while any of them
// is outstanding Parse does not parse anything and returns an error with the
// message "errors occurred before parsing, see GetErrors() for details".
// Calling the same setter again with a valid value clears its error.
//
// A sitemap that is reached through a redirect is located at the URL it was
// served from, not at the URL that was requested: relative URLs in it are
// resolved against that URL, strict mode compares the URLs it lists with that
// URL, and the errors about the document name it.
//
// It sets the mainURL field to the given URL. The content is the given URL
// content or, if that is nil, what the URL serves.
// If the URL ends with "/robots.txt", it parses the robots.txt file and
// fetches URLs from the sitemap files mentioned in the robots.txt.
// If the URL does not end with "/robots.txt", the content is checked
// and unzipped if necessary, then parsed and fetched.
// The content of a document is not kept once the document is parsed.
//
// A call fetches no more sitemaps than SetMaxSitemaps allows, and collects no
// more URLs than SetMaxURLs allows, so that it comes to an end whatever the
// documents list.
//
// It returns the S structure, and a nil error if the document at the given
// URL was fetched and parsed. Otherwise the error tells why it was not:
//
//   - a configuration error is outstanding, see above;
//   - the URL is not valid: a *ValidationError;
//   - the document could not be fetched: a *NetworkError;
//   - the document could not be parsed: a *ParseError.
//
// Except for the first, the error returned is the one GetErrors() holds about
// it. What goes wrong further on is in GetErrors() only and does not fail the
// call: a sitemap the document lists that cannot be fetched or parsed, a limit
// that is reached, an entry that is not valid.
func (s *S) Parse(url string, urlContent *string) (*S, error) {
	return s.ParseContext(context.Background(), url, urlContent)
}

// validateInputURL parses url and verifies it uses http or https and has a host.
func (s *S) validateInputURL(url string) error {
	parsedURL, parseErr := neturl.Parse(url)
	if parseErr != nil {
		return &ValidationError{URL: url, Err: parseErr}
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{URL: url, Err: fmt.Errorf("invalid URL scheme %q: only http and https are supported", parsedURL.Scheme)}
	}
	if parsedURL.Host == "" {
		return &ValidationError{URL: url, Err: errors.New("missing host")}
	}
	return nil
}

// ParseContext parses the given URL and its content, honoring the supplied
// context for cancellation and deadlines.
//
// The context is propagated through every HTTP request issued by the parser
// (both the initial fetch and the recursive sitemap-index/urlset fetches),
// so cancelling ctx aborts in-flight downloads and prevents new ones from
// starting. Already-parsed URLs accumulated in s.urls before cancellation
// remain available via GetURLs().
//
// If ctx is done by the time the call ends, ParseContext returns a *ParseError
// that names the given URL and wraps the error of ctx, so errors.Is matches
// context.Canceled and context.DeadlineExceeded. The same error is recorded in
// the error list, once for the call: whether the sitemaps are fetched
// concurrently or one at a time, and however many of them were not fetched. A
// request that was cut short is recorded as the *NetworkError of that sitemap
// in addition.
//
// A call that fails for the given URL itself, as described at Parse, returns
// and records that error alone. This is the case when it is the request for
// the given URL that is cut short: the error is the *NetworkError of that
// request, which wraps the error of ctx as well.
//
// All other semantics match Parse.
func (s *S) ParseContext(ctx context.Context, url string, urlContent *string) (*S, error) {
	s.parseMu.Lock()
	defer s.parseMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	// Every call starts from a clean state, so nothing collected by a previous call carries
	// over into this one, whichever way this call ends. Configuration errors are the
	// exception: they belong to the instance rather than to a call, and keep blocking
	// parsing until the offending setting is corrected.
	s.mainURL = ""
	s.robotsTxtSitemapURLs = nil
	s.sitemapLocations = nil
	s.fetchedURLs = make(map[string]struct{})
	s.urls = nil
	s.errs = filterErrors(s.errs, isConfigError)
	// The limits of the call are those set by now: a SetMaxSitemaps or SetMaxURLs call made
	// while this one runs applies to the next one.
	s.limits = callLimits{maxSitemaps: s.cfg.maxSitemaps, maxURLs: s.cfg.maxURLs}

	if len(s.errs) > 0 {
		s.mu.Unlock()
		return s, errors.New("errors occurred before parsing, see GetErrors() for details")
	}

	if urlContent == nil {
		if vErr := s.validateInputURL(url); vErr != nil {
			s.errs = append(s.errs, vErr)
			s.mu.Unlock()
			return s, vErr
		}
	}

	if s.cfg.maxConcurrency > 0 {
		s.sem = make(chan struct{}, s.cfg.maxConcurrency)
	} else {
		s.sem = nil
	}
	// Whether the sitemaps are fetched concurrently is decided here, with the lock held and
	// once for the whole call: a SetMultiThread call made while this one runs applies to the
	// next one.
	parseAndFetchUrls := s.parseAndFetchUrlsSequential
	if s.cfg.multiThread {
		parseAndFetchUrls = s.parseAndFetchUrlsMultiThread
	}
	s.mu.Unlock()

	s.mainURL = url
	content, servedFrom, err := s.setContent(ctx, urlContent)
	if err != nil {
		s.mu.Lock()
		s.errs = append(s.errs, err)
		s.mu.Unlock()
		return s, err
	}

	if strings.HasSuffix(s.mainURL, "/robots.txt") {
		s.mu.Lock()
		s.parseRobotsTXT(content)
		locations := s.robotsTXTSitemapLocations(servedFrom)
		s.mu.Unlock()

		// The sitemaps a robots.txt lists are fetched the way those of a sitemap index are,
		// so that every setting governing the fetches applies to them as well.
		parseAndFetchUrls(ctx, servedFrom, locations, robotsTXTDepth)
	} else {
		locations, err := s.parseDocument(servedFrom, content)
		if err != nil {
			// The document the call is about cannot be parsed, which fails the call the way
			// its fetch failing does. The error is in the error list already.
			return s, err
		}

		parseAndFetchUrls(ctx, servedFrom, locations, 0)
	}

	s.mu.Lock()
	s.reportLimits(url)
	s.mu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// The cancellation is recorded here, and nowhere else: once for the call, whichever
		// way the sitemaps are fetched and wherever the cancellation caught them.
		err := &ParseError{URL: url, Err: ctxErr}
		s.mu.Lock()
		s.errs = append(s.errs, err)
		s.mu.Unlock()
		return s, err
	}

	return s, nil
}

func (s *S) GetErrorsCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.errs))
}

func (s *S) GetErrors() []error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs
}

// GetURLs returns the list of parsed URLs.
func (s *S) GetURLs() []URL {
	if s == nil {
		return []URL{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) <= 0 {
		return []URL{}
	}
	return s.urls
}

// GetURLCount returns the count of URLs in the S struct.
func (s *S) GetURLCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) <= 0 {
		return 0
	}
	return int64(len(s.urls))
}

// GetRandomURLs returns a slice of randomly selected URLs from the S object's URL list. The number of URLs to select is specified by the parameter n.
// If n exceeds the number of URLs in the list, all of them are returned, in random order.
// If n is zero or negative, or the S object is nil, an empty slice is returned.
// The function creates a copy of the original URLs list and randomly selects n URLs from it, removing them to avoid duplicates.
// The selected URLs are returned as a new slice.
func (s *S) GetRandomURLs(n int) []URL {
	if s == nil || n <= 0 {
		return []URL{}
	}

	s.mu.Lock()
	originalURLs := make([]URL, len(s.urls))
	copy(originalURLs, s.urls)
	s.mu.Unlock()

	// No more URLs can be selected than there are. Without this cap the result would be
	// allocated for n elements, however large n is.
	n = min(n, len(originalURLs))
	randURLs := make([]URL, 0, n)

	for i := 0; i < n; i++ {
		index := rand.IntN(len(originalURLs))
		randURLs = append(randURLs, originalURLs[index])

		// Remove the selected URL from the original list to avoid duplicates
		originalURLs[index] = originalURLs[len(originalURLs)-1] // Replace it with the last one.
		originalURLs = originalURLs[:len(originalURLs)-1]       // Remove last element.
	}

	return randURLs
}

// setContent extracts the main URL content or returns the provided URL content if not nil.
// It returns the extracted content as a string or an error if there was a problem fetching the content.
// Next to the content it returns the URL the content was served from: the main URL, or the URL
// the request for it was redirected to.
// The supplied context is propagated to the underlying HTTP request when fetching is required.
func (s *S) setContent(ctx context.Context, urlContent *string) (string, string, error) {
	if urlContent != nil {
		return *urlContent, s.mainURL, nil
	}

	return s.fetch(ctx, s.mainURL)
}

// parseRobotsTXT retrieves the sitemap URLs from the provided robots.txt content.
// It splits the content into lines and checks for lines beginning with "Sitemap:"
// (case-insensitive). UTF-8 BOM at the beginning of the file is stripped, lines
// starting with "#" are treated as comments and skipped, and any inline comment
// (text following an unescaped "#") is removed before extracting the URL.
// A value that is not empty is appended to the robotsTxtSitemapURLs slice as it stands in the
// file; whether it is a URL to fetch is decided by robotsTXTSitemapLocations.
// The method does not return any values, but it updates the robotsTxtSitemapURLs field of the S struct.
func (s *S) parseRobotsTXT(robotsTXTContent string) {
	// Strip UTF-8 BOM if present at the very beginning of the file.
	robotsTXTContent = strings.TrimPrefix(robotsTXTContent, "\ufeff")

	for line := range strings.SplitSeq(robotsTXTContent, "\n") {
		line = strings.TrimRight(line, "\r")
		// Trim leading whitespace so that indented directives are still recognised.
		line = strings.TrimLeft(line, " \t")
		// Skip blank lines and full-line comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 8 || !strings.EqualFold(line[:8], "sitemap:") {
			continue
		}
		value := line[8:]
		// Strip inline comments: anything after a "#" is considered a comment.
		if idx := strings.IndexByte(value, '#'); idx >= 0 {
			value = value[:idx]
		}
		url := strings.TrimSpace(value)
		if url != "" {
			// The value is copied: as a part of the content it would keep the whole file in
			// memory for as long as it is kept itself.
			s.robotsTxtSitemapURLs = append(s.robotsTxtSitemapURLs, strings.Clone(url))
		}
	}
}

// robotsTXTSitemapLocations returns the sitemaps to fetch of the ones a robots.txt names.
// What its Sitemap lines hold comes from the document just like the <loc> of a sitemap index
// entry does, and is treated the same way before anything is fetched: it is resolved and
// validated, a value that is rejected is skipped and reported, and a URL that matches none of
// the patterns set with SetFollow is skipped.
// url is the URL the robots.txt was served from.
// Must be called with s.mu held.
func (s *S) robotsTXTSitemapLocations(url string) []string {
	var locations []string
	for _, sitemapURL := range s.robotsTxtSitemapURLs {
		// A robots.txt may name a sitemap of any host: that is how the protocol has the
		// owner of a host approve of a sitemap kept elsewhere.
		location, err := s.resolveAndValidate(sitemapURL, url, false)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		if !s.matchesFollowFilter(location) {
			continue
		}
		locations = append(locations, location)
	}
	return locations
}

// acquireSlot blocks until a concurrency slot is available, or returns the
// context error if ctx is cancelled while waiting. When the semaphore is nil
// (unlimited concurrency) it still honours ctx so that callers receive a
// deterministic cancellation error.
func (s *S) acquireSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.sem == nil {
		return nil
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseSlot frees a previously acquired concurrency slot. It is a no-op when
// the semaphore is nil.
func (s *S) releaseSlot() {
	if s.sem == nil {
		return
	}
	<-s.sem
}

// fetch retrieves the content of the specified URL using an HTTP GET request.
// It returns the content, the URL the content was served from and an error if
// there was a problem fetching the URL.
// The URL the content was served from is url itself, unless the request was redirected: then
// it is the URL the last redirect led to.
// The HTTP status must be 200 (OK) for the request to be successful.
// The response body is automatically closed after reading using a defer statement.
// The supplied context is attached to the HTTP request, so cancelling it aborts
// the in-flight transfer.
func (s *S) fetch(ctx context.Context, url string) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	fetchTimeout := s.cfg.fetchTimeout
	userAgent := s.cfg.userAgent
	maxResponseSize := s.cfg.maxResponseSize
	httpClient := s.cfg.httpClient
	s.mu.Unlock()

	var client *http.Client
	if httpClient != nil {
		client = httpClient
	} else {
		client = &http.Client{
			Timeout: time.Duration(fetchTimeout) * time.Second,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}

	req.Header.Set("User-Agent", userAgent)

	response, err := client.Do(req)
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		return "", "", &NetworkError{URL: url, Err: fmt.Errorf("received HTTP status %d", response.StatusCode)}
	}

	body, err := readString(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}

	if int64(len(body)) > maxResponseSize {
		return "", "", &NetworkError{URL: url, Err: fmt.Errorf("response size exceeds limit of %d bytes", maxResponseSize)}
	}

	return body, finalURL(url, req, response), nil
}

// readBuffers holds the buffers readString reads through. A read needs one for as long as it
// lasts. Allocated for every read, the buffers of a call that fetches many small sitemaps would
// take up more memory than the sitemaps themselves.
var readBuffers = sync.Pool{
	New: func() any {
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

// readString reads r to its end and returns what it read.
// The content is read into the string that is returned, so that it does not have to be copied
// to become one, and is not copied again on its way through the parser.
// If reading fails, what was read until then is returned together with the error.
func readString(r io.Reader) (string, error) {
	buffer := readBuffers.Get().(*[]byte)
	defer readBuffers.Put(buffer)

	var content strings.Builder
	_, err := io.CopyBuffer(&content, r, *buffer)
	return content.String(), err
}

// finalURL returns the URL the response to req was served from: url, the URL req was made for,
// or the URL the request ended at when it was redirected.
// A document is located where it was served from, not where the request for it began.
// Relative URLs in it are resolved against that URL (RFC 3986, section 5.1.3), and it is the
// location the URLs a sitemap lists are compared with in strict mode.
// When no redirect took place, url is returned as it was passed in, so that the URL of a
// document stays spelled the way the caller or the sitemap naming it spelled it.
func finalURL(url string, req *http.Request, response *http.Response) string {
	// A RoundTripper is not required to name the request it answered; http.Transport does.
	last := response.Request
	if last == nil || last.URL == nil {
		return url
	}
	if final := last.URL.String(); final != req.URL.String() {
		return final
	}
	return url
}

// checkAndUnzipContent checks if the content is a gzip file and unzips it if necessary.
// If the content is a gzip file, it returns the uncompressed content.
// The decompressed size is capped at cfg.maxResponseSize, so a small compressed payload
// cannot expand without bound.
// If an error occurs during unzipping, or the decompressed size exceeds the cap, it appends
// a *ParseError (with the provided url) and returns the original content.
//
// Param url: The URL the content was fetched from (used for error context)
// Param content: The content to be checked and possibly unzipped
// Return string: The checked and possibly uncompressed content
func (s *S) checkAndUnzipContent(url string, content string) string {
	const gzipPrefix = "\x1f\x8b\x08"
	if strings.HasPrefix(content, gzipPrefix) {
		maxSize := s.cfg.maxResponseSize
		if maxSize <= 0 {
			// A zero-value S (one not created via New) has no configured limit.
			// Fall back to the default rather than leaving decompression unbounded.
			maxSize = defaultMaxResponseSize
		}
		uncompressed, err := unzip(content, maxSize)
		if err != nil {
			s.errs = append(s.errs, &ParseError{URL: url, Err: err})
			// return the original content if error
			return content
		}
		content = uncompressed
	}
	return content
}

// claimSitemap decides whether the sitemap at url is to be fetched, and counts it as fetched
// if it is. It returns fetch as false for a sitemap that was fetched already: a sitemap is
// fetched once in a call, however many documents list it.
//
// It returns limited as true if a limit of the call is reached, which goes for every sitemap
// that is still to come as well, and notes that a sitemap had to be left out for it. The limit
// is the one set with SetMaxSitemaps on the sitemaps the call fetches, or the one set with
// SetMaxURLs: a call that cannot collect any more URLs has no use for further sitemaps.
//
// If fetchedURLs has not been initialised (e.g. in direct unit tests), it is initialised here.
func (s *S) claimSitemap(url string) (fetch, limited bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, fetched := s.fetchedURLs[url]; fetched {
		return false, false
	}
	if s.limits.maxSitemaps > 0 && len(s.fetchedURLs) >= s.limits.maxSitemaps {
		s.limits.sitemapsLeftOut = true
		return false, true
	}
	if !s.hasRoomForURL() {
		return false, true
	}
	if s.fetchedURLs == nil {
		s.fetchedURLs = make(map[string]struct{})
	}
	s.fetchedURLs[url] = struct{}{}
	return true, false
}

// hasRoomForURL reports whether s may collect another URL. If the limit set with SetMaxURLs
// is reached, it notes that something had to be left out for it.
// Must be called with s.mu held, unless no other goroutine has access to s.
func (s *S) hasRoomForURL() bool {
	if s.limits.maxURLs > 0 && len(s.urls) >= s.limits.maxURLs {
		s.limits.urlsLeftOut = true
		return false
	}
	return true
}

// reportLimits records the limits that kept the call for url from fetching a sitemap or from
// collecting a URL. Each is recorded once, however many sitemaps and URLs were left out, and
// names the URL the call was made for: it is the call as a whole that the limits are about.
// Must be called with s.mu held.
func (s *S) reportLimits(url string) {
	if s.limits.sitemapsLeftOut {
		s.errs = append(s.errs, &ParseError{URL: url, Err: fmt.Errorf("limit of %d sitemaps reached", s.limits.maxSitemaps)})
	}
	if s.limits.urlsLeftOut {
		s.errs = append(s.errs, &ParseError{URL: url, Err: fmt.Errorf("limit of %d URLs reached", s.limits.maxURLs)})
	}
}

// withinMaxDepth reports whether the sitemaps found at the given depth may still be fetched.
// If the limit set with SetMaxDepth is reached, it records the error and returns false.
// The error names url, the document that lists the sitemaps: it is that document whose sitemaps
// are not followed.
// The limit is read with the lock held, like every other setting: a setter may be called
// while Parse is running.
func (s *S) withinMaxDepth(url string, depth int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if depth < s.cfg.maxDepth {
		return true
	}
	s.errs = append(s.errs, &ParseError{URL: url, Err: fmt.Errorf("max recursion depth of %d reached", s.cfg.maxDepth)})
	return false
}

// parseAndFetchUrlsMultiThread concurrently parses and fetches the URLs specified in the "locations" parameter.
// url is the document that lists them, as it was served.
// It uses a sync.WaitGroup to wait for all fetch operations to complete.
// For each location, it starts a goroutine that fetches and parses the document with fetchAndParse.
// A goroutine holds a concurrency slot while it does so, which makes the limit set with
// SetMaxConcurrency the number of documents that are being fetched or parsed at any time. It gives
// the slot back before it follows the sitemaps the document lists: the goroutines started for
// those need slots themselves, and would wait for this one forever if it kept its slot.
// The slot is taken before the goroutine is started, not by the goroutine. A sitemap that has
// to wait for a slot therefore costs no goroutine: the ones that exist are those working on a
// sitemap, and those waiting to go on with the sitemaps a sitemap index of theirs lists.
// If the context is done, also while a slot is waited for, the remaining locations are left
// alone. Nothing is recorded for them: that the call was cut short is recorded by ParseContext.
// The same goes for the locations that are left when a limit of the call is reached, see
// claimSitemap.
// If there is an error during the fetch operation, the error is appended to the "errs" field of the S structure.
// This method does not return any value.
func (s *S) parseAndFetchUrlsMultiThread(ctx context.Context, url string, locations []string, depth int) {
	if !s.withinMaxDepth(url, depth) {
		return
	}
	var wg sync.WaitGroup
	for _, location := range locations {
		if ctx.Err() != nil {
			break
		}
		fetch, limited := s.claimSitemap(location)
		if limited {
			break
		}
		if !fetch {
			continue
		}
		// acquireSlot also honours ctx cancellation, so a single check
		// here covers both the unlimited-concurrency and bounded paths.
		if s.acquireSlot(ctx) != nil {
			break
		}
		wg.Add(1)

		loc := location
		go func() {
			defer wg.Done()
			servedFrom, parsedLocations, err := s.fetchAndParse(ctx, loc)
			s.releaseSlot()
			if err != nil {
				s.mu.Lock()
				s.errs = append(s.errs, err)
				s.mu.Unlock()
				return
			}
			if len(parsedLocations) > 0 {
				s.parseAndFetchUrlsMultiThread(ctx, servedFrom, parsedLocations, depth+1)
			}
		}()
	}
	wg.Wait()
}

// parseAndFetchUrlsSequential sequentially parses and fetches the URLs specified in the "locations" parameter.
// url is the document that lists them, as it was served.
// For each location, it fetches and parses the document with fetchAndParse.
// If the context is done, or a limit of the call is reached (see claimSitemap), the remaining
// locations are left alone. Nothing is recorded for them: that the call was cut short is
// recorded by ParseContext.
// If there is an error during the fetch operation, the error is appended to the "errs" field of the S structure.
// This method does not return any value.
func (s *S) parseAndFetchUrlsSequential(ctx context.Context, url string, locations []string, depth int) {
	if !s.withinMaxDepth(url, depth) {
		return
	}
	for _, location := range locations {
		if ctx.Err() != nil {
			return
		}
		fetch, limited := s.claimSitemap(location)
		if limited {
			return
		}
		if !fetch {
			continue
		}
		servedFrom, parsedLocations, err := s.fetchAndParse(ctx, location)
		if err != nil {
			s.mu.Lock()
			s.errs = append(s.errs, err)
			s.mu.Unlock()
			continue
		}
		if len(parsedLocations) > 0 {
			s.parseAndFetchUrlsSequential(ctx, servedFrom, parsedLocations, depth+1)
		}
	}
}

// fetchAndParse fetches the sitemap at url and parses it.
// It returns the URL the sitemap was served from and the sitemaps it lists, or the error of the
// fetch. A sitemap that cannot be parsed lists none, and what is wrong with it is in the error
// list: of the sitemaps a document lists, one that cannot be parsed costs only itself.
func (s *S) fetchAndParse(ctx context.Context, url string) (string, []string, error) {
	content, servedFrom, err := s.fetch(ctx, url)
	if err != nil {
		return "", nil, err
	}
	locations, _ := s.parseDocument(servedFrom, content)

	return servedFrom, locations, nil
}

// parseDocument unzips the content served from url if it is gzip content, and parses it.
// It returns the sitemaps the document lists, and the error that tells the document could not
// be parsed if it could not. Like everything else that is wrong with the document, that error
// is added to the error list too.
//
// The work is done without holding s.mu. Unzipping and decoding a large document takes long, and
// with the lock held for it the documents would be processed one at a time however many were
// fetched concurrently, while every getter waited for the one being processed. The document is
// therefore processed by an instance of its own, which no other goroutine has access to. That
// instance has the settings of s as they stand when the work begins, so they are the same for
// the whole document. What it collects is added to s with the lock held and in one step, which
// keeps the URLs of a document together and in the order the document lists them.
//
// A call that has collected as many URLs as the limit set with SetMaxURLs allows has no use
// for the document: it is not parsed then, and nothing is returned.
//
// Must be called without s.mu held.
func (s *S) parseDocument(url string, content string) ([]string, error) {
	document := s.newDocument()
	if document == nil {
		return nil, nil
	}

	locations := document.parse(url, document.checkAndUnzipContent(url, content))
	s.addDocument(document)

	return locations, documentError(document.errs)
}

// newDocument returns the instance a document is processed by, see parseDocument. It returns
// nil if s has no room for another URL.
//
// The instance may collect as many URLs as s has room for at this point, so that a document
// does not pile up URLs only to have them dropped when they are added to s.
//
// Must be called without s.mu held.
func (s *S) newDocument() *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasRoomForURL() {
		return nil
	}

	document := &S{cfg: s.cfg}
	if s.limits.maxURLs > 0 {
		document.limits.maxURLs = s.limits.maxURLs - len(s.urls)
	}
	return document
}

// addDocument adds what document, an instance made by newDocument, collected to s.
//
// Must be called without s.mu held.
func (s *S) addDocument(document *S) {
	s.mu.Lock()
	defer s.mu.Unlock()

	urls := document.urls
	if room := s.limits.maxURLs - len(s.urls); s.limits.maxURLs > 0 && len(urls) > room {
		// There was room for these URLs when the document was begun, but other documents
		// were added in the meantime. The URLs there is no room for any more are given up.
		clear(urls[room:])
		urls = urls[:room]
		s.limits.urlsLeftOut = true
	}
	if s.urls == nil {
		// Nothing is collected yet, so the URLs of the document are taken over as they are
		// rather than copied: a call that parses a single sitemap is spared a second copy of
		// all of its URLs.
		s.urls = urls
	} else {
		s.urls = append(s.urls, urls...)
	}
	s.limits.urlsLeftOut = s.limits.urlsLeftOut || document.limits.urlsLeftOut
	s.sitemapLocations = append(s.sitemapLocations, document.sitemapLocations...)
	s.errs = append(s.errs, document.errs...)
}

// documentError returns the error that tells a document could not be parsed, out of errs, the
// errors the document yielded. It returns nil if the document was parsed.
// A document that cannot be parsed is reported as a *ParseError, what is wrong with one of its
// entries as a *ValidationError. Of several, the first is returned: content that cannot be
// unzipped is not recognised as a sitemap either, and it is the former that tells why.
func documentError(errs []error) error {
	for _, err := range errs {
		var parseErr *ParseError
		if errors.As(err, &parseErr) {
			return parseErr
		}
	}
	return nil
}

// newXMLDecoder returns a decoder for content that honours the encoding named in its XML
// declaration, so that a document in an encoding other than UTF-8 is transcoded while it is
// read. Every XML read in this package goes through it: a decoder without a CharsetReader
// rejects any document that declares another encoding.
func newXMLDecoder(content string) *xml.Decoder {
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder
}

// newDecoder returns the decoder the parsers read content with. Strict mode requires
// well-formed XML. Tolerant mode puts up with the mistakes encoding/xml can read past: an
// unescaped "&" or an unknown entity such as "&nbsp;" is taken literally, and a missing end
// tag is made up for.
func (s *S) newDecoder(content string) *xml.Decoder {
	decoder := newXMLDecoder(content)
	decoder.Strict = s.cfg.strict
	return decoder
}

// detectionCharsetReader is the CharsetReader of root element detection. It transcodes like
// the one newXMLDecoder installs, but reads the bytes as they are when the declared encoding
// is not supported. An encoding that cannot be transcoded must not hide the document type:
// the parser of the detected type then reports the unsupported encoding, instead of the
// document being taken for an unknown format.
func detectionCharsetReader(label string, input io.Reader) (io.Reader, error) {
	if reader, err := charset.NewReaderLabel(label, input); err == nil {
		return reader, nil
	}
	return input, nil
}

// detectRootElement reads the first XML start element from the content
// to determine the document type without fully parsing it.
// Returns the local name of the root element, or an empty string if detection fails.
func detectRootElement(content string) string {
	decoder := newXMLDecoder(content)
	decoder.CharsetReader = detectionCharsetReader
	// Detection is as lenient as the most lenient parser, for the same reason: what is wrong
	// with a document is for the parser of its type to report.
	decoder.Strict = false
	root, err := rootElement(decoder)
	if err != nil {
		return ""
	}
	return root.Name.Local
}

// rootElement reads decoder up to the first start element, which is the root element of the
// document when nothing has been read from decoder yet, and returns it.
func rootElement(decoder *xml.Decoder) (xml.StartElement, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if element, ok := token.(xml.StartElement); ok {
			return element, nil
		}
	}
}

// parse parses the provided URL and its content.
// The URL is the one the content was served from, which is not the one that was requested
// when the request was redirected.
// It determines whether the content is a sitemap index or a sitemap by inspecting
// the root XML element, then only invokes the appropriate parser.
// If it is a sitemap index, it adds the URLs from the sitemap index to the sitemap locations.
// If it is a sitemap, it adds the URLs from the sitemap to the URL list.
// Parsing errors are added to the error list.
// It returns a slice of sitemap locations that were added.
func (s *S) parse(url string, content string) []string {
	rootElement := detectRootElement(content)
	switch rootElement {
	case "sitemapindex":
		return s.parseSitemapIndexContent(url, content)
	case "urlset":
		s.parseURLSetContent(url, content)
	case "rss":
		s.parseRSSContent(url, content)
	case "feed":
		s.parseFeedContent(url, content)
	default:
		s.parseTextContent(url, rootElement, content)
	}
	return nil
}

func (s *S) parseSitemapIndexContent(url, content string) []string {
	var added []string
	smIndex, err := s.parseSitemapIndex(content)
	if err != nil {
		s.errs = append(s.errs, &ParseError{URL: url, Err: err})
		return added
	}
	s.sitemapLocations = append(s.sitemapLocations, url)
	for _, sm := range smIndex.Sitemap {
		sm.Loc = strings.TrimSpace(sm.Loc)
		resolvedLoc, err := s.resolveAndValidateLoc(sm.Loc, url)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		sm.Loc = resolvedLoc
		if !s.matchesFollowFilter(sm.Loc) {
			continue
		}
		added = append(added, sm.Loc)
		s.sitemapLocations = append(s.sitemapLocations, sm.Loc)
	}
	return added
}

func (s *S) parseURLSetContent(url, content string) {
	// The entries are dealt with one by one while the document is read, yet a document that
	// cannot be read to its end yields nothing but the error: what its entries added until
	// then is dropped again, and with it the note that a URL of it had to be left out.
	urls, errs, limits := s.urls, s.errs, s.limits
	err := s.parseURLSet(content, func(entry *urlEntry) {
		s.addURLEntry(entry, url)
	})
	if err != nil {
		s.urls, s.errs, s.limits = urls, errs, limits
		s.errs = append(s.errs, &ParseError{URL: url, Err: err})
	}
}

// addURLEntry resolves, validates and filters an entry of the <urlset> served from baseURL,
// and appends the URL it stands for to s.urls. What is wrong with the entry is added to s.errs.
// Once s has no room for another URL, the entries are left out as they come: unread, so that
// they add no errors either.
func (s *S) addURLEntry(entry *urlEntry, baseURL string) {
	if !s.hasRoomForURL() {
		return
	}
	u := entry.URL
	u.Loc = strings.TrimSpace(u.Loc)
	resolvedLoc, err := s.resolveAndValidateLoc(u.Loc, baseURL)
	if err != nil {
		s.errs = append(s.errs, err)
		return
	}
	u.Loc = resolvedLoc
	// A value that cannot be parsed is left out and reported. Strict mode also skips the
	// entry when the value is one of its own, as it does for a priority out of range; a
	// value of an extension costs the entry nothing more in either mode.
	for _, invalid := range entry.invalid {
		s.errs = append(s.errs, &ValidationError{URL: u.Loc, Err: invalid.err()})
	}
	if s.cfg.strict && entry.invalidOwn {
		return
	}
	if err := s.validatePriority(u.Loc, u.Priority); err != nil {
		s.errs = append(s.errs, err)
		return
	}
	validImages, imageErrs := s.validateAndFilterImages(u.Images)
	u.Images = validImages
	s.errs = append(s.errs, imageErrs...)
	validNews, newsErrs := s.validateNews(u.Loc, u.News, entry.newsDateInvalid())
	u.News = validNews
	s.errs = append(s.errs, newsErrs...)
	validVideos, videoErrs := s.validateAndFilterVideos(u.Videos)
	u.Videos = validVideos
	s.errs = append(s.errs, videoErrs...)
	validHreflangs, hreflangErrs := s.validateAndFilterHreflangs(u.Hreflangs)
	u.Hreflangs = validHreflangs
	s.errs = append(s.errs, hreflangErrs...)
	if !s.matchesRulesFilter(u.Loc) {
		return
	}
	s.urls = append(s.urls, u)
}

func (s *S) parseRSSContent(url, content string) {
	rssFeed, err := s.parseRSS(content)
	if err != nil {
		s.errs = append(s.errs, &ParseError{URL: url, Err: err})
		return
	}
	for _, item := range rssFeed.Channel.Item {
		s.addURL(item.Link, url)
	}
}

func (s *S) parseFeedContent(url, content string) {
	atomFeed, err := s.parseAtom(content)
	if err != nil {
		s.errs = append(s.errs, &ParseError{URL: url, Err: err})
		return
	}
	for _, entry := range atomFeed.Entry {
		var loc string
		for _, l := range entry.Link {
			if l.Rel == "" || l.Rel == "alternate" {
				loc = l.Href
				break
			}
		}
		s.addURL(loc, url)
	}
}

func (s *S) parseTextContent(url, rootElement, content string) {
	found := false
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			found = true
			// The line is copied: as a part of the content it would keep the whole document
			// in memory for as long as the URL or an error about it is kept.
			s.addURL(strings.Clone(line), url)
		}
	}
	if found {
		return
	}
	if len(content) == 0 {
		s.errs = append(s.errs, &ParseError{URL: url, Err: errors.New("sitemap content is empty")})
	} else {
		s.errs = append(s.errs, &ParseError{URL: url, Err: fmt.Errorf("unrecognized sitemap format (root element: %q)", rootElement)})
	}
}

func (s *S) matchesFollowFilter(loc string) bool {
	if len(s.cfg.followRegexes) == 0 {
		return true
	}
	for _, re := range s.cfg.followRegexes {
		if re.MatchString(loc) {
			return true
		}
	}
	return false
}

func (s *S) matchesRulesFilter(loc string) bool {
	if len(s.cfg.rulesRegexes) == 0 {
		return true
	}
	for _, re := range s.cfg.rulesRegexes {
		if re.MatchString(loc) {
			return true
		}
	}
	return false
}

// addURL resolves, validates, filters, and appends a single location to s.urls.
// Used by RSS, Atom, and Text parsers.
// An empty location is skipped without an error: the link of a feed item is optional, so an
// item without one is not a mistake, it merely names no page.
// Once s has no room for another URL, the locations are left out as they come, see addURLEntry.
func (s *S) addURL(loc string, baseURL string) {
	loc = strings.TrimSpace(loc)
	if loc == "" || !s.hasRoomForURL() {
		return
	}
	resolvedLoc, err := s.resolveAndValidateLoc(loc, baseURL)
	if err != nil {
		s.errs = append(s.errs, err)
		return
	}
	if s.matchesRulesFilter(resolvedLoc) {
		s.urls = append(s.urls, URL{Loc: resolvedLoc})
	}
}

// parseSitemapIndex parses the sitemap index data and returns a sitemapIndex object and an error.
// The data parameter contains the XML data of the sitemap index.
// If the data is empty, it returns an error with the message "sitemapindex is empty".
// It uses an xml.Decoder with charset support to decode the XML data into a sitemapIndex object.
// It returns the sitemapIndex object and any decoding error that occurred.
func (s *S) parseSitemapIndex(data string) (sitemapIndex, error) {
	var smIndex sitemapIndex

	if len(data) == 0 {
		return smIndex, fmt.Errorf("sitemapindex is empty")
	}

	err := s.newDecoder(data).Decode(&smIndex)
	return smIndex, err

}

// parseURLSet takes a string of XML data representing a sitemap and hands its <url> entries
// to handle, one at a time and in the order they stand in the document.
// If the data is empty, it returns an error with the message "sitemap is empty".
// It uses an xml.Decoder with charset support to decode the XML data.
// If there is an error during decoding, it returns the decode error. The entries that precede
// the place of the error have been handed to handle by then.
//
// The entries are not collected. Decoded into a slice first, the entries of a large sitemap
// take up several times the memory of the URLs they end up as, for as long as the document is
// being read. The <urlset> is therefore read element by element, the way encoding/xml reads
// it into a structure that has a field for <url>: an element of that name is decoded,
// whatever its namespace, any other element is skipped together with what it contains, and
// what follows the end of the <urlset> is not read.
func (s *S) parseURLSet(data string, handle func(entry *urlEntry)) error {
	if len(data) == 0 {
		return fmt.Errorf("sitemap is empty")
	}

	decoder := s.newDecoder(data)
	root, err := rootElement(decoder)
	if err != nil {
		return err
	}
	if root.Name.Local != "urlset" {
		return xml.UnmarshalError("expected element type <urlset> but have <" + root.Name.Local + ">")
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "url" {
				if err := decoder.Skip(); err != nil {
					return err
				}
				continue
			}
			var entry urlEntry
			if err := decoder.DecodeElement(&entry, &element); err != nil {
				return err
			}
			entry.convert()
			handle(&entry)
		case xml.EndElement:
			return nil
		}
	}
}

// convert parses the elements that were read as text into the typed fields of the embedded
// URL. An element whose content is invalid leaves its field unset and is added to e.invalid.
func (e *urlEntry) convert() {
	e.LastMod = parseElement(&e.invalid, "<lastmod>", e.LastModText, parseLastModTime)
	e.Priority = parseElement(&e.invalid, "<priority>", e.PriorityText, parseFloat32)
	e.invalidOwn = len(e.invalid) > 0

	if e.NewsEntry != nil {
		news := e.NewsEntry.News
		news.PublicationDate = parseElement(&e.invalid, "news <publication_date>", e.NewsEntry.PublicationDateText, parseLastModTime)
		e.News = &news
	}

	for _, entry := range e.VideoEntries {
		video := entry.Video
		video.Duration = parseElement(&e.invalid, "video <duration>", entry.DurationText, parseInt)
		video.ExpirationDate = parseElement(&e.invalid, "video <expiration_date>", entry.ExpirationDateText, parseLastModTime)
		video.Rating = parseElement(&e.invalid, "video <rating>", entry.RatingText, parseFloat32)
		video.ViewCount = parseElement(&e.invalid, "video <view_count>", entry.ViewCountText, parseInt)
		video.PublicationDate = parseElement(&e.invalid, "video <publication_date>", entry.PublicationDateText, parseLastModTime)
		e.Videos = append(e.Videos, video)
	}
}

// newsDateInvalid reports whether the entry has a news publication date that could not be
// parsed.
func (e *urlEntry) newsDateInvalid() bool {
	return e.NewsEntry != nil && e.NewsEntry.PublicationDateText != nil && e.News.PublicationDate == nil
}

// parseElement parses the text of an optional element. It returns nil for an element that is
// absent, which is what a nil text stands for, and for one whose content parse rejects. The
// latter is also added to invalid under the given name.
func parseElement[T any](invalid *[]invalidValue, element string, text *string, parse func(string) (T, error)) *T {
	if text == nil {
		return nil
	}
	value, err := parse(*text)
	if err != nil {
		*invalid = append(*invalid, invalidValue{element: element, text: *text})
		return nil
	}
	return &value
}

// parseFloat32 parses the content of an element holding a decimal number. As when encoding/xml
// decodes into a numeric field, surrounding whitespace is ignored and an empty element reads
// as zero.
func parseFloat32(text string) (float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(text, 32)
	return float32(value), err
}

// parseInt parses the content of an element holding an integer, by the rules of parseFloat32.
func parseInt(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	return strconv.Atoi(text)
}

// err returns the error that reports v.
func (v invalidValue) err() error {
	return fmt.Errorf("invalid %s value %q", v.element, strings.TrimSpace(v.text))
}

// parseRSS parses the RSS 2.0 data and returns an rss object and an error.
func (s *S) parseRSS(data string) (rss, error) {
	var feed rss
	if len(data) == 0 {
		return feed, fmt.Errorf("rss is empty")
	}

	err := s.newDecoder(data).Decode(&feed)
	return feed, err
}

// parseAtom parses the Atom 1.0 data and returns an atom object and an error.
func (s *S) parseAtom(data string) (atom, error) {
	var feed atom
	if len(data) == 0 {
		return feed, fmt.Errorf("atom is empty")
	}

	err := s.newDecoder(data).Decode(&feed)
	return feed, err
}

// maxLocLength is the maximum URL length allowed in a sitemap <loc> element per the sitemaps.org specification.
const maxLocLength = 2048

// maxVideoDuration is the maximum allowed <video:duration> in seconds per the Google specification.
const maxVideoDuration = 28800

// maxVideoTags is the maximum number of <video:tag> elements allowed per video per the Google specification.
const maxVideoTags = 32

// maxVideoRating is the maximum allowed <video:rating> value per the Google specification.
const maxVideoRating = float32(5.0)

// maxRegexPatternLength is the maximum allowed length of a regex pattern string passed to SetFollow or SetRules.
// Go's regexp package uses RE2 semantics and is therefore not vulnerable to catastrophic backtracking,
// but arbitrarily long patterns can still produce large compiled automata and consume significant memory.
const maxRegexPatternLength = 1000

// defaultMaxResponseSize is the default maximum size in bytes of an HTTP response body and of
// decompressed gzip content. 50 MB is the uncompressed sitemap size limit of the sitemaps.org
// specification.
const defaultMaxResponseSize = 50 * 1024 * 1024

// defaultMaxConcurrency is the default maximum number of sitemaps that are fetched or parsed at
// the same time in a Parse call.
// Limiting concurrency by default prevents unbounded goroutine and connection growth when parsing
// large sitemap indexes. Pass 0 to SetMaxConcurrency to restore unlimited concurrency.
const defaultMaxConcurrency = 16

// defaultMaxSitemaps is the default maximum number of sitemaps a Parse call fetches. 50,000 is
// the number of sitemaps a sitemap index may list according to the sitemaps.org specification.
// Pass 0 to SetMaxSitemaps to lift the limit.
const defaultMaxSitemaps = 50000

// defaultMaxURLs is the default maximum number of URLs a Parse call collects. It is more than
// all but the largest sites list, and there to keep a call from collecting URLs without end.
// Pass 0 to SetMaxURLs to lift the limit.
const defaultMaxURLs = 10000000

// robotsTXTDepth is the depth at which the sitemaps listed in a robots.txt are fetched: one
// level above 0, the depth of the sitemaps a sitemap index names. A sitemap listed in a
// robots.txt stands where the main URL of a call does when that is a sitemap itself, and
// fetching the main URL does not count towards the limit set with SetMaxDepth either.
const robotsTXTDepth = -1

// validatePriority validates the <priority> value of a URL entry.
// In strict mode, the value must be between 0.0 and 1.0 inclusive per the sitemaps.org specification.
// In tolerant mode, any value is accepted and nil is returned.
// loc is the page URL used as context in the returned *ValidationError.
func (s *S) validatePriority(loc string, priority *float32) error {
	if !s.cfg.strict || priority == nil {
		return nil
	}
	if *priority < 0.0 || *priority > 1.0 {
		return &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: priority %g is out of range [0.0, 1.0]", *priority)}
	}
	return nil
}

// validateAndFilterImages validates the image entries on a parsed URL and returns
// the filtered slice of valid images along with any validation errors.
//
// In tolerant mode, images with an empty Loc are silently dropped. In strict mode,
// an empty Loc is an error. In both modes, a Loc exceeding maxLocLength characters
// is rejected. In strict mode, Loc must additionally be an absolute HTTP or HTTPS URL.
//
// Note: image Loc values are not required to share the host of the parent page URL —
// CDN-hosted images are explicitly permitted by the Google Image Sitemap specification.
func (s *S) validateAndFilterImages(images []Image) ([]Image, []error) {
	if len(images) == 0 {
		return images, nil
	}
	valid := images[:0:0]
	var errs []error
	for _, img := range images {
		if img.Loc == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: "", Err: errors.New("strict mode: image <loc> is empty")})
			}
			continue
		}
		if len(img.Loc) > maxLocLength {
			errs = append(errs, &ValidationError{URL: img.Loc, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(img.Loc))})
			continue
		}
		if s.cfg.strict {
			parsed, err := neturl.Parse(img.Loc)
			if err != nil {
				errs = append(errs, &ValidationError{URL: img.Loc, Err: err})
				continue
			}
			if parsed.Scheme != "http" && parsed.Scheme != "https" {
				errs = append(errs, &ValidationError{URL: img.Loc, Err: fmt.Errorf("strict mode: unsupported scheme %q", parsed.Scheme)})
				continue
			}
		}
		valid = append(valid, img)
	}
	return valid, errs
}

// validateNews validates the news entry on a parsed URL in strict mode and returns
// the entry along with any validation errors.
//
// In tolerant mode the entry is returned unchanged with no errors.
// In strict mode all four fields required by the Google News Sitemap specification
// must be present: Publication.Name, Publication.Language, PublicationDate, and Title.
// Missing required fields are each reported as a separate *ValidationError; the News entry itself
// is kept so that callers still have access to any data that was successfully parsed.
// A nil input is a no-op and returns nil, nil.
// loc is the parent page URL used as context in the returned *ValidationError values.
// dateInvalid tells that the entry does have a publication date, which could not be parsed.
// That has been reported already, so the date is not reported as missing on top of it.
func (s *S) validateNews(loc string, news *News, dateInvalid bool) (*News, []error) {
	if news == nil {
		return nil, nil
	}
	if !s.cfg.strict {
		return news, nil
	}
	var errs []error
	if news.Title == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <title> is empty")})
	}
	if news.Publication.Name == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication><name> is empty")})
	}
	if news.Publication.Language == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication><language> is empty")})
	}
	if news.PublicationDate == nil && !dateInvalid {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication_date> is missing")})
	}
	return news, errs
}

// validateAndFilterVideos validates the video entries on a parsed URL and returns
// the filtered slice of valid videos along with any validation errors.
//
// ThumbnailLoc is treated as the primary key: videos with an empty ThumbnailLoc
// are silently dropped in tolerant mode or produce an error in strict mode.
// In both modes, a ThumbnailLoc exceeding maxLocLength is rejected. In strict mode,
// ThumbnailLoc must additionally be a parseable absolute HTTP(S) URL.
//
// For videos that pass the ThumbnailLoc check, strict mode also validates the
// remaining required fields (Title, Description, at least one of ContentLoc or
// PlayerLoc) and optional numeric fields (Duration range 1–28800, Rating range
// 0.0–5.0, Tags count ≤ 32). These failures record errors but keep the video entry.
func (s *S) validateAndFilterVideos(videos []Video) ([]Video, []error) {
	if len(videos) == 0 {
		return videos, nil
	}
	valid := videos[:0:0]
	var errs []error
	for _, v := range videos {
		if v.ThumbnailLoc == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: "", Err: errors.New("strict mode: video <thumbnail_loc> is empty")})
			}
			continue
		}
		if len(v.ThumbnailLoc) > maxLocLength {
			errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(v.ThumbnailLoc))})
			continue
		}
		if s.cfg.strict {
			ok, thumbErrs := s.validateVideoThumbnailStrict(v.ThumbnailLoc)
			errs = append(errs, thumbErrs...)
			if !ok {
				continue
			}
			errs = append(errs, s.validateVideoFieldsStrict(v)...)
		}
		valid = append(valid, v)
	}
	return valid, errs
}

// validateVideoThumbnailStrict validates the ThumbnailLoc URL scheme in strict mode.
// Returns false if the video should be skipped entirely.
func (s *S) validateVideoThumbnailStrict(thumbnailLoc string) (bool, []error) {
	parsed, err := neturl.Parse(thumbnailLoc)
	if err != nil {
		return false, []error{&ValidationError{URL: thumbnailLoc, Err: err}}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false, []error{&ValidationError{URL: thumbnailLoc, Err: fmt.Errorf("strict mode: unsupported scheme %q", parsed.Scheme)}}
	}
	return true, nil
}

// validateVideoFieldsStrict checks non-fatal strict-mode constraints on a video entry.
func (s *S) validateVideoFieldsStrict(v Video) []error {
	var errs []error
	if v.Title == "" {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: errors.New("strict mode: video <title> is empty")})
	}
	if v.Description == "" {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: errors.New("strict mode: video <description> is empty")})
	}
	if v.ContentLoc == "" && v.PlayerLoc == "" {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: errors.New("strict mode: video must have at least one of <content_loc> or <player_loc>")})
	}
	if v.Duration != nil && (*v.Duration < 1 || *v.Duration > maxVideoDuration) {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: fmt.Errorf("strict mode: video <duration> %d is out of range [1, %d]", *v.Duration, maxVideoDuration)})
	}
	if v.Rating != nil && (*v.Rating < 0.0 || *v.Rating > maxVideoRating) {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: fmt.Errorf("strict mode: video <rating> %g is out of range [0.0, %g]", *v.Rating, maxVideoRating)})
	}
	if len(v.Tags) > maxVideoTags {
		errs = append(errs, &ValidationError{URL: v.ThumbnailLoc, Err: fmt.Errorf("strict mode: video has %d tags, maximum is %d", len(v.Tags), maxVideoTags)})
	}
	return errs
}

// validateAndFilterHreflangs validates the alternate link (hreflang) entries on a parsed URL
// and returns the filtered slice of valid links along with any validation errors.
//
// In tolerant mode, links with an empty Href are silently dropped. In strict mode,
// an empty Href is an error. In both modes, an Href exceeding maxLocLength characters
// is rejected. In strict mode, Href must additionally be an absolute HTTP or HTTPS URL,
// and Hreflang must not be empty.
func (s *S) validateAndFilterHreflangs(links []AlternateLink) ([]AlternateLink, []error) {
	if len(links) == 0 {
		return links, nil
	}
	valid := links[:0:0]
	var errs []error
	for _, link := range links {
		if link.Href == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: "", Err: errors.New("strict mode: alternate link <href> is empty")})
			}
			continue
		}
		if len(link.Href) > maxLocLength {
			errs = append(errs, &ValidationError{URL: link.Href, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(link.Href))})
			continue
		}
		if s.cfg.strict {
			if link.Rel != "alternate" {
				errs = append(errs, &ValidationError{URL: link.Href, Err: fmt.Errorf("strict mode: alternate link <rel> must be \"alternate\", got %q", link.Rel)})
				continue
			}
			if link.Hreflang == "" {
				errs = append(errs, &ValidationError{URL: link.Href, Err: errors.New("strict mode: alternate link <hreflang> is empty")})
				continue
			}
			parsed, err := neturl.Parse(link.Href)
			if err != nil {
				errs = append(errs, &ValidationError{URL: link.Href, Err: err})
				continue
			}
			if parsed.Scheme != "http" && parsed.Scheme != "https" {
				errs = append(errs, &ValidationError{URL: link.Href, Err: fmt.Errorf("strict mode: unsupported scheme %q", parsed.Scheme)})
				continue
			}
		}
		valid = append(valid, link)
	}
	return valid, errs
}

// resolveAndValidateLoc resolves and validates a <loc> URL found in a sitemap.
// In both modes, an empty loc is rejected: it is a location that is missing, not a relative
// URL, and resolved as one it would name the sitemap itself. The error names that sitemap,
// there being no location to name.
// In both modes, URLs must not exceed 2048 characters (sitemaps.org specification).
// In tolerant mode (strict=false), relative URLs are resolved against baseURL before the length check.
// In strict mode (strict=true), URLs must additionally be absolute HTTP(S), on the same host
// and protocol as baseURL.
// baseURL is the URL the sitemap was served from: the location of the sitemap, whichever URL
// the request for it began at.
// Returns the resolved URL string and an error if validation fails.
func (s *S) resolveAndValidateLoc(loc string, baseURL string) (string, error) {
	return s.resolveAndValidate(loc, baseURL, true)
}

// resolveAndValidate resolves and validates a URL found in the document served from baseURL.
// It does what resolveAndValidateLoc is documented to do. sameHost tells whether the URL has to
// be on the host and protocol of baseURL in strict mode: the URLs listed in a sitemap have to,
// the sitemaps named in a robots.txt do not.
func (s *S) resolveAndValidate(loc string, baseURL string, sameHost bool) (string, error) {
	if loc == "" {
		return loc, &ValidationError{URL: baseURL, Err: errors.New("<loc> of an entry is empty or missing")}
	}

	base, err := neturl.Parse(baseURL)
	if err != nil {
		return loc, &ValidationError{URL: baseURL, Err: err}
	}

	parsed, err := neturl.Parse(loc)
	if err != nil {
		return loc, &ValidationError{URL: loc, Err: err}
	}

	if s.cfg.strict {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: unsupported scheme %q", parsed.Scheme)}
		}
		if parsed.Host == "" {
			return loc, &ValidationError{URL: loc, Err: errors.New("strict mode: missing host")}
		}
		if sameHost && parsed.Scheme != base.Scheme {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: scheme %q does not match sitemap scheme %q", parsed.Scheme, base.Scheme)}
		}
		if sameHost && parsed.Host != base.Host {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: host %q does not match sitemap host %q", parsed.Host, base.Host)}
		}
		if len(loc) > maxLocLength {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(loc))}
		}
		return loc, nil
	}

	// Tolerant mode: resolve relative URLs against the base
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		resolvedStr := resolved.String()
		return loc, &ValidationError{URL: resolvedStr, Err: fmt.Errorf("unsupported scheme %q", resolved.Scheme)}
	}
	resolvedStr := resolved.String()
	if len(resolvedStr) > maxLocLength {
		return loc, &ValidationError{URL: resolvedStr, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(resolvedStr))}
	}

	return resolvedStr, nil
}

// unzip decompresses the given content using gzip compression.
// It returns the uncompressed content and any error encountered during decompression.
// At most maxSize bytes are decompressed. If the payload is larger, decompression stops and
// an error is returned without any data, so a small compressed input cannot exhaust memory
// (decompression bomb).
// If the gzip header is invalid, the original content is returned together with the error.
// If decompression fails mid-stream (e.g. truncated/corrupted gzip data), the partially
// decompressed content is returned together with the error so the caller can decide how to react.
// In all error cases a non-nil error is returned; callers must not silently use the data.
func unzip(content string, maxSize int64) (string, error) {
	reader, err := gzip.NewReader(strings.NewReader(content))
	if err != nil {
		return content, err
	}
	// Disable multistream support: many real-world sitemap servers (and the test
	// harness in this package) append a trailing newline or other padding after
	// the gzip footer. Without this, gzip.Reader would try to parse a second
	// member and fail with io.ErrUnexpectedEOF, even though the actual payload
	// was decompressed correctly.
	reader.Multistream(false)

	defer func(reader *gzip.Reader) {
		_ = reader.Close()
	}(reader)

	// Read one byte past the limit so that a payload exceeding it can be told apart from one
	// that fits exactly, while never buffering more than maxSize+1 bytes.
	readLimit := maxSize
	if readLimit < math.MaxInt64 {
		readLimit++
	}

	uncompressed, err := readString(io.LimitReader(reader, readLimit))
	if err != nil {
		return uncompressed, fmt.Errorf("gzip decompression failed: %w", err)
	}

	if int64(len(uncompressed)) > maxSize {
		return "", fmt.Errorf("decompressed size exceeds limit of %d bytes", maxSize)
	}

	return uncompressed, nil
}

// lastModFormats are the date and time layouts accepted in <lastmod> and in the date elements
// of the extensions.
var lastModFormats = []string{
	"2006",
	"2006-01",
	"2006-01-02",
	"2006-01-02T15:04-07:00",
	"2006-01-02T15:04Z",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999Z",
	time.RFC3339,
	time.RFC3339Nano,
}

func (l *LastModTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	err := d.DecodeElement(&v, &start)
	if err != nil {
		return err
	}

	// An empty <lastmod> element (or one containing only whitespace) is common
	// in real-world sitemaps. Treat it as "not set" rather than an error: leave
	// the zero value in place and let the caller decide how to interpret it.
	if strings.TrimSpace(v) == "" {
		return nil
	}

	parsed, err := parseLastModTime(v)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// parseLastModTime parses a date or a date and time in one of lastModFormats, ignoring
// surrounding whitespace. Empty text yields the zero value, as an empty element does when
// decoded by UnmarshalXML.
func parseLastModTime(text string) (LastModTime, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return LastModTime{}, nil
	}

	for _, format := range lastModFormats {
		if parsedTime, err := time.Parse(format, text); err == nil {
			return LastModTime{parsedTime}, nil
		}
	}

	return LastModTime{}, fmt.Errorf("unsupported lastmod format: %q", text)
}

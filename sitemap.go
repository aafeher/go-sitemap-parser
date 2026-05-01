package sitemap

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"
)

type (

	// S is a structure that holds various data related to processing URLs.
	// It contains a cfg field of type `config`, which stores configuration settings.
	// The mainURL field of type string represents the main URL being processed.
	// The mainURLContent field of type string stores the content of the main URL.
	// The robotsTxtSitemapURLs field is a slice of strings that contains the URLs present in the robots.txt file's sitemap directive.
	// The sitemapLocations field is a slice of strings that represents the locations of the sitemap files.
	// The urls field is a slice of URL structs that stores the URLs to be processed.
	// The errs field is a slice of errors that holds any encountered errors during processing.
	S struct {
		parseMu              sync.Mutex
		mu                   sync.Mutex
		cfg                  config
		mainURL              string
		mainURLContent       string
		robotsTxtSitemapURLs []string
		sitemapLocations     []string
		urls                 []URL
		errs                 []error
		// sem is a per-Parse-call semaphore that bounds the number of
		// concurrently running fetch goroutines when cfg.maxConcurrency > 0.
		// It is created at the start of ParseContext and is nil when
		// concurrency is unlimited.
		sem chan struct{}
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
		multiThread     bool
		strict          bool
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

	// URLSet is a structure of <urlset>
	URLSet struct {
		XMLName xml.Name `xml:"urlset"`
		URL     []URL    `xml:"url"`
	}

	// URL is a structure of <url> in <urlset>
	URL struct {
		Loc        string         `xml:"loc"`
		LastMod    *lastModTime   `xml:"lastmod"`
		ChangeFreq *URLChangeFreq `xml:"changefreq"`
		Priority   *float32       `xml:"priority"`
	}

	lastModTime struct {
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
		maxResponseSize: 50 * 1024 * 1024, // 50 MB per sitemaps.org spec
		maxDepth:        10,
		maxConcurrency:  0, // 0 = unlimited (backward compatible)
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
// It should be specified in seconds as a uint16 value.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFetchTimeout(fetchTimeout uint16) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.fetchTimeout = fetchTimeout

	return s
}

// SetMultiThread sets the multi-threading for the Sitemap Parser.
// The multi-threading flag determines whether the parser should fetch URLs concurrently using goroutines.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMultiThread(multiThread bool) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.multiThread = multiThread

	return s
}

// SetMaxResponseSize sets the maximum allowed HTTP response size in bytes.
// Responses exceeding this limit will be truncated and may cause parsing errors.
// The default is 50 MB, matching the sitemaps.org protocol limit.
// The value must be greater than 0; invalid values are ignored and an error is recorded.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxResponseSize(maxResponseSize int64) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxResponseSize <= 0 {
		s.errs = append(s.errs, fmt.Errorf("maxResponseSize must be greater than 0, got %d", maxResponseSize))
		return s
	}
	s.cfg.maxResponseSize = maxResponseSize

	return s
}

// SetMaxDepth sets the maximum recursion depth for following sitemap indexes.
// A sitemap index may reference other sitemap indexes; this limits how many levels deep
// the parser will follow. The default is 10.
// The value must be greater than 0; invalid values are ignored and an error is recorded.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxDepth(maxDepth int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxDepth <= 0 {
		s.errs = append(s.errs, fmt.Errorf("maxDepth must be greater than 0, got %d", maxDepth))
		return s
	}
	s.cfg.maxDepth = maxDepth

	return s
}

// SetMaxConcurrency sets the maximum number of concurrent fetch goroutines used
// when multi-threaded parsing is enabled. A value of 0 (the default) means
// unlimited concurrency, preserving the historical behaviour. A positive value
// caps the number of in-flight HTTP fetches across the recursive sitemap-index
// traversal, which is recommended for very large sitemap indexes to avoid
// goroutine and connection blow-up.
// The value must be greater than or equal to 0; negative values are ignored
// and an error is recorded.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxConcurrency(maxConcurrency int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxConcurrency < 0 {
		s.errs = append(s.errs, fmt.Errorf("maxConcurrency must be >= 0, got %d", maxConcurrency))
		return s
	}
	s.cfg.maxConcurrency = maxConcurrency

	return s
}

// SetFollow sets the follow patterns using the provided list of regex strings and compiles them into regex objects.
// Patterns longer than maxRegexPatternLength characters are rejected with an error.
// Any errors encountered during compilation are appended to the error list in the struct.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFollow(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.follow = regexes
	s.cfg.followRegexes = nil
	for _, followPattern := range s.cfg.follow {
		if len(followPattern) > maxRegexPatternLength {
			s.errs = append(s.errs, fmt.Errorf("follow pattern exceeds maximum length of %d characters (%d)", maxRegexPatternLength, len(followPattern)))
			continue
		}
		re, err := regexp.Compile(followPattern)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		s.cfg.followRegexes = append(s.cfg.followRegexes, re)
	}

	return s
}

// SetRules sets the rules patterns using the provided list of regex strings and compiles them into regex objects.
// Patterns longer than maxRegexPatternLength characters are rejected with an error.
// Any errors encountered during compilation are appended to the error list in the struct.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetRules(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.rules = regexes
	s.cfg.rulesRegexes = nil
	for _, rulePattern := range s.cfg.rules {
		if len(rulePattern) > maxRegexPatternLength {
			s.errs = append(s.errs, fmt.Errorf("rules pattern exceeds maximum length of %d characters (%d)", maxRegexPatternLength, len(rulePattern)))
			continue
		}
		re, err := regexp.Compile(rulePattern)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		s.cfg.rulesRegexes = append(s.cfg.rulesRegexes, re)
	}
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

// Parse is a method of the S structure. It parses the given URL and its content.
//
// Parse is a backward-compatible wrapper around ParseContext that uses
// context.Background(). For new code, prefer ParseContext so that callers
// can propagate cancellation and deadlines.
//
// If the S object has any errors, it returns an error with the message
// "errors occurred before parsing, see GetErrors() for details".
// It sets the mainURL field to the given URL and the mainURLContent field to
// the given URL content. It returns an error if there was an error setting
// the content.
// If the URL ends with "/robots.txt", it parses the robots.txt file and
// fetches URLs from the sitemap files mentioned in the robots.txt.
// If the URL does not end with "/robots.txt", the mainURLContent is checked
// and unzipped if necessary, then parsed and fetched.
// It returns the S structure and nil error if the method was able to complete
// successfully.
func (s *S) Parse(url string, urlContent *string) (*S, error) {
	return s.ParseContext(context.Background(), url, urlContent)
}

// ParseContext parses the given URL and its content, honoring the supplied
// context for cancellation and deadlines.
//
// The context is propagated through every HTTP request issued by the parser
// (both the initial fetch and the recursive sitemap-index/urlset fetches),
// so cancelling ctx aborts in-flight downloads and prevents new ones from
// starting. Already-parsed URLs accumulated in s.urls before cancellation
// remain available via GetURLs(); the cancellation cause is recorded in the
// error list and is also returned by ParseContext.
//
// All other semantics match Parse.
func (s *S) ParseContext(ctx context.Context, url string, urlContent *string) (*S, error) {
	s.parseMu.Lock()
	defer s.parseMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	var err error
	var wg sync.WaitGroup

	s.mu.Lock()
	if len(s.errs) > 0 {
		s.mu.Unlock()
		return s, errors.New("errors occurred before parsing, see GetErrors() for details")
	}

	if urlContent == nil {
		parsedURL, err := neturl.Parse(url)
		if err != nil {
			s.errs = append(s.errs, fmt.Errorf("invalid URL: %w", err))
			s.mu.Unlock()
			return s, s.errs[len(s.errs)-1]
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			err := fmt.Errorf("invalid URL scheme %q: only http and https are supported", parsedURL.Scheme)
			s.errs = append(s.errs, err)
			s.mu.Unlock()
			return s, err
		}
		if parsedURL.Host == "" {
			err := fmt.Errorf("invalid URL: missing host")
			s.errs = append(s.errs, err)
			s.mu.Unlock()
			return s, err
		}
	}

	s.robotsTxtSitemapURLs = nil
	s.sitemapLocations = nil
	s.urls = nil
	s.errs = nil

	if s.cfg.maxConcurrency > 0 {
		s.sem = make(chan struct{}, s.cfg.maxConcurrency)
	} else {
		s.sem = nil
	}
	s.mu.Unlock()

	s.mainURL = url
	s.mainURLContent, err = s.setContent(ctx, urlContent)
	if err != nil {
		s.mu.Lock()
		s.errs = append(s.errs, err)
		s.mu.Unlock()
		return s, err
	}

	if strings.HasSuffix(s.mainURL, "/robots.txt") {
		s.parseRobotsTXT(s.mainURLContent)

		s.mu.Lock()
		for _, robotsTXTSitemapURL := range s.robotsTxtSitemapURLs {
			wg.Add(1)
			rTXTsmURL := robotsTXTSitemapURL
			go func() {
				defer wg.Done()

				// acquireSlot also honours ctx cancellation, so a single check
				// here covers both the unlimited-concurrency and bounded paths.
				if err := s.acquireSlot(ctx); err != nil {
					s.mu.Lock()
					s.errs = append(s.errs, err)
					s.mu.Unlock()
					return
				}

				robotsTXTSitemapContent, err := s.fetch(ctx, rTXTsmURL)
				s.releaseSlot()
				if err != nil {
					s.mu.Lock()
					s.errs = append(s.errs, err)
					s.mu.Unlock()
					return
				}

				s.mu.Lock()
				robotsTXTSitemapContent = s.checkAndUnzipContent(robotsTXTSitemapContent)
				locations := s.parse(rTXTsmURL, string(robotsTXTSitemapContent))
				s.mu.Unlock()

				if s.cfg.multiThread {
					s.parseAndFetchUrlsMultiThread(ctx, locations, 0)
				} else {
					s.parseAndFetchUrlsSequential(ctx, locations, 0)
				}
			}()
		}
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		mainURLContent := s.checkAndUnzipContent([]byte(s.mainURLContent))
		s.mainURLContent = string(mainURLContent)
		locations := s.parse(s.mainURL, s.mainURLContent)
		s.mu.Unlock()

		if s.cfg.multiThread {
			s.parseAndFetchUrlsMultiThread(ctx, locations, 0)
		} else {
			s.parseAndFetchUrlsSequential(ctx, locations, 0)
		}
	}

	wg.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return s, ctxErr
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
// If the S object is nil, an empty slice is returned.
// The function creates a copy of the original URLs list and randomly selects n URLs from it, removing them to avoid duplicates.
// The selected URLs are returned as a new slice.
func (s *S) GetRandomURLs(n int) []URL {
	if s == nil {
		return []URL{}
	}

	s.mu.Lock()
	originalURLs := make([]URL, len(s.urls))
	copy(originalURLs, s.urls)
	s.mu.Unlock()

	randURLs := make([]URL, 0, n)

	for i := 0; i < n; i++ {
		if len(originalURLs) == 0 {
			break
		}

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
// The supplied context is propagated to the underlying HTTP request when fetching is required.
func (s *S) setContent(ctx context.Context, urlContent *string) (string, error) {
	if urlContent != nil {
		return *urlContent, nil
	}
	mainURLContent, err := s.fetch(ctx, s.mainURL)

	if err != nil {
		return "", err
	}
	return string(mainURLContent), nil
}

// parseRobotsTXT retrieves the sitemap URLs from the provided robots.txt content.
// It splits the content into lines and checks for lines beginning with "Sitemap:"
// (case-insensitive). UTF-8 BOM at the beginning of the file is stripped, lines
// starting with "#" are treated as comments and skipped, and any inline comment
// (text following an unescaped "#") is removed before extracting the URL.
// If a valid URL is found, it is appended to the robotsTxtSitemapURLs slice.
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
			s.robotsTxtSitemapURLs = append(s.robotsTxtSitemapURLs, url)
		}
	}
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
// It returns the content as a []byte and an error if there was a problem fetching the URL.
// The HTTP status must be 200 (OK) for the request to be successful.
// The response body is automatically closed after reading using a defer statement.
// The supplied context is attached to the HTTP request, so cancelling it aborts
// the in-flight transfer.
func (s *S) fetch(ctx context.Context, url string) ([]byte, error) {
	var body bytes.Buffer

	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	fetchTimeout := s.cfg.fetchTimeout
	userAgent := s.cfg.userAgent
	maxResponseSize := s.cfg.maxResponseSize
	s.mu.Unlock()

	client := &http.Client{
		Timeout: time.Duration(fetchTimeout) * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received HTTP status %d", response.StatusCode)
	}

	_, err = io.Copy(&body, io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}

	if int64(body.Len()) > maxResponseSize {
		return nil, fmt.Errorf("response size exceeds limit of %d bytes", maxResponseSize)
	}

	return body.Bytes(), nil
}

// checkAndUnzipContent checks if the content is a gzip file and unzips it if necessary
// If the content is a gzip file, it returns the uncompressed content.
// If an error occurs during unzipping or checking, it returns the original content.
// It updates the internal error list if an error occurs while unzipping.
//
// Param content: The content to be checked and possibly unzipped
// Return []byte: The checked and possibly uncompressed content
func (s *S) checkAndUnzipContent(content []byte) []byte {
	gzipPrefix := []byte("\x1f\x8b\x08")
	if bytes.HasPrefix(content, gzipPrefix) {
		uncompressed, err := unzip(content)
		if err != nil {
			s.errs = append(s.errs, err)
			// return the original content if error
			return content
		}
		content = uncompressed
	}
	return content
}

// parseAndFetchUrlsMultiThread concurrently parses and fetches the URLs specified in the "locations" parameter.
// It uses a sync.WaitGroup to wait for all fetch operations to complete.
// For each location, it starts a goroutine that fetches the content using the fetch method of the S structure.
// If there is an error during the fetch operation, the error is appended to the "errs" field of the S structure.
// The fetched content is then checked and uncompressed using the checkAndUnzipContent method of the S structure.
// Finally, the uncompressed content is passed to the parse method of the S structure.
// This method does not return any value.
func (s *S) parseAndFetchUrlsMultiThread(ctx context.Context, locations []string, depth int) {
	if depth >= s.cfg.maxDepth {
		s.mu.Lock()
		s.errs = append(s.errs, fmt.Errorf("max recursion depth of %d reached", s.cfg.maxDepth))
		s.mu.Unlock()
		return
	}
	var wg sync.WaitGroup
	for _, location := range locations {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)

		loc := location
		go func() {
			defer wg.Done()
			// acquireSlot also honours ctx cancellation, so a single check
			// here covers both the unlimited-concurrency and bounded paths.
			if err := s.acquireSlot(ctx); err != nil {
				s.mu.Lock()
				s.errs = append(s.errs, err)
				s.mu.Unlock()
				return
			}
			content, err := s.fetch(ctx, loc)
			s.releaseSlot()
			if err != nil {
				s.mu.Lock()
				s.errs = append(s.errs, err)
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			content = s.checkAndUnzipContent(content)
			parsedLocations := s.parse(loc, string(content))
			s.mu.Unlock()
			if len(parsedLocations) > 0 {
				s.parseAndFetchUrlsMultiThread(ctx, parsedLocations, depth+1)
			}
		}()
	}
	wg.Wait()
}

// parseAndFetchUrlsSequential sequentially parses and fetches the URLs specified in the "locations" parameter.
// For each location, it fetches the content using the fetch method of the S structure.
// If there is an error during the fetch operation, the error is appended to the "errs" field of the S structure.
// The fetched content is then checked and uncompressed using the checkAndUnzipContent method of the S structure.
// Finally, the uncompressed content is passed to the parse method of the S structure.
// This method does not return any value.
func (s *S) parseAndFetchUrlsSequential(ctx context.Context, locations []string, depth int) {
	if depth >= s.cfg.maxDepth {
		s.mu.Lock()
		s.errs = append(s.errs, fmt.Errorf("max recursion depth of %d reached", s.cfg.maxDepth))
		s.mu.Unlock()
		return
	}
	for _, location := range locations {
		if ctx.Err() != nil {
			return
		}
		content, err := s.fetch(ctx, location)
		if err != nil {
			s.mu.Lock()
			s.errs = append(s.errs, err)
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		content = s.checkAndUnzipContent(content)
		parsedLocations := s.parse(location, string(content))
		s.mu.Unlock()
		if len(parsedLocations) > 0 {
			s.parseAndFetchUrlsSequential(ctx, parsedLocations, depth+1)
		}
	}
}

// detectRootElement reads the first XML start element from the content
// to determine the document type without fully parsing it.
// Returns the local name of the root element, or an empty string if detection fails.
func detectRootElement(content string) string {
	decoder := xml.NewDecoder(bytes.NewReader([]byte(content)))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if se, ok := token.(xml.StartElement); ok {
			return se.Name.Local
		}
	}
}

// parse parses the provided URL and its content.
// It determines whether the content is a sitemap index or a sitemap by inspecting
// the root XML element, then only invokes the appropriate parser.
// If it is a sitemap index, it adds the URLs from the sitemap index to the sitemap locations.
// If it is a sitemap, it adds the URLs from the sitemap to the URL list.
// Parsing errors are added to the error list.
// It returns a slice of sitemap locations that were added.
func (s *S) parse(url string, content string) []string {
	var sitemapLocationsAdded []string

	rootElement := detectRootElement(content)

	switch rootElement {
	case "sitemapindex":
		smIndex, err := s.parseSitemapIndex(content)
		if err != nil {
			s.errs = append(s.errs, err)
			return sitemapLocationsAdded
		}
		s.sitemapLocations = append(s.sitemapLocations, url)
		for _, sitemapIndexSitemap := range smIndex.Sitemap {
			sitemapIndexSitemap.Loc = strings.TrimSpace(sitemapIndexSitemap.Loc)
			resolvedLoc, err := s.resolveAndValidateLoc(sitemapIndexSitemap.Loc, url)
			if err != nil {
				s.errs = append(s.errs, err)
				continue
			}
			sitemapIndexSitemap.Loc = resolvedLoc
			// Check if the sitemapIndexSitemap.Loc matches any of the regular expressions in s.cfg.followRegexes.
			matches := false
			if len(s.cfg.followRegexes) > 0 {
				for _, re := range s.cfg.followRegexes {
					if re.MatchString(sitemapIndexSitemap.Loc) {
						matches = true
						break
					}
				}
			} else {
				matches = true
			}
			if !matches {
				continue
			}
			sitemapLocationsAdded = append(sitemapLocationsAdded, sitemapIndexSitemap.Loc)
			s.sitemapLocations = append(s.sitemapLocations, sitemapIndexSitemap.Loc)
		}

	case "urlset":
		urlSet, err := s.parseURLSet(content)
		if err != nil {
			s.errs = append(s.errs, err)
			return sitemapLocationsAdded
		}
		for _, urlSetURL := range urlSet.URL {
			urlSetURL.Loc = strings.TrimSpace(urlSetURL.Loc)
			resolvedLoc, err := s.resolveAndValidateLoc(urlSetURL.Loc, url)
			if err != nil {
				s.errs = append(s.errs, err)
				continue
			}
			urlSetURL.Loc = resolvedLoc
			if err := s.validatePriority(urlSetURL.Priority); err != nil {
				s.errs = append(s.errs, err)
				continue
			}
			// Check if the urlSetURL.Loc matches any of the regular expressions in s.cfg.rulesRegexes.
			matches := false
			if len(s.cfg.rulesRegexes) > 0 {
				for _, re := range s.cfg.rulesRegexes {
					if re.MatchString(urlSetURL.Loc) {
						matches = true
						break
					}
				}
			} else {
				matches = true
			}
			if !matches {
				continue
			}
			s.urls = append(s.urls, urlSetURL)
		}

	default:
		// Unknown root element: report a single error
		if len(content) == 0 {
			s.errs = append(s.errs, fmt.Errorf("sitemap content is empty"))
		} else {
			s.errs = append(s.errs, fmt.Errorf("unrecognized sitemap format (root element: %q)", rootElement))
		}
	}

	return sitemapLocationsAdded
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

	decoder := xml.NewDecoder(bytes.NewReader([]byte(data)))
	decoder.CharsetReader = charset.NewReaderLabel

	err := decoder.Decode(&smIndex)
	return smIndex, err

}

// parseURLSet takes a string of XML data representing a sitemap and parses it into a URLSet.
// If the data is empty, it returns an error with the message "sitemap is empty".
// It uses an xml.Decoder with charset support to decode the XML data into the URLSet struct.
// If there is an error during decoding, it returns the empty URLSet and the decode error.
// Otherwise, it returns the parsed URLSet and nil error.
func (s *S) parseURLSet(data string) (URLSet, error) {
	var urlSet URLSet
	if len(data) == 0 {
		return urlSet, fmt.Errorf("sitemap is empty")
	}

	decoder := xml.NewDecoder(bytes.NewReader([]byte(data)))
	decoder.CharsetReader = charset.NewReaderLabel

	err := decoder.Decode(&urlSet)
	return urlSet, err
}

// maxLocLength is the maximum URL length allowed in a sitemap <loc> element per the sitemaps.org specification.
const maxLocLength = 2048

// maxRegexPatternLength is the maximum allowed length of a regex pattern string passed to SetFollow or SetRules.
// Go's regexp package uses RE2 semantics and is therefore not vulnerable to catastrophic backtracking,
// but arbitrarily long patterns can still produce large compiled automata and consume significant memory.
const maxRegexPatternLength = 1000

// validatePriority validates the <priority> value of a URL entry.
// In strict mode, the value must be between 0.0 and 1.0 inclusive per the sitemaps.org specification.
// In tolerant mode, any value is accepted and nil is returned.
func (s *S) validatePriority(priority *float32) error {
	if !s.cfg.strict || priority == nil {
		return nil
	}
	if *priority < 0.0 || *priority > 1.0 {
		return fmt.Errorf("strict mode: priority %g is out of range [0.0, 1.0]", *priority)
	}
	return nil
}

// resolveAndValidateLoc resolves and validates a <loc> URL found in a sitemap.
// In tolerant mode (strict=false), relative URLs are resolved against baseURL.
// In strict mode (strict=true), URLs must be absolute HTTP(S), on the same host
// and protocol as baseURL, and no longer than 2048 characters.
// Returns the resolved URL string and an error if validation fails.
func (s *S) resolveAndValidateLoc(loc string, baseURL string) (string, error) {
	base, err := neturl.Parse(baseURL)
	if err != nil {
		return loc, fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}

	parsed, err := neturl.Parse(loc)
	if err != nil {
		return loc, fmt.Errorf("invalid URL %q: %w", loc, err)
	}

	if s.cfg.strict {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return loc, fmt.Errorf("strict mode: URL %q has unsupported scheme %q", loc, parsed.Scheme)
		}
		if parsed.Host == "" {
			return loc, fmt.Errorf("strict mode: URL %q is missing host", loc)
		}
		if parsed.Scheme != base.Scheme {
			return loc, fmt.Errorf("strict mode: URL %q has scheme %q, expected %q (same as sitemap)", loc, parsed.Scheme, base.Scheme)
		}
		if parsed.Host != base.Host {
			return loc, fmt.Errorf("strict mode: URL %q has host %q, expected %q (same as sitemap)", loc, parsed.Host, base.Host)
		}
		if len(loc) > maxLocLength {
			return loc, fmt.Errorf("strict mode: URL exceeds %d characters (%d)", maxLocLength, len(loc))
		}
		return loc, nil
	}

	// Tolerant mode: resolve relative URLs against the base
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return loc, fmt.Errorf("resolved URL %q has unsupported scheme %q", resolved.String(), resolved.Scheme)
	}

	return resolved.String(), nil
}

// unzip decompresses the given content using gzip compression.
// It returns the uncompressed content and any error encountered during decompression.
// If the gzip header is invalid, the original content is returned together with the error.
// If decompression fails mid-stream (e.g. truncated/corrupted gzip data), the partially
// decompressed bytes are returned together with the error so the caller can decide how to react.
// In all error cases a non-nil error is returned; callers must not silently use the data.
func unzip(content []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(content))
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

	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		return uncompressed, fmt.Errorf("gzip decompression failed: %w", err)
	}

	return uncompressed, nil
}

func (l *lastModTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	err := d.DecodeElement(&v, &start)
	if err != nil {
		return err
	}

	formats := []string{
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

	v = strings.TrimSpace(v)

	// An empty <lastmod> element (or one containing only whitespace) is common
	// in real-world sitemaps. Treat it as "not set" rather than an error: leave
	// the zero value in place and let the caller decide how to interpret it.
	if v == "" {
		return nil
	}

	for _, format := range formats {
		parsedTime, parseErr := time.Parse(format, v)
		if parseErr == nil {
			*l = lastModTime{parsedTime}
			return nil
		}
	}

	return fmt.Errorf("unsupported lastmod format: %q", v)
}

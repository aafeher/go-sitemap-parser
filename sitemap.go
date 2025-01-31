package sitemap

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"golang.org/x/net/html/charset"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
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
		cfg                  config
		mainURL              string
		mainURLContent       string
		robotsTxtSitemapURLs []string
		sitemapLocations     []string
		urls                 []URL
		errs                 []error
	}

	// config is a structure that holds configuration settings.
	// It contains a userAgent field of type string, which represents the User-Agent header value for HTTP requests.
	// The fetchTimeout field of type uint8 represents the timeout value (in seconds) for fetching data.
	// The multiThread field of type bool determines whether to use multi-threading for fetching URLs.
	// The follow field is a slice of strings that contains regular expressions to match URLs to follow.
	// The followRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the follow field.
	// The rules field is a slice of strings that contains regular expressions to match URLs to include.
	// The rulesRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the rules field.
	config struct {
		userAgent     string
		fetchTimeout  uint8
		multiThread   bool
		follow        []string
		followRegexes []*regexp.Regexp
		rules         []string
		rulesRegexes  []*regexp.Regexp
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
		ChangeFreq *urlChangeFreq `xml:"changefreq"`
		Priority   *float32       `xml:"priority"`
	}

	lastModTime struct {
		time.Time
	}

	// URLChangeFreq represents the frequency at which a URL should be crawled and indexed.
	// Possible values are: "always", "hourly", "daily", "weekly", "monthly", "yearly", and "never".
	urlChangeFreq string
)

const (
	// changeFreqAlways is a constant representing the "always" value for URLChangeFreq.
	changeFreqAlways urlChangeFreq = "always"

	// changeFreqHourly is a constant representing the "hourly" value for URLChangeFreq.
	changeFreqHourly urlChangeFreq = "hourly"

	// changeFreqDaily is a constant representing the "daily" value for URLChangeFreq.
	changeFreqDaily urlChangeFreq = "daily"

	// changeFreqWeekly is a constant representing the "weekly" value for URLChangeFreq.
	changeFreqWeekly urlChangeFreq = "weekly"

	// changeFreqMonthly is a constant representing the "monthly" value for URLChangeFreq.
	changeFreqMonthly urlChangeFreq = "monthly"

	// changeFreqYearly is a constant representing the "yearly" value for URLChangeFreq.
	changeFreqYearly urlChangeFreq = "yearly"

	// changeFreqNever is a constant representing the "never" value for URLChangeFreq.
	changeFreqNever urlChangeFreq = "never"
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
		userAgent:    "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
		fetchTimeout: 3,
		multiThread:  true,
		follow:       []string{},
		rules:        []string{},
	}
}

// SetUserAgent sets the user agent for the Sitemap Parser.
// The user agent is used for making HTTP requests when parsing and fetching URLs.
// It should be a string representing the user agent header value.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetUserAgent(userAgent string) *S {
	s.cfg.userAgent = userAgent

	return s
}

// SetFetchTimeout sets the fetch timeout for the Sitemap Parser.
// The fetch timeout determines how long the parser will wait for an HTTP request to complete.
// It should be specified in seconds as an uint8 value.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFetchTimeout(fetchTimeout uint8) *S {
	s.cfg.fetchTimeout = fetchTimeout

	return s
}

// SetMultiThread sets the multi-threading for the Sitemap Parser.
// The multi-threading flag determines whether the parser should fetch URLs concurrently using goroutines.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMultiThread(multiThread bool) *S {
	s.cfg.multiThread = multiThread

	return s
}

// SetFollow sets the follow patterns using the provided list of regex strings and compiles them into regex objects.
// Any errors encountered during compilation are appended to the error list in the struct.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFollow(regexes []string) *S {
	s.cfg.follow = regexes
	for _, followPattern := range s.cfg.follow {
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
// Any errors encountered during compilation are appended to the error list in the struct.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetRules(regexes []string) *S {
	s.cfg.rules = regexes
	for _, rulePattern := range s.cfg.rules {
		re, err := regexp.Compile(rulePattern)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		s.cfg.rulesRegexes = append(s.cfg.rulesRegexes, re)
	}
	return s
}

// Parse is a method of the S structure. It parses the given URL and its content.
// If the S object has any errors, it returns an error with the message "errors occurred before parsing, see GetErrors() for details".
// It sets the mainURL field to the given URL and the mainURLContent field to the given URL content.
// It returns an error if there was an error setting the content.
// If the URL ends with "/robots.txt", it parses the robots.txt file and fetches URLs from the sitemap files mentioned in the robots.txt.
// The URLs are fetched concurrently using goroutines and the wait group wg.
// If there was an error fetching a sitemap file, the error is appended to the errs field.
// The fetched content is checked and unzipped if necessary.
// The fetched sitemap file URLs are parsed and fetched.
// If the URL does not end with "/robots.txt", the mainURLContent is checked and unzipped if necessary.
// The mainURLContent is then parsed and fetched.
// After all URLs are fetched and parsed, the method waits for all goroutines to complete using wg.Wait().
// It returns the S structure and nil error if the method was able to complete successfully.
func (s *S) Parse(url string, urlContent *string) (*S, error) {
	var err error
	var mu sync.Mutex
	var wg sync.WaitGroup

	if len(s.errs) > 0 {
		return s, errors.New("errors occurred before parsing, see GetErrors() for details")
	}

	s.mainURL = url
	s.mainURLContent, err = s.setContent(urlContent)
	if err != nil {
		s.errs = append(s.errs, err)
		return s, err
	}

	if strings.HasSuffix(s.mainURL, "/robots.txt") {
		s.parseRobotsTXT(s.mainURLContent)

		for _, robotsTXTSitemapURL := range s.robotsTxtSitemapURLs {
			wg.Add(1)
			rTXTsmURL := robotsTXTSitemapURL
			go func() {
				defer wg.Done()

				mu.Lock()
				defer mu.Unlock()

				robotsTXTSitemapContent, err := s.fetch(rTXTsmURL)
				if err != nil {
					s.errs = append(s.errs, err)
					return
				}
				robotsTXTSitemapContent = s.checkAndUnzipContent(robotsTXTSitemapContent)

				if s.cfg.multiThread {
					s.parseAndFetchUrlsMultiThread(s.parse(rTXTsmURL, string(robotsTXTSitemapContent)))
				} else {
					s.parseAndFetchUrlsSequential(s.parse(rTXTsmURL, string(robotsTXTSitemapContent)))
				}
			}()
		}
	} else {
		mainURLContent := s.checkAndUnzipContent([]byte(s.mainURLContent))
		s.mainURLContent = string(mainURLContent)
		if s.cfg.multiThread {
			s.parseAndFetchUrlsMultiThread(s.parse(s.mainURL, s.mainURLContent))
		} else {
			s.parseAndFetchUrlsSequential(s.parse(s.mainURL, s.mainURLContent))
		}
	}

	wg.Wait()

	return s, nil
}

func (s *S) GetErrorsCount() int64 {
	if s == nil {
		return 0
	}
	return int64(len(s.errs))
}

func (s *S) GetErrors() []error {
	if s == nil {
		return nil
	}
	return s.errs
}

// GetURLs returns the list of parsed URLs.
func (s *S) GetURLs() []URL {
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

	originalURLs := s.urls

	randURLs := make([]URL, 0, n)

	for i := 0; i < n; i++ {
		if len(originalURLs) == 0 {
			break
		}

		index := rand.Intn(len(originalURLs))
		randURLs = append(randURLs, originalURLs[index])

		// Remove the selected URL from the original list to avoid duplicates
		originalURLs[index] = originalURLs[len(originalURLs)-1] // Replace it with the last one.
		originalURLs = originalURLs[:len(originalURLs)-1]       // Remove last element.
	}

	return randURLs
}

// setContent extracts the main URL content or returns the provided URL content if not nil.
// It returns the extracted content as a string or an error if there was a problem fetching the content.
func (s *S) setContent(urlContent *string) (string, error) {
	if urlContent != nil {
		return *urlContent, nil
	}
	mainURLContent, err := s.fetch(s.mainURL)

	if err != nil {
		return "", err
	}
	return string(mainURLContent), nil
}

// parseRobotsTXT retrieves the sitemap URLs from the provided robots.txt content.
// It splits the content into lines and checks for lines beginning with "Sitemap: ".
// If a line matches, it extracts the URL and adds it to the robotsTxtSitemapURLs slice.
// The method does not return any values, but it updates the robotsTxtSitemapURLs field of the S struct.
func (s *S) parseRobotsTXT(robotsTXTContent string) {
	lines := strings.Split(robotsTXTContent, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "Sitemap: ") {
			continue
		}
		url := strings.Split(line, "Sitemap: ")[1]
		s.robotsTxtSitemapURLs = append(s.robotsTxtSitemapURLs, url)
	}
}

// fetch retrieves the content of the specified URL using an HTTP GET request.
// It returns the content as a []byte and an error if there was a problem fetching the URL.
// The HTTP status must be 200 (OK) for the request to be successful.
// The response body is automatically closed after reading using a defer statement.
func (s *S) fetch(url string) ([]byte, error) {
	var body bytes.Buffer

	client := &http.Client{
		Timeout: time.Duration(s.cfg.fetchTimeout) * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", s.cfg.userAgent)

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received HTTP status %d", response.StatusCode)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	_, err = io.Copy(&body, response.Body)
	if err != nil {
		return nil, err
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
		uncompressed, err := s.unzip(content)
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
func (s *S) parseAndFetchUrlsMultiThread(locations []string) {
	var wg sync.WaitGroup
	for _, location := range locations {
		wg.Add(1)

		loc := location
		go func() {
			defer wg.Done()
			content, err := s.fetch(loc)
			if err != nil {
				s.errs = append(s.errs, err)
				return
			}
			content = s.checkAndUnzipContent(content)
			parsedLocations := s.parse(loc, string(content))
			if len(parsedLocations) > 0 {
				s.parseAndFetchUrlsMultiThread(parsedLocations)
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
func (s *S) parseAndFetchUrlsSequential(locations []string) {
	for _, location := range locations {
		content, err := s.fetch(location)
		if err != nil {
			s.errs = append(s.errs, err)
			continue
		}
		content = s.checkAndUnzipContent(content)
		parsedLocations := s.parse(location, string(content))
		if len(parsedLocations) > 0 {
			s.parseAndFetchUrlsSequential(parsedLocations)
		}
	}
}

// parse parses the provided URL and its content.
// It determines whether the content is a sitemap index or a sitemap.
// If it is a sitemap index, it adds the URLs from the sitemap index to the sitemap locations.
// If it is a sitemap, it adds the URLs from the sitemap to the URL list.
// Parsing errors are added to the error list.
// It returns a slice of sitemap locations that were added.
func (s *S) parse(url string, content string) []string {
	smIndex, errSitemapIndex := s.parseSitemapIndex(content)
	urlSet, errURLSet := s.parseURLSet(content)

	var sitemapLocationsAdded []string

	if smIndex.Sitemap != nil {
		// SitemapIndex
		s.sitemapLocations = append(s.sitemapLocations, url)
		for _, sitemapIndexSitemap := range smIndex.Sitemap {
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
	} else if len(urlSet.URL) > 0 {
		// URLSet
		for _, urlSetURL := range urlSet.URL {
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
	}

	if errSitemapIndex != nil && len(urlSet.URL) == 0 {
		s.errs = append(s.errs, errSitemapIndex)
	}
	if errURLSet != nil && smIndex.Sitemap == nil {
		s.errs = append(s.errs, errURLSet)
	}

	return sitemapLocationsAdded
}

// parseSitemapIndex parses the sitemap index data and returns a SitemapIndex object and an error.
// The data parameter contains the XML data of the sitemap index.
// If the data is empty, it returns an error with the message "sitemapindex is empty".
// It uses the xml.Unmarshal function to unmarshal the XML data into a SitemapIndex object.
// The unmarshalling result is stored in the sitemapIndex variable.
// It returns the sitemapIndex object and any unmarshalling error that occurred.
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
// It uses the xml.Unmarshal function to unmarshal the XML data into the URLSet struct.
// If there is an error during unmarshalling, it returns the empty URLSet and the unmarshal error.
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

// unzip decompresses the given content using gzip compression.
// It returns the uncompressed content and any error encountered during decompression.
// If an error occurs and it is not `io.ErrUnexpectedEOF`, the original content is returned.
func (s *S) unzip(content []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return content, err
	}

	defer func(reader *gzip.Reader) {
		_ = reader.Close()
	}(reader)

	uncompressed, err := io.ReadAll(reader)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return content, err
	}

	return uncompressed, nil
}

// zip compresses the given content using gzip compression.
// It returns the compressed content as a byte array.
// If an error occurs during compression, it returns the original content and the error.
func (s *S) zip(content []byte) ([]byte, error) {
	writer := bytes.NewBuffer(nil)
	gzipWriter := gzip.NewWriter(writer)
	_, err := gzipWriter.Write(content)
	if err != nil {
		return content, err
	}
	err = gzipWriter.Close()
	if err != nil {
		return content, err
	}
	compressed := writer.Bytes()
	return compressed, nil
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

	var parsedTime time.Time
	for _, format := range formats {
		parsedTime, err = time.Parse(format, v)
		if err == nil {
			*l = lastModTime{parsedTime}
			return nil
		}
	}

	return err
}

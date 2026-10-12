// Package sitemap fetches and parses sitemaps and collects the URLs they list.
//
// A call to [S.Parse] or [S.ParseContext] takes the URL of a robots.txt, of a
// sitemap index or of a sitemap. It follows the sitemaps a robots.txt names
// and the ones a sitemap index lists, and collects the pages of every sitemap
// it reaches:
//
//	s := sitemap.New()
//	if _, err := s.Parse("https://www.sitemaps.org/sitemap.xml", nil); err != nil {
//		log.Fatal(err)
//	}
//	for _, u := range s.GetURLs() {
//		fmt.Println(u.Loc)
//	}
//
// The document at the URL can be handed to the call in place of nil. It is not
// fetched then; the sitemaps it lists are.
//
// The sitemaps the call found on its way, the ones a robots.txt names and a
// sitemap index lists, are returned by [S.GetSitemaps], whether or not they
// were fetched, and with the date a sitemap index gives for them, see
// [Sitemap].
//
// [S.GetURLs] returns the list the instance keeps, which is to be read and not
// to be changed. [S.URLs] is an iterator over the same URLs that yields each
// of them as a copy of the caller's own, and [S.Errors] is one over the errors
// [S.GetErrors] returns. A list of the URLs to sort or to change is collected
// from the iterator:
//
//	urls := slices.Collect(s.URLs())
//	slices.SortFunc(urls, func(a, b sitemap.URL) int {
//		return strings.Compare(a.Loc, b.Loc)
//	})
//
// # Formats
//
// A sitemap is a <urlset> of the sitemaps.org protocol, an RSS 2.0 feed, an
// Atom 1.0 feed, or a plain text file with a URL on a line. A sitemap and a
// sitemap index may be gzip compressed. An entry of a <urlset> is returned
// with its <lastmod>, <changefreq> and <priority>, and with what the image,
// news, video and hreflang extensions say about the page, see [URL]. Of a feed,
// the location of a page and the date it was last modified are taken: the
// <pubDate> of an RSS item, or else its <dc:date>, and the <updated> of an
// Atom entry, or else its <modified> or its <published>. A text file gives
// nothing but the location.
//
// # Configuration
//
// An instance is configured with its Set methods, which can be chained:
//
//	s := sitemap.New().
//		SetUserAgent("my-crawler/1.0").
//		SetFetchTimeout(10).
//		SetFollow([]string{`^https://example\.com/sitemaps/`})
//
// A Set method that is called with a value it does not accept records a
// [ConfigError]. Nothing is parsed while one is outstanding; calling the
// method again with a valid value clears it.
//
// The parser is in tolerant mode unless [S.SetStrict] says otherwise. Tolerant
// mode makes the best of a document that does not keep to the protocol: a
// relative URL, for one, is resolved against the URL of the sitemap. Strict
// mode holds a document to the protocol, and skips and reports the entries
// that do not keep to it.
//
// What a call may take up is bounded: the size of a response by
// [S.SetMaxResponseSize], the depth of nested sitemap indexes by
// [S.SetMaxDepth], the number of sitemaps it fetches and of sitemaps it lists
// by [S.SetMaxSitemaps], the number of URLs it collects by [S.SetMaxURLs], the
// number of validation errors it records by [S.SetMaxValidationErrors], and
// the sitemaps it works on at a time by [S.SetMaxConcurrency]. [S.SetFollow]
// restricts the sitemaps that are fetched, [S.SetRules] the URLs that are
// collected.
//
// A call requests the sitemaps the documents list wherever they are, and
// follows the redirects of its requests. Where the documents are not trusted,
// a client set with [S.SetHTTPClient] keeps the parser from the addresses of
// an internal network by checking the address of every connection it makes:
// SECURITY.md describes the check, and examples/ssrf of the repository is a
// client that makes it.
//
// # Errors
//
// Parse and ParseContext return an error when the call fails for the URL it
// was made for: the URL is not valid, or the document at it cannot be fetched
// or parsed. What goes wrong further on does not fail the call and is reported
// by [S.GetErrors] only: a sitemap that cannot be fetched or parsed, an entry
// that is not valid, a limit that is reached. A nil error therefore does not
// mean that nothing was skipped.
//
// The errors are of the types [ConfigError], [NetworkError], [ParseError] and
// [ValidationError], to be told apart with [errors.As].
//
// The error of a limit that was reached wraps the error that stands for the
// limit, so [errors.Is] tells which limit it was: [ErrMaxResponseSize],
// [ErrMaxDepth], [ErrMaxSitemaps], [ErrMaxURLs] or [ErrMaxValidationErrors].
//
//	for _, err := range s.GetErrors() {
//		if errors.Is(err, sitemap.ErrMaxURLs) {
//			log.Print("the URLs are not all there: ", err)
//		}
//	}
//
// # Concurrency
//
// The methods of an instance may be called from several goroutines. An
// instance carries out one Parse or ParseContext call at a time, a second one
// waits for the first to end; use an instance for each of the calls that are
// to run side by side. An instance can be used for any number of calls one
// after the other: its results are those of the most recent one.
package sitemap

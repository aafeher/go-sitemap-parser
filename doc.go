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
// # Formats
//
// A sitemap is a <urlset> of the sitemaps.org protocol, an RSS 2.0 feed, an
// Atom 1.0 feed, or a plain text file with a URL on a line. A sitemap and a
// sitemap index may be gzip compressed. An entry of a <urlset> is returned
// with its <lastmod>, <changefreq> and <priority>, and with what the image,
// news, video and hreflang extensions say about the page, see [URL]. Nothing
// but the location of a page is taken from a feed or a text file.
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
// [S.SetMaxDepth], the number of sitemaps it fetches by [S.SetMaxSitemaps],
// the number of URLs it collects by [S.SetMaxURLs], and the sitemaps it works
// on at a time by [S.SetMaxConcurrency]. [S.SetFollow] restricts the sitemaps
// that are fetched, [S.SetRules] the URLs that are collected.
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
// # Concurrency
//
// The methods of an instance may be called from several goroutines. An
// instance carries out one Parse or ParseContext call at a time, a second one
// waits for the first to end; use an instance for each of the calls that are
// to run side by side. An instance can be used for any number of calls one
// after the other: its results are those of the most recent one.
package sitemap

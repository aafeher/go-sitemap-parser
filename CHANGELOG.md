# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Package documentation: an overview of what the package does, the formats it reads, how it is configured, how errors are reported and what is safe to do concurrently. `go doc` and pkg.go.dev had nothing to show for the package itself.
- Documentation for `GetErrors()` and `GetErrorsCount()`, the two exported methods that had none: which errors are returned and for how long they are kept, their types and order, how they relate to the error `Parse()` / `ParseContext()` return, and that the slice returned must not be modified. The `Error()` methods of the four error types are documented with the message they return.

### Fixed
- Tolerant mode no longer returns a URL that is none. `net/url` checks the host of a URL only when the URL names its scheme, so a relative location that begins with `//` and gives something else than a host after it, such as `//::` or `//example.com:80:80/page`, was read without complaint and resolved to `https://::` or `https://example.com:80:80/page`, which `net/url` itself cannot parse. In a `<urlset>`, an RSS or an Atom feed such a location was returned by `GetURLs()`; in a sitemap index or a `robots.txt` it was reported as the `*NetworkError` of a request that could not be built. It is now skipped and reported as a `*ValidationError` that wraps the error of `net/url`: `validate "//::": parse "https://::": invalid port "::" after host`
- A location has to name a host in tolerant mode as well. `https:///page`, `https:page`, `https:` and `https://:8080/page`, which names a port but no host, passed for HTTP(S) URLs. A `<urlset>`, an RSS or an Atom feed returned them by `GetURLs()`, and so did a text sitemap, which takes a line that begins with `https://` for an entry. For a sitemap index or a `robots.txt` a request was attempted, see `Security` below for the one that names a port; the others were reported as the `*NetworkError` of the request (`http: no Host in request URL`). They are now skipped and reported as a `*ValidationError` (`validate "https:///page": missing host`) without a request
- A relative location that begins with `//` and names no host after it (`//`, `//?page=2`, `///page`) is rejected in tolerant mode with the same error. It was resolved to the host of the sitemap, and `//` to the URL of the sitemap itself: the sitemap was returned as one of its own pages, and a sitemap index or a `robots.txt` with such an entry was fetched a second time
- A port is no longer taken for a host. `https://:8080/sitemap.xml` names no host, but passed the check for one in three places: passed to `Parse()` / `ParseContext()` it was requested instead of being turned down with a `*ValidationError`, as the value of a `Sitemap:` line of a `robots.txt` it was requested in strict mode, and as the URL of an image, of a video thumbnail or of an alternate link it was accepted in strict mode. All three report `missing host` now. As the `<loc>` of an entry strict mode rejected it before as well, for not being on the host of the sitemap; that error reads `strict mode: missing host` now
- `SetMaxResponseSize(math.MaxInt64)` no longer makes every response an empty one. A response is read one byte past the limit, the byte that tells whether it exceeds the limit. For the largest `int64` there is that sum overflowed to a negative number, and a read limited to a negative number of bytes reads nothing: every sitemap that was fetched was reported as `sitemap content is empty`, and `Parse()` failed with that `*ParseError` for the URL passed to it. The largest limit is now read without the extra byte, as it already was when gzip content is decompressed
- `examples/rss`, `examples/atom` and `examples/text` work again. They requested files of this repository that do not exist, and printed the `*NetworkError` of the `404` response. Each one now parses a document that is passed in directly, so it runs without network access, and shows which links of a feed and which lines of a text sitemap are taken for pages. `README.md` describes the same under `Formats supported` and links the three examples
- `examples/geturls`, `examples/getrandomurls`, `examples/rules` and `examples/advanced` print the value of `ChangeFreq` (`daily`) instead of the address of the pointer to it (`0xc000012345`)

### Security
- A URL that names a port but no host, such as `http://:8080/sitemap.xml`, is no longer requested. Go's HTTP client sends the request for such a URL to the machine the parser runs on. A sitemap index in tolerant mode, and a `robots.txt` in both modes, could therefore make the parser fetch from a local port without naming `localhost` or a loopback address, which a check of the host name in a custom `*http.Client` may not reckon with. The URL is rejected as a `*ValidationError` (`missing host`) before anything is requested, see `Fixed` above. `SECURITY.md` names the host among the conditions for a URL to be requested

## [1.2.0] - 2026-10-10

### Added
- `examples/follow`: runnable example of restricting the sitemaps that are fetched with `SetFollow()`, those a `robots.txt` names and those of a sitemap index
- `examples/redirect`: runnable example of parsing a sitemap that is reached through a redirect, in tolerant and in strict mode. `README.md` gained a matching `Redirects` section
- `examples/multithread`: runnable example of fetching the sitemaps of a `robots.txt` with multi-threading on and off. `README.md` describes what `SetMultiThread(false)` guarantees and links the example
- `examples/reuse`: runnable example of reusing one instance for several `Parse()` calls and of correcting an invalid setting. `README.md` gained a matching `Reusing an instance` section
- `examples/tolerant`: runnable example of how tolerant and strict mode treat a sitemap with invalid values, entries without a location and malformed XML in it
- `examples/errors` shows the error `Parse()` returns for a document that cannot be parsed, and the one `ParseContext()` returns for a call that was cut short
- `examples/encoding`: runnable example of parsing a sitemap that is not UTF-8 encoded. `README.md` gained a matching `Character encoding` section, which also lists the limitations (UTF-16 / UTF-32 and the HTTP `Content-Type` charset are not supported)
- `SetMaxSitemaps()` / `GetMaxSitemaps()`: limit on the number of sitemaps a `Parse()` / `ParseContext()` call fetches, on all levels together. Every sitemap the call requests counts, a sitemap listed more than once counts once, the document passed to `Parse()` does not count. `0` lifts the limit, a negative value is rejected with a `*ConfigError`. See `Changed` below for the default
- `SetMaxURLs()` / `GetMaxURLs()`: limit on the number of URLs a `Parse()` / `ParseContext()` call collects, for all sitemaps together. Entries that are not valid and URLs left out by `SetRules()` do not count. Once the limit is reached, what is left of the document is left out unread and the sitemaps that are left are not fetched. `0` lifts the limit, a negative value is rejected with a `*ConfigError`. See `Changed` below for the default
- `examples/maxsitemaps` and `examples/maxurls`: runnable examples of the two limits, with multi-threading on and off. `README.md` gained matching `Max sitemaps` and `Max URLs` sections

### Changed
- `SetMaxConcurrency()` bounds the sitemaps that are being fetched or parsed at the same time; it used to bound the fetches only. A sitemap takes up a slot from the moment its request is sent until its content is parsed, and the slot is given back before the sitemaps it lists are followed. The limit therefore also caps the CPU cores in use and the documents held in memory at once, now that the sitemaps are parsed in parallel (see `Fixed` below). Parsing in parallel takes more memory than parsing one sitemap at a time did: for an index of 96 sitemaps of 20,000 URLs each, the peak heap went from about 1.05 GB to about 1.35 GB at the default limit of 16, and stayed at 1.1 GB with `SetMaxConcurrency(2)`. `SetMaxConcurrency(0)` lets all the sitemaps of an index be parsed at once, which took about 2.4 GB in the same measurement
- **`SetFollow()` filters the sitemaps a `robots.txt` names as well**, see `Security` below. Patterns written with only the entries of a sitemap index in mind have to match the sitemaps and sitemap indexes named in the `robots.txt` too, otherwise those are no longer fetched
- The value of a `Sitemap:` line of a `robots.txt` is validated before it is fetched, the way the `<loc>` of a sitemap index entry is. A value that is not an HTTP or HTTPS URL is reported as a `*ValidationError` (`validate "ftp://example.com/sitemap.xml": unsupported scheme "ftp"`) without a request being attempted; it used to be reported as the `*NetworkError` of the failed request. A URL of more than 2,048 characters is rejected as well, it used to be fetched
- In tolerant mode a relative `Sitemap:` value in a `robots.txt` (`Sitemap: /sitemap.xml`) is resolved against the URL of the `robots.txt` and fetched. It used to fail with `unsupported protocol scheme ""`. Strict mode rejects it as not being absolute. A sitemap on another host than the `robots.txt` is accepted in both modes, as before
- In strict mode the URLs of a redirected sitemap are compared with the URL the sitemap was served from. URLs on the host and protocol the sitemap was requested at are therefore rejected when the redirect led to another host or protocol; they used to be the only ones accepted
- Errors about a sitemap that was reached through a redirect name the URL the sitemap was served from instead of the URL that was requested. This concerns `*ParseError` and the `*ValidationError` for an entry without a location. A failed fetch is still reported as a `*NetworkError` naming the requested URL
- **`Parse()` / `ParseContext()` fail when the document at the URL passed cannot be parsed.** They used to return a `nil` error for it, with the error in `GetErrors()` only, so a call for a page that is no sitemap at all (`unrecognized sitemap format (root element: "html")`), for an empty document, for broken XML or gzip content, or for content that expands beyond the size limit looked like a successful call that found no URLs. The error returned is the `*ParseError` that `GetErrors()` holds about the document; of two, as for gzip content that cannot be unzipped, the first. Only the document the call is made for fails the call: a sitemap it lists that cannot be fetched or parsed, an entry that is not valid and the depth limit being reached are reported via `GetErrors()` alone, as before. Code that stops on a non-`nil` error now stops on such a document; `README.md` gained a description of what the returned error means
- A call that is cut short by its context reports it the same way however the sitemaps are fetched: `ParseContext()` returns a `*ParseError` that names the URL passed and wraps the error of the context, and records that error in `GetErrors()` once for the call. It used to return the bare context error. `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)` match as before; a comparison with `==` no longer does, and never did when it was the request for the URL passed that was cut short. What `GetErrors()` held depended on how the sitemaps were fetched: one untyped `context canceled` error for every sitemap that was waiting for a `SetMaxConcurrency()` slot and for every sitemap a `robots.txt` lists, but none for the sitemaps of a sitemap index fetched with multi-threading off. Nothing is recorded any more for the sitemaps a call did not get to, and they are not fetched. A request that was cut short is still reported as the `*NetworkError` of its sitemap
- The `*ParseError` recorded when the `SetMaxDepth()` limit is reached names the sitemap index whose sitemaps are not followed, by the URL it was served from: `parse "https://example.com/sitemap-index-2.xml": max recursion depth of 10 reached`. Its `URL` used to be empty (`parse "": max recursion depth of 10 reached`), which left no way of telling where the parser stopped
- In strict mode an entry without a `<loc>` is reported as `validate "https://example.com/sitemap.xml": <loc> of an entry is empty or missing`, naming the sitemap it stands in, instead of `validate "": strict mode: unsupported scheme ""`
- In strict mode an RSS `<item>` without a `<link>` is no longer reported, the link of a feed item being optional. It is skipped without an error, as an Atom `<entry>` without a link already was
- Tolerant mode reads XML leniently: an unescaped `&` or an unknown entity such as `&nbsp;` is taken literally, and a missing end tag is made up for. Such a document used to be rejected as a whole with an XML syntax error. Strict mode still requires well-formed XML and rejects the document
- A value that cannot be parsed is reported as a `*ValidationError` for the page it belongs to (`validate "https://example.com/page": invalid <lastmod> value "2024-01-15 10:30:00"`) instead of a `*ParseError` for the whole sitemap
- A `Parse()` / `ParseContext()` call that returns early — because a configuration error is outstanding or the input URL is invalid — now leaves `GetURLs()` empty. Previously the URLs collected by the preceding call were still returned in that case, so a failed call could be mistaken for a successful one. `GetURLs()` and `GetErrors()` now always describe the most recent call only
- **A `Parse()` / `ParseContext()` call fetches at most 50,000 sitemaps and collects at most 10,000,000 URLs by default.** There was no limit on either, see `Security` below. A call that reaches a limit does not fail: it returns what it collected until then and records a `*ParseError` that names the URL passed to it, once for the call (`parse "https://example.com/sitemap-index.xml": limit of 50000 sitemaps reached`, `... limit of 10000000 URLs reached`). Calls for sites beyond these numbers have to raise the limits with `SetMaxSitemaps()` / `SetMaxURLs()`, or lift them with `0`, to get everything as before
- **In strict mode a `<changefreq>` has to be one of the values of the protocol, written as the protocol writes them** (`always`, `hourly`, `daily`, `weekly`, `monthly`, `yearly`, `never`). Strict mode did not look at the value at all: `Daily` and `sometimes` were accepted and returned as written. An entry with such a value is now skipped and reported, as one with a priority out of range is: `validate "https://example.com/page": strict mode: invalid <changefreq> value "Daily"`. When the change frequency and the priority of an entry are both not allowed, both are reported. Tolerant mode is not affected
- **In strict mode a URL with a space in it is rejected**: `validate "https://example.com/a b": strict mode: URL contains a space`. A URL has to give a space as `%20`; strict mode used to accept the URL and return it with the space. The same goes for a control character, a tab or a line break for one, in the fragment of a URL (`strict mode: URL contains a control character`); anywhere else in the URL it was rejected already. This applies to every URL strict mode validates: the `<loc>` of a `<url>` and of a `<sitemap>` entry, the link of an RSS item and of an Atom entry, the line of a text sitemap, the value of a `Sitemap:` line of a `robots.txt`, and the URL of an image, of the thumbnail of a video and of an alternate link
- In strict mode the URL of an image, of the thumbnail of a video and of an alternate link has to name a host, as a `<loc>` has to: `https:///image.jpg` is reported as `strict mode: missing host` and the image, video or link is left out. Only the protocol of these URLs was checked
- `golang.org/x/net` updated from v0.59.0 to v0.60.0
- CI: golangci-lint pinned to v2.14.0 instead of v2.13.2. The `stable` leg of the build matrix moved on to Go 1.27.2, with which v2.13.2 fails with `typecheck` errors (`export data version 5 is greater than maximum supported version 4`)

### Fixed
- Multi-threaded parsing runs in parallel. A fetched sitemap used to be unzipped and decoded with the internal mutex held, so the sitemaps were fetched concurrently but parsed one after the other, and multi-threading gained nothing but the time spent waiting for the network: on 16 cores, an index of 16 sitemaps of 60,000 URLs each took 5.8 s with multi-threading on and 5.8 s with it off. Every sitemap is now unzipped and decoded without the mutex, which is taken only to add what the sitemap yielded to the results. The same index takes 1.6 s with multi-threading on; with it off the time is unchanged
- The getters and setters no longer wait for a sitemap to be parsed. Called while `Parse()` / `ParseContext()` was running, `GetURLs()`, `GetURLCount()`, `GetErrors()` and every other method blocked until the sitemap being parsed was finished, and with several sitemaps waiting for the mutex until those were finished too: up to 5.3 s in the measurement above. They now wait only while the results of a sitemap are added (0.2 s at most in the same measurement, with close to a million URLs collected)
- Parsing a sitemap takes less memory. The content of a document used to be copied in full up to three times on its way through the parser, converted from bytes to a string and back, and the entries of a `<urlset>` were all decoded into a slice of their own before they were turned into the URLs that are returned. The content is now read into a string once and handed on as it is, and the entries of a `<urlset>` are processed one at a time while the document is read. For a `<urlset>` of 20 MB holding 155,000 URLs, the peak heap went from about 290 MB to about 150 MB when the document is fetched, and from about 235 MB to about 110 MB when it is passed to `Parse()` as content; the memory allocated on the way went from about 800 MB to about 610 MB. Less copying is less work too: the benchmark of an index of 16 sitemaps of 5,000 URLs each takes 69 ms instead of 124 ms with multi-threading on, and 247 ms instead of 323 ms with it off. What a document yields is unchanged, a `<urlset>` that cannot be read to its end still yields nothing but the error. A text sitemap gains little, its memory being taken up by the URLs themselves
- The content of the URL passed to `Parse()` / `ParseContext()` is no longer kept once it is parsed. The instance used to hold it until the next call: while the sitemaps it lists were fetched, and for as long as the results were in use afterwards, although nothing read it any more. For a sitemap index or a `robots.txt` of 16 MB that was 16 MB. Parts of a document could keep the whole of it in memory as well, for as long as a single one of them was in use: the URLs of a text sitemap parsed in strict mode, the lines of a text sitemap reported as invalid, and the `Sitemap:` values of a `robots.txt`. They are now copies of their own
- With multi-threading on, a goroutine is started for a sitemap only when a `SetMaxConcurrency()` slot is free for it. A goroutine used to be started at once for every sitemap a sitemap index or a `robots.txt` lists, to wait for a slot there, so the limit bounded the requests but not the goroutines: parsing an index of 5,000 sitemaps with `SetMaxConcurrency(4)` had about 4,850 goroutines running. It now has about 30, those of the HTTP client and of the test server included. With `SetMaxConcurrency(0)` there is a goroutine for every sitemap, as before
- A sitemap reached through a redirect is treated as located at the URL it was served from. The URL that was requested used to be taken for its location, so after a redirect from `http://example.com/sitemap.xml` to `https://www.example.com/sitemap.xml` strict mode rejected every URL of the sitemap (`strict mode: host "www.example.com" does not match sitemap host "example.com"`), and tolerant mode resolved relative URLs to the host that was left behind (`<loc>/page</loc>` to `http://example.com/page`). Relative URLs are now resolved against the URL the last redirect led to, and strict mode compares the URLs with it. This goes for the URL passed to `Parse()` as well as for the sitemaps of a sitemap index and of a `robots.txt`
- `SetMultiThread(false)` is honoured for the sitemaps a `robots.txt` lists. They used to be fetched concurrently whatever the setting, one goroutine and one request for every `Sitemap:` line, so a `robots.txt` listing eight sitemaps put eight requests in flight at once on a parser that was told to fetch one at a time. They are now fetched the way the sitemaps of a sitemap index are: one at a time and in the order listed when multi-threading is off, concurrently and bounded by `SetMaxConcurrency()` when it is on. The depth limit of `SetMaxDepth()` applies to them as before
- The documentation of `ParseContext()` is attached to `ParseContext()`. It stood above an unexported function, so the method was shown without any in the generated documentation
- `GetRandomURLs()` no longer panics with `makeslice: cap out of range` when `n` is negative or very large (e.g. `math.MaxInt`). A negative `n` yields an empty slice, an `n` beyond the number of parsed URLs yields all of them. The result is no longer allocated for `n` elements either: asking for 1,000,000 URLs of a sitemap of three used to reserve memory for a million
- Data race between `SetMaxDepth()` / `SetMultiThread()` and a running `Parse()` / `ParseContext()`. The two settings were read without the internal mutex while the sitemaps were being fetched, although calling a setter during a parse is documented as safe. Both are now read with the mutex held. Whether the sitemaps are fetched concurrently is decided once, when a call starts, so a `SetMultiThread()` call made during a parse applies to the next one
- An entry without a location no longer yields the URL of the sitemap itself. In tolerant mode a `<url>` whose `<loc>` is missing, empty or holds only whitespace used to be resolved like a relative URL, that is to the URL of the sitemap, which `GetURLs()` then returned as one of its pages. The same happened to an RSS `<item>` without a `<link>` and to an Atom `<link>` with a whitespace-only `href`. Such a `<url>` is now skipped and reported as a `*ValidationError` naming the sitemap; such a feed item is skipped without an error
- A `<sitemap>` entry without a `<loc>` in a sitemap index no longer makes the parser fetch the index itself, a second time or, when the content of the index was passed to `Parse()`, over the network for the first time. The entry is skipped and reported
- A single value that cannot be parsed no longer discards the whole sitemap. A malformed `<lastmod>` (e.g. `2024-01-15T10:30:00`, `2024-01-15T10:30:00+0100`, `2024-01-15 10:30:00`), `<priority>` (e.g. `0,5`), `<video:duration>`, `<video:rating>`, `<video:view_count>`, `<video:expiration_date>`, `<video:publication_date>` or `<news:publication_date>` in one entry used to fail the decoding of the entire document, so a sitemap of 50,000 URLs yielded none. Such a value now costs only itself: the field is left `nil`, the entry is kept and the value is reported. In strict mode an entry whose `<lastmod>` or `<priority>` cannot be parsed is skipped, as an entry with an out-of-range priority already was
- An unescaped `&` in a URL (`<loc>https://example.com/?a=1&b=2</loc>`) no longer discards the whole document in tolerant mode, and neither does an undefined entity such as `&nbsp;` in an RSS or Atom feed
- A numeric element holding only whitespace (`<priority> </priority>`) is read like an empty one instead of failing the document
- XML documents that declare an encoding other than UTF-8 are parsed again. Since v0.3.0 the root element detection that selects the parser used an XML decoder without charset support, so every sitemap index, urlset, RSS or Atom document with a declaration such as `encoding="ISO-8859-1"`, `"windows-1252"`, `"US-ASCII"` or `"utf8"` was rejected with `unrecognized sitemap format (root element: "")` — the charset support added in v0.1.5 could no longer be reached. Detection and parsing now share one charset-aware decoder
- A document that declares an encoding that cannot be transcoded is now reported with a `*ParseError` naming the encoding (`xml: opening charset "IBM437": unsupported charset: "IBM437"`) instead of `unrecognized sitemap format`
- An instance can be reused after a `Parse()` / `ParseContext()` call that recorded an error. Previously any error left over from a call — a failed fetch, an entry skipped during validation, an invalid input URL — made every later call on the same instance fail immediately with `errors occurred before parsing, see GetErrors() for details`, because the check for outstanding configuration errors also counted the errors of the previous call. Each call now discards the URLs and errors of the previous call before anything else
- Configuration errors can be corrected. A `*ConfigError` recorded by `SetFetchTimeout()`, `SetMaxResponseSize()`, `SetMaxDepth()`, `SetMaxConcurrency()`, `SetFollow()` or `SetRules()` used to stay in the error list for the lifetime of the instance, so a single invalid call made the instance permanently unusable even after the value was corrected. Each call to one of these setters now replaces the errors recorded by its previous call: a valid value clears them, and repeating an invalid value no longer accumulates duplicates
- An empty `<lastmod>` is read as absent: `LastMod` is `nil`, as the entry of v0.4.0 below says it is. It was a pointer to the zero time (`0001-01-01 00:00:00 UTC`) instead, so `if u.LastMod != nil` let through a date the sitemap never gave. The same goes for `<news:publication_date>`, `<video:expiration_date>` and `<video:publication_date>`, and for an element that holds only whitespace, an empty CDATA section or a comment. A date field that is set now always holds what the document gives. Code that told an empty element by `LastMod.IsZero()` has to check for `nil` instead. `examples/tolerant` shows an entry with an empty `<lastmod>`
- In strict mode a news entry whose `<news:publication_date>` is empty is reported: `validate "https://example.com/page": strict mode: news <publication_date> is empty`. The zero time the empty element was read as passed for the date the Google News extension requires, so only an entry without the element was reported
- `LastModTime` and its `UnmarshalXML()` are documented, including what decoding an empty element with `encoding/xml` yields: a field that is not `nil` and holds the zero time
- A plain text sitemap that begins with a UTF-8 byte order mark (BOM) is read from its first line on. The mark was taken for a part of the first line, which was then not recognised as a URL: the first URL of the sitemap was left out without an error, and a sitemap of a single URL was rejected with `unrecognized sitemap format (root element: "")`. The mark is now taken off before the lines are read, as it has been for `robots.txt` files, also when the sitemap is gzip compressed. The XML formats were not affected. A document that holds nothing but the mark is reported as `sitemap content is empty`. `README.md` describes the mark in the `Character encoding` section, and `examples/encoding` parses a text sitemap that begins with one
- Whitespace around a text value of a `<url>` entry is no longer taken for a part of the value. Only `<loc>`, the numbers and the dates were read without it; `<changefreq>` and every field of the image, news, video and hreflang extensions, attributes included, came back as written, so that a value on a line of its own (`<image:loc>`, `<video:title>`, ...) held the line breaks and the indentation. Every text value of an entry is now returned without the whitespace that surrounds it, in both modes. `README.md` describes the rule in the `Strict mode` section
- Strict mode accepts an image, a video and an alternate link whose location is padded or stands on a line of its own. They were rejected with `net/url: invalid control character in URL`, and an `<xhtml:link>` with a padded `rel` attribute with `alternate link <rel> must be "alternate"`. The 2,048 character limit applies to the location without the whitespace around it, in both modes
- A value an extension requires that holds nothing but whitespace is missing, the same as an empty one. Tolerant mode kept an image, a video and an alternate link whose location was a single space; strict mode reported it as `unsupported scheme ""` and let a news `<title>`, `<publication><name>`, `<publication><language>`, a video `<title>`, `<description>` and an `hreflang` attribute of whitespace pass for a value. They are now left out and reported the way the empty ones are
- A `<changefreq>` is read without the whitespace around it, so `<changefreq> daily </changefreq>` equals `ChangeFreqDaily`. An empty `<changefreq>` is read as absent: `ChangeFreq` is `nil`, not a pointer to an empty string
- In tolerant mode a `<changefreq>` value of the protocol that is written in another letter case (`Daily`, `WEEKLY`) is read as the value itself, so that it equals its constant. A value the protocol does not know is kept as the document gives it. Strict mode accepts neither, see `Changed` above. `examples/tolerant` shows an entry with such a value
- An Atom `<link>` whose `rel` attribute is padded (`rel=" alternate "`) is recognised as the link of its entry. The URL of such an entry was left out without an error
- Strict mode no longer rejects a URL for the letter case of its host or for naming the default port. The hosts of the URL and of the sitemap were compared as they are written, so in a sitemap at `https://example.com/sitemap.xml` the URLs `https://Example.com/page` and `https://example.com:443/page` were reported as `host "Example.com" does not match sitemap host "example.com"` and left out, and a sitemap a sitemap index listed that way was not fetched. The name of a host is not case-sensitive, and a URL without a port is on the default port of its protocol (80 for HTTP, 443 for HTTPS); strict mode now compares the hosts accordingly. A URL on another host, on a subdomain or on another port is rejected as before. `README.md` describes the comparison in the `Strict mode` section, and `examples/strict` shows which URLs strict mode accepts
- In tolerant mode a space in the query of a URL is percent-encoded, as one in the path and in the fragment already was: `https://example.com/search?q=summer sale` is returned as `https://example.com/search?q=summer%20sale`. It was returned with the space in it, which is a URL that cannot be requested: the request for a sitemap that a sitemap index or a `robots.txt` named like that was malformed and failed with HTTP status 400. A URL could also end in a space (`https://example.com/search?q=a #`). The 2,048 character limit applies to the encoded URL. `examples/tolerant` shows an entry with such a URL

### Security
- The patterns set with `SetFollow()` apply to the sitemaps a `robots.txt` names. They used to be applied to the entries of a sitemap index only, so every `Sitemap:` line of a `robots.txt` was requested whatever the patterns, although `SECURITY.md` recommends `SetFollow()` for restricting the requests the parser can be made to send (SSRF). `SECURITY.md` now also says what the patterns do not cover: the URL passed to `Parse()` and redirect targets
- Gzip decompression is now capped at the `SetMaxResponseSize()` limit (default: 50 MB). Previously only the compressed HTTP response body was limited, so a small `.gz` response could expand without bound in memory (decompression bomb), contrary to what `SECURITY.md` stated. Decompression now stops as soon as the output exceeds the limit, and the content is rejected with a `*ParseError` (`decompressed size exceeds limit of N bytes`). The cap also applies to gzip content passed in through the `urlContent` argument of `Parse()` / `ParseContext()`
- A call comes to an end whatever a server lists. The number of sitemaps a call fetches and of URLs it collects was not limited: `SetMaxDepth()` bounds the levels only, and with up to 50,000 sitemaps listed on every level that is no bound in practice. Against a server that lists sitemaps without end, a `Parse()` call without a deadline went on, and its results grew, for as long as the server answered. `SetMaxSitemaps()` (default: 50,000) and `SetMaxURLs()` (default: 10,000,000) bound both. The defaults are a last resort, sized for the largest sitemaps the protocol allows: `SECURITY.md` recommends lower limits and a deadline for documents that are not trusted, and notes that the errors of a call are not limited

## [1.1.0] - 2026-09-21

### Added
- Go native fuzz targets (`sitemap_fuzz_test.go`): `FuzzParse`, `FuzzDetectRootElement`, `FuzzUnzip` and `FuzzParseRobotsTXT`. They assert that every location the parser returns stays within the 2,048-character limit and uses an http/https scheme, that parsing is deterministic, and that gzip payloads round trip unchanged. Seeds are drawn from the fixtures in `test/`, so the seed corpus runs as part of the normal `go test` suite. This also addresses the OpenSSF Scorecard `Fuzzing` check

### Changed
- **Minimum supported Go version raised to 1.26.** `golang.org/x/net` v0.59.0 declares `go 1.26.0`, so the dependency update raises this package's own floor; Go 1.25 is no longer supported upstream either (the Go project currently maintains 1.26 and 1.27). Documented under `Requirements` in `README.md`
- CI: the build matrix now runs Go `1.26` and `stable` instead of `1.25` and `stable`
- `golang.org/x/net` updated from v0.53.0 to v0.59.0, and `golang.org/x/text` (indirect) from v0.36.0 to v0.42.0. The v0.56.0 step along the way resolved OSV advisories GO-2026-5025 through GO-2026-5030
- CI: `govulncheck` installation pinned to exact version v1.5.0 instead of `@latest` (addresses the Scorecard Pinned-Dependencies check)
- CI: `github/codeql-action/init` and `.../analyze` updated from v3.36.3 to v4.37.7 — both steps are bumped together, as CodeQL reports a configuration error when the workflow's action versions do not match
- CI: `golangci/golangci-lint-action` updated from v6.5.2 to v9.3.0 with golangci-lint pinned to v2.13.2. This resolves the `typecheck` failures on `main` (`undefined: sitemap`, `undefined: rand`) caused by golangci-lint v1.64.8 being unable to analyse code built with the current `stable` Go toolchain
- CI: the lint step now runs only on the `stable` Go matrix leg — golangci-lint v2.13.2 declares `go 1.26.0`, while `actions/setup-go` sets `GOTOOLCHAIN=local`, so it cannot be built on the Go 1.25 leg
- CI: `fail-fast: false` added to the build matrix so one failing Go version no longer cancels the other leg's results
- `.golangci.yml` migrated to the golangci-lint v2 configuration schema (`version: "2"`, `linters.default: none`, `linters.exclusions`); `gosimple` removed from the enabled linters as it was merged into `staticcheck` in v2, so check coverage is unchanged
- Dependabot: `github/codeql-action*` updates are now grouped into a single pull request, preventing the mismatched-version failures caused by `init` and `analyze` being bumped separately

## [1.0.2] - 2026-07-06

### Added
- OpenSSF Scorecard workflow (`.github/workflows/scorecard.yml`): weekly and on-push supply-chain security analysis, with results published to [scorecard.dev](https://scorecard.dev/viewer/?uri=github.com/aafeher/go-sitemap-parser); Scorecard badge added to `README.md`
- CodeQL code-scanning workflow (`.github/workflows/codeql.yml`): analyses Go code and GitHub Actions workflows on push, pull request, and a weekly schedule
- Dependabot configuration (`.github/dependabot.yml`): weekly update checks for the `gomod` and `github-actions` ecosystems

### Changed
- CI workflow (`go.yml`) hardened: added a least-privilege `permissions: contents: read` block and pinned all GitHub Actions to full commit SHAs (addresses the Scorecard Token-Permissions and Pinned-Dependencies checks)

## [1.0.1] - 2026-05-06

### Changed
- Internal refactoring of `sitemap.go` to reduce cyclomatic complexity across all functions to ≤ 15 (gocyclo threshold). `parse()` was split into dedicated handlers: `parseSitemapIndexContent`, `parseURLSetContent`, `parseRSSContent`, `parseFeedContent`, and `parseTextContent`. URL validation logic was extracted into `validateInputURL`. Video validation was split into `validateVideoThumbnailStrict` and `validateVideoFieldsStrict`. Regex filter logic was centralised into `matchesFollowFilter` and `matchesRulesFilter` helpers. No public API changes.
- Internal refactoring of test files (`sitemap_test.go`, `test_server_test.go`) to reduce cyclomatic complexity: added generic `mustEqual`, `requireParse`, `assertCounts`, `requireURLSetParse`, `assertImageFields`, `assertPtrInt`, `assertPtrFloat32`, `assertVideoRestriction`, `assertVideoPlatform`, `assertVideoUploader`, `assertStringSlice`, `assertHasSuffix`, `mustGetBody`, and `mustUnzip` helpers; converted high-complexity test functions to table-driven style.

## [1.0.0] - 2026-05-04

### Added
- Support for RSS 2.0, Atom 1.0, and Plain Text sitemaps: the parser now automatically detects these formats and extracts URLs from them.
- XHTML hreflang extension support (`<xhtml:link>`): the `URL` struct now exposes a `Hreflangs []AlternateLink` field populated from `xmlns:xhtml="http://www.w3.org/1999/xhtml"` elements. Each `AlternateLink` exposes `Rel`, `Hreflang`, and `Href`.
- `SECURITY.md`: security policy, vulnerability reporting via GitHub Private Security Advisories, and guidance on SSRF, resource exhaustion, XXE, and TLS verification
- Hreflang validation: links with an empty `Href` are silently dropped in tolerant mode or produce an error in strict mode. In strict mode, `Rel` must be `"alternate"`, `Hreflang` must not be empty, and `Href` must be a valid absolute HTTP(S) URL.
- New examples: [`examples/rss`](examples/rss/main.go), [`examples/atom`](examples/atom/main.go), [`examples/text`](examples/text/main.go), [`examples/hreflang`](examples/hreflang/main.go), and [`examples/maxdepth`](examples/maxdepth/main.go).
- Configuration getter methods: `GetUserAgent()`, `GetFetchTimeout()`, `GetMultiThread()`, `GetMaxResponseSize()`, `GetMaxDepth()`, `GetMaxConcurrency()`, `GetFollow()`, `GetRules()`, `GetHTTPClient()`, `GetStrict()` — each returns the current value of the corresponding configuration field. `GetFollow()` and `GetRules()` return copies of the internal slice.

### Changed
- `SetFetchTimeout()` now rejects `0` with a `*ConfigError`; the default value is kept unchanged. Previously `0` was silently accepted but caused every HTTP request to time out immediately.
- `URLSet`, `RSS`, and `Atom` XML parsing structs are now unexported (`urlSet`, `rss`, `atom`). These were internal implementation details used only for XML unmarshalling and were never part of the documented public API.
- `lastModTime` renamed to `LastModTime` (exported). This type is the value behind `URL.LastMod`, `Video.ExpirationDate`, `Video.PublicationDate`, and `News.PublicationDate`. Callers that stored a `*lastModTime` value must update to `*LastModTime`.
- Go minimum version bumped from 1.24.0 to 1.25.0; CI matrix updated from `1.24` to `1.25`; golangci-lint switched to `install-mode: goinstall` so the linter binary is always compiled with the current matrix Go version, resolving compatibility failures when the pre-built binary lags behind the module's `go` directive
- `golang.org/x/net` updated from v0.45.0 to v0.53.0
- `golang.org/x/text` (indirect) updated from v0.29.0 to v0.36.0

## [0.9.0] - 2026-05-03

### Added
- Typed errors: four new exported error types allow callers to distinguish error categories with `errors.As` and inspect structured context:
  - `*ConfigError` — returned when a `Set*` configuration method receives an invalid value; exposes `Field` (setting name) and `Err` (root cause).
  - `*NetworkError` — returned when an HTTP fetch fails; exposes `URL` (the requested URL) and `Err` (root cause).
  - `*ParseError` — returned when XML or gzip parsing of a sitemap document fails; exposes `URL` (the sitemap URL) and `Err` (root cause).
  - `*ValidationError` — returned when a URL or field value fails validation; exposes `URL` (the value being validated) and `Err` (root cause).
  - All four types implement `Unwrap()`, enabling `errors.Is` traversal to the root cause.
- New example: [`examples/errors`](examples/errors/main.go)

### Changed
- All errors stored in `GetErrors()` and returned by `Parse()` / `ParseContext()` are now wrapped in the appropriate typed error. Error messages have changed format to include error-type context (e.g. `fetch "URL": received HTTP status 404`, `parse "URL": sitemap content is empty`, `validate "URL": strict mode: unsupported scheme "ftp"`, `config "field": must be greater than 0, got -1`). Code that matched on exact error message strings must be updated to use `errors.As` or `strings.Contains`.

## [0.8.0] - 2026-05-03

### Added
- Google Video Sitemap extension support (`<video:video>`): the `URL` struct now exposes a `Videos []Video` field populated from `xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"` elements. `Video` exposes `ThumbnailLoc`, `Title`, `Description`, `ContentLoc`, `PlayerLoc`, `Duration`, `ExpirationDate`, `Rating`, `ViewCount`, `PublicationDate`, `FamilyFriendly`, `Restriction`, `Platform`, `RequiresSubscription`, `Uploader`, `Live`, and `Tags`.
- Video validation: videos with an empty `ThumbnailLoc` are silently dropped in tolerant mode or produce an error in strict mode; `ThumbnailLoc` values exceeding 2,048 characters or with an invalid/non-HTTP(S) scheme are rejected in strict mode. In strict mode, `Title`, `Description`, at least one of `ContentLoc`/`PlayerLoc`, `Duration` range (1–28800), `Rating` range (0.0–5.0), and tag count (≤ 32) are also validated.
- New example: [`examples/video`](examples/video/main.go)

## [0.7.0] - 2026-05-03

### Added
- Google Image Sitemap extension support (`<image:image>`): the `URL` struct now exposes an `Images []Image` field populated from `xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"` elements. Each `Image` exposes `Loc`, `Title`, `Caption`, `GeoLocation`, and `License` fields.
- Image validation: in tolerant mode, images with an empty `<image:loc>` are silently dropped; URLs exceeding 2,048 characters are rejected with an error. In strict mode, `<image:loc>` must additionally be a non-empty absolute HTTP(S) URL. CDN-hosted images (different host from the page URL) are permitted in both modes per the Google specification.
- Google News Sitemap extension support (`<news:news>`): the `URL` struct now exposes a `News *News` field populated from `xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"` elements. `News` exposes `Publication` (with `Name` and `Language`), `PublicationDate`, and `Title`.
- News validation: in strict mode, all four required fields (`Title`, `Publication.Name`, `Publication.Language`, `PublicationDate`) must be present; each missing field is reported via `GetErrors()` while the `News` entry is still included. In tolerant mode no validation is performed.
- New examples: [`examples/image`](examples/image/main.go), [`examples/news`](examples/news/main.go)

## [0.6.0] - 2026-05-03

### Added
- `SetHTTPClient()`: supply a custom `*http.Client` for all HTTP requests, enabling custom transports, proxies, TLS configuration, and authentication via a custom `http.RoundTripper`. When a custom client is set, `SetFetchTimeout` has no effect — the client's own `Timeout` field controls the request deadline. Pass `nil` to restore the default behaviour.
- New example: [`examples/httpclient`](examples/httpclient/main.go)

## [0.5.0] - 2026-05-01

### Changed
- Default `maxConcurrency` changed from `0` (unlimited) to `16`, preventing unbounded goroutine and connection growth on large sitemap indexes (**breaking**: call `SetMaxConcurrency(0)` to restore the previous unlimited behaviour)

## [0.4.0] - 2026-05-01

### Added
- `ParseContext()` method: propagates `context.Context` cancellation and deadlines to every HTTP request issued during parsing
- `SetMaxConcurrency()`: bounds the number of concurrent HTTP fetches per `Parse()` call; `0` (default) means unlimited
- URL deduplication: each sitemap URL is fetched at most once per `Parse()` call, even if referenced from multiple sitemap indexes or `robots.txt` directives
- `<priority>` value validation in strict mode: values outside `[0.0, 1.0]` are rejected; tolerant mode accepts any value
- Maximum regex pattern length (1,000 characters) enforced in `SetFollow()` and `SetRules()`; oversized patterns are rejected with an error

### Changed
- `<loc>` URL length limit (2,048 characters per the sitemaps.org spec) is now enforced in both strict and tolerant modes; previously only applied in strict mode
- Parse errors now include the source URL for easier debugging (e.g. `"sitemap content is empty at \"https://…\""`, `"failed to parse sitemapindex at \"https://…\": …"`)
- Thread-safety guarantees and deadlock prevention documented in README

### Fixed
- Deadlock when `SetMaxConcurrency` was used together with a `robots.txt` listing multiple sitemaps: the semaphore slot is now released immediately after the HTTP fetch, before any recursive parse step
- Data race: all configuration setters and result getters now hold the internal mutex during field access
- Gzip decompression: improved error handling and recovery for truncated or corrupted streams
- `<lastmod>` elements that are empty or contain only whitespace are now treated as absent (`nil`) instead of causing a parse error
- `robots.txt` parser: UTF-8 BOM, inline comments (`#`), and mixed whitespace are now handled correctly

## [0.3.0] - 2026-04-26

### Added
- `SetStrict()`: enables strict URL validation per the sitemaps.org specification (`<loc>` must be an absolute HTTP/HTTPS URL on the same host, ≤ 2,048 characters)
- `SetMaxDepth()`: limits sitemap index recursion depth (default: 10)
- `SetMaxResponseSize()`: caps the HTTP response body size accepted per fetch (default: 50 MB)
- `URLChangeFreq` type and change-frequency constants exported: `ChangeFreqAlways`, `ChangeFreqHourly`, `ChangeFreqDaily`, `ChangeFreqWeekly`, `ChangeFreqMonthly`, `ChangeFreqYearly`, `ChangeFreqNever`
- Concurrent `Parse()` / `ParseContext()` calls on the same instance are serialised via a dedicated parse-level mutex

### Changed
- `SetFetchTimeout()` parameter widened from `uint8` to `uint16`, allowing timeouts up to 65,535 seconds (**breaking**: typed `uint8` variables must be updated)
- XML root element is now detected in a single pass to avoid double-parsing
- Go minimum version bumped to 1.24; `math/rand` migrated to `math/rand/v2`; `x/net` and `x/text` dependencies updated
- `SetMaxResponseSize()` and `SetMaxDepth()` reject non-positive values with a recorded error

### Fixed
- `GetURLs()` panic when called on a nil receiver
- `GetRandomURLs()` was mutating the original URL slice
- `SetFollow()` and `SetRules()` were accumulating compiled regexes across repeated calls instead of replacing them
- HTTP response body leak when the server returned a non-200 status in `fetch()`
- Data race in concurrent sitemap parsing (struct-level mutex added)
- `Parse()` now resets all internal state at the start of each call, making instance reuse safe
- `robots.txt` parsing: CRLF line endings and case-insensitive `Sitemap:` directive now handled correctly

## [0.2.0] - 2025-07-03

### Added
- Examples for `SetFollow()` and `SetRules()` in the `examples/` directory
- Comprehensive tests for HTTP server response handling and gzip compression
- Tests for fetch error scenarios (invalid URL, interrupted I/O)

### Changed
- Gzip compression/decompression logic refactored; `S` receiver dependency removed from helper functions

## [0.1.9] - 2025-03-19

### Added
- Tests for `lastModTime` XML unmarshaling

### Fixed
- Whitespace is now trimmed from timestamp strings before parsing

## [0.1.8] - 2025-03-10

### Fixed
- URL `<loc>` values are normalised by trimming surrounding whitespace

## [0.1.7] - 2025-02-09

### Fixed
- Whitespace trimmed from sitemap index `<loc>` entries before appending

## [0.1.6] - 2025-01-31

### Added
- Datetime parsing supports multiple formats: ISO 8601 with timezone, RFC 3339, date-only (`YYYY-MM-DD`), and several others

## [0.1.5] - 2025-01-26

### Changed
- XML decoding now uses a charset-aware reader (`charset.NewReaderLabel`) to handle non-UTF-8 encoded sitemaps
- Error handling and parsing logic refined

## [0.1.4] - 2025-01-11

### Changed
- Recursive URL parsing refactored for clarity and correctness

## [0.1.3] - 2025-01-11

### Added
- `SetFollow()`: regex-based filtering of which sitemaps in an index are followed
- `SetRules()`: regex-based filtering of which URLs are included in results

## [0.1.2] - 2025-01-05

### Added
- `SetMultiThread()`: toggle for concurrent (multi-threaded) fetching and parsing

## [0.1.1] - 2024-11-01

### Fixed
- Mutex added to synchronise concurrent access in `Parse()`

## [0.1.0] - 2024-02-23

### Added
- Initial release
- Recursive XML sitemap parsing: sitemap index → sitemaps → URLs
- `robots.txt` support for discovering sitemap URLs via `Sitemap:` directives
- Gzip-compressed sitemap support (`.xml.gz`)
- Configurable user agent (`SetUserAgent()`) and fetch timeout (`SetFetchTimeout()`)
- `GetURLs()`, `GetURLCount()`, `GetRandomURLs()`, `GetErrors()`, `GetErrorsCount()`
- Each parsed `URL` exposes `Loc`, `LastMod`, `ChangeFreq`, and `Priority`
- Method chaining (fluent interface) on all setters

[Unreleased]: https://github.com/aafeher/go-sitemap-parser/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/aafeher/go-sitemap-parser/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/aafeher/go-sitemap-parser/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/aafeher/go-sitemap-parser/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/aafeher/go-sitemap-parser/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.9.0...v1.0.0
[0.9.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.9...v0.2.0
[0.1.9]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/aafeher/go-sitemap-parser/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/aafeher/go-sitemap-parser/releases/tag/v0.1.0

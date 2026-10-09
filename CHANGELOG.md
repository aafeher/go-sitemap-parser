# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `examples/follow`: runnable example of restricting the sitemaps that are fetched with `SetFollow()`, those a `robots.txt` names and those of a sitemap index
- `examples/redirect`: runnable example of parsing a sitemap that is reached through a redirect, in tolerant and in strict mode. `README.md` gained a matching `Redirects` section
- `examples/multithread`: runnable example of fetching the sitemaps of a `robots.txt` with multi-threading on and off. `README.md` describes what `SetMultiThread(false)` guarantees and links the example
- `examples/reuse`: runnable example of reusing one instance for several `Parse()` calls and of correcting an invalid setting. `README.md` gained a matching `Reusing an instance` section
- `examples/tolerant`: runnable example of how tolerant and strict mode treat a sitemap with invalid values, entries without a location and malformed XML in it
- `examples/encoding`: runnable example of parsing a sitemap that is not UTF-8 encoded. `README.md` gained a matching `Character encoding` section, which also lists the limitations (UTF-16 / UTF-32 and the HTTP `Content-Type` charset are not supported)

### Changed
- `SetMaxConcurrency()` bounds the sitemaps that are being fetched or parsed at the same time; it used to bound the fetches only. A sitemap takes up a slot from the moment its request is sent until its content is parsed, and the slot is given back before the sitemaps it lists are followed. The limit therefore also caps the CPU cores in use and the documents held in memory at once, now that the sitemaps are parsed in parallel (see `Fixed` below). Parsing in parallel takes more memory than parsing one sitemap at a time did: for an index of 96 sitemaps of 20,000 URLs each, the peak heap went from about 1.05 GB to about 1.35 GB at the default limit of 16, and stayed at 1.1 GB with `SetMaxConcurrency(2)`. `SetMaxConcurrency(0)` lets all the sitemaps of an index be parsed at once, which took about 2.4 GB in the same measurement
- **`SetFollow()` filters the sitemaps a `robots.txt` names as well**, see `Security` below. Patterns written with only the entries of a sitemap index in mind have to match the sitemaps and sitemap indexes named in the `robots.txt` too, otherwise those are no longer fetched
- The value of a `Sitemap:` line of a `robots.txt` is validated before it is fetched, the way the `<loc>` of a sitemap index entry is. A value that is not an HTTP or HTTPS URL is reported as a `*ValidationError` (`validate "ftp://example.com/sitemap.xml": unsupported scheme "ftp"`) without a request being attempted; it used to be reported as the `*NetworkError` of the failed request. A URL of more than 2,048 characters is rejected as well, it used to be fetched
- In tolerant mode a relative `Sitemap:` value in a `robots.txt` (`Sitemap: /sitemap.xml`) is resolved against the URL of the `robots.txt` and fetched. It used to fail with `unsupported protocol scheme ""`. Strict mode rejects it as not being absolute. A sitemap on another host than the `robots.txt` is accepted in both modes, as before
- In strict mode the URLs of a redirected sitemap are compared with the URL the sitemap was served from. URLs on the host and protocol the sitemap was requested at are therefore rejected when the redirect led to another host or protocol; they used to be the only ones accepted
- Errors about a sitemap that was reached through a redirect name the URL the sitemap was served from instead of the URL that was requested. This concerns `*ParseError` and the `*ValidationError` for an entry without a location. A failed fetch is still reported as a `*NetworkError` naming the requested URL
- When the context is already cancelled by the time the sitemaps of a `robots.txt` are to be fetched, none of them is attempted and nothing is recorded for them, as has been the case for the sitemaps of a sitemap index. `ParseContext()` still returns the context error. `GetErrors()` used to hold one `context canceled` error for every sitemap the `robots.txt` lists
- A call that is cancelled while sitemaps wait for a `SetMaxConcurrency()` slot records the cancellation once for the sitemap index or `robots.txt` that lists them, and leaves the sitemaps that were still waiting alone. `GetErrors()` used to hold one `context canceled` error for every sitemap that was waiting. `ParseContext()` returns the context error as before, and a request that was cut short is still reported as a `*NetworkError` of its own
- In strict mode an entry without a `<loc>` is reported as `validate "https://example.com/sitemap.xml": <loc> of an entry is empty or missing`, naming the sitemap it stands in, instead of `validate "": strict mode: unsupported scheme ""`
- In strict mode an RSS `<item>` without a `<link>` is no longer reported, the link of a feed item being optional. It is skipped without an error, as an Atom `<entry>` without a link already was
- Tolerant mode reads XML leniently: an unescaped `&` or an unknown entity such as `&nbsp;` is taken literally, and a missing end tag is made up for. Such a document used to be rejected as a whole with an XML syntax error. Strict mode still requires well-formed XML and rejects the document
- A value that cannot be parsed is reported as a `*ValidationError` for the page it belongs to (`validate "https://example.com/page": invalid <lastmod> value "2024-01-15 10:30:00"`) instead of a `*ParseError` for the whole sitemap
- A `Parse()` / `ParseContext()` call that returns early — because a configuration error is outstanding or the input URL is invalid — now leaves `GetURLs()` empty. Previously the URLs collected by the preceding call were still returned in that case, so a failed call could be mistaken for a successful one. `GetURLs()` and `GetErrors()` now always describe the most recent call only

### Fixed
- Multi-threaded parsing runs in parallel. A fetched sitemap used to be unzipped and decoded with the internal mutex held, so the sitemaps were fetched concurrently but parsed one after the other, and multi-threading gained nothing but the time spent waiting for the network: on 16 cores, an index of 16 sitemaps of 60,000 URLs each took 5.8 s with multi-threading on and 5.8 s with it off. Every sitemap is now unzipped and decoded without the mutex, which is taken only to add what the sitemap yielded to the results. The same index takes 1.6 s with multi-threading on; with it off the time is unchanged
- The getters and setters no longer wait for a sitemap to be parsed. Called while `Parse()` / `ParseContext()` was running, `GetURLs()`, `GetURLCount()`, `GetErrors()` and every other method blocked until the sitemap being parsed was finished, and with several sitemaps waiting for the mutex until those were finished too: up to 5.3 s in the measurement above. They now wait only while the results of a sitemap are added (0.2 s at most in the same measurement, with close to a million URLs collected)
- Parsing a sitemap takes less memory. The content of a document used to be copied in full up to three times on its way through the parser, converted from bytes to a string and back, and the entries of a `<urlset>` were all decoded into a slice of their own before they were turned into the URLs that are returned. The content is now read into a string once and handed on as it is, and the entries of a `<urlset>` are processed one at a time while the document is read. For a `<urlset>` of 20 MB holding 155,000 URLs, the peak heap went from about 290 MB to about 150 MB when the document is fetched, and from about 235 MB to about 110 MB when it is passed to `Parse()` as content; the memory allocated on the way went from about 800 MB to about 610 MB. Less copying is less work too: the benchmark of an index of 16 sitemaps of 5,000 URLs each takes 69 ms instead of 124 ms with multi-threading on, and 247 ms instead of 323 ms with it off. What a document yields is unchanged, a `<urlset>` that cannot be read to its end still yields nothing but the error. A text sitemap gains little, its memory being taken up by the URLs themselves
- The content of the URL passed to `Parse()` / `ParseContext()` is no longer kept once it is parsed. The instance used to hold it until the next call: while the sitemaps it lists were fetched, and for as long as the results were in use afterwards, although nothing read it any more. For a sitemap index or a `robots.txt` of 16 MB that was 16 MB. Parts of a document could keep the whole of it in memory as well, for as long as a single one of them was in use: the URLs of a text sitemap parsed in strict mode, the lines of a text sitemap reported as invalid, and the `Sitemap:` values of a `robots.txt`. They are now copies of their own
- With multi-threading on, a goroutine is started for a sitemap only when a `SetMaxConcurrency()` slot is free for it. A goroutine used to be started at once for every sitemap a sitemap index or a `robots.txt` lists, to wait for a slot there, so the limit bounded the requests but not the goroutines: parsing an index of 5,000 sitemaps with `SetMaxConcurrency(4)` had about 4,850 goroutines running. It now has about 30, those of the HTTP client and of the test server included. With `SetMaxConcurrency(0)` there is a goroutine for every sitemap, as before
- A sitemap reached through a redirect is treated as located at the URL it was served from. The URL that was requested used to be taken for its location, so after a redirect from `http://example.com/sitemap.xml` to `https://www.example.com/sitemap.xml` strict mode rejected every URL of the sitemap (`strict mode: host "www.example.com" does not match sitemap host "example.com"`), and tolerant mode resolved relative URLs to the host that was left behind (`<loc>/page</loc>` to `http://example.com/page`). Relative URLs are now resolved against the URL the last redirect led to, and strict mode compares the URLs with it. This goes for the URL passed to `Parse()` as well as for the sitemaps of a sitemap index and of a `robots.txt`
- `SetMultiThread(false)` is honoured for the sitemaps a `robots.txt` lists. They used to be fetched concurrently whatever the setting, one goroutine and one request for every `Sitemap:` line, so a `robots.txt` listing eight sitemaps put eight requests in flight at once on a parser that was told to fetch one at a time. They are now fetched the way the sitemaps of a sitemap index are: one at a time and in the order listed when multi-threading is off, concurrently and bounded by `SetMaxConcurrency()` when it is on. The depth limit of `SetMaxDepth()` applies to them as before
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

### Security
- The patterns set with `SetFollow()` apply to the sitemaps a `robots.txt` names. They used to be applied to the entries of a sitemap index only, so every `Sitemap:` line of a `robots.txt` was requested whatever the patterns, although `SECURITY.md` recommends `SetFollow()` for restricting the requests the parser can be made to send (SSRF). `SECURITY.md` now also says what the patterns do not cover: the URL passed to `Parse()` and redirect targets
- Gzip decompression is now capped at the `SetMaxResponseSize()` limit (default: 50 MB). Previously only the compressed HTTP response body was limited, so a small `.gz` response could expand without bound in memory (decompression bomb), contrary to what `SECURITY.md` stated. Decompression now stops as soon as the output exceeds the limit, and the content is rejected with a `*ParseError` (`decompressed size exceeds limit of N bytes`). The cap also applies to gzip content passed in through the `urlContent` argument of `Parse()` / `ParseContext()`

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

[Unreleased]: https://github.com/aafeher/go-sitemap-parser/compare/v1.1.0...HEAD
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

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Support for RSS 2.0, Atom 1.0, and Plain Text sitemaps: the parser now automatically detects these formats and extracts URLs from them.
- XHTML hreflang extension support (`<xhtml:link>`): the `URL` struct now exposes a `Hreflangs []AlternateLink` field populated from `xmlns:xhtml="http://www.w3.org/1999/xhtml"` elements. Each `AlternateLink` exposes `Rel`, `Hreflang`, and `Href`.
- `SECURITY.md`: security policy, vulnerability reporting via GitHub Private Security Advisories, and guidance on SSRF, resource exhaustion, XXE, and TLS verification
- Hreflang validation: links with an empty `Href` are silently dropped in tolerant mode or produce an error in strict mode. In strict mode, `Rel` must be `"alternate"`, `Hreflang` must not be empty, and `Href` must be a valid absolute HTTP(S) URL.
- New examples: [`examples/rss`](examples/rss/main.go), [`examples/atom`](examples/atom/main.go), [`examples/text`](examples/text/main.go), and [`examples/hreflang`](examples/hreflang/main.go).
- Configuration getter methods: `GetUserAgent()`, `GetFetchTimeout()`, `GetMultiThread()`, `GetMaxResponseSize()`, `GetMaxDepth()`, `GetMaxConcurrency()`, `GetFollow()`, `GetRules()`, `GetHTTPClient()`, `GetStrict()` — each returns the current value of the corresponding configuration field. `GetFollow()` and `GetRules()` return copies of the internal slice.

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

[Unreleased]: https://github.com/aafeher/go-sitemap-parser/compare/v0.9.0...HEAD
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

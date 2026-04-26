# go-sitemap-parser

[![codecov](https://codecov.io/gh/aafeher/go-sitemap-parser/graph/badge.svg?token=KEABI9UTQY)](https://codecov.io/gh/aafeher/go-sitemap-parser)
[![Go](https://github.com/aafeher/go-sitemap-parser/actions/workflows/go.yml/badge.svg)](https://github.com/aafeher/go-sitemap-parser/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/aafeher/go-sitemap-parser.svg)](https://pkg.go.dev/github.com/aafeher/go-sitemap-parser)
[![Go Report Card](https://goreportcard.com/badge/github.com/aafeher/go-sitemap-parser)](https://goreportcard.com/report/github.com/aafeher/go-sitemap-parser)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

A Go package to parse XML Sitemaps compliant with the [Sitemaps.org protocol](http://www.sitemaps.org/protocol.html).

## Features
- Recursive parsing (sitemap index → sitemaps → URLs)
- Concurrent (multi-threaded) fetching and parsing
- Configurable follow rules to filter which sitemaps to parse
- Configurable URL rules to filter which URLs to include
- Configurable HTTP response size limit
- Tolerant mode (default): resolves relative URLs in `<loc>` elements
- Strict mode: validates URLs per the sitemaps.org specification
- Thread-safe

## Formats supported
- `robots.txt`
- XML `.xml`
- Gzip compressed XML `.xml.gz`

## Installation

```bash
go get github.com/aafeher/go-sitemap-parser
```

```go
import "github.com/aafeher/go-sitemap-parser"
```

## Usage

### Create instance

To create a new instance with default settings, you can simply call the `New()` function.
```go
s := sitemap.New()
```

### Configuration defaults

 - userAgent: `"go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)"`
 - fetchTimeout: `3` seconds
 - maxResponseSize: `52428800` (50 MB)
 - maxDepth: `10`
 - multiThread: `true`
 - strict: `false`

### Overwrite defaults

#### User Agent

To set the user agent, use the `SetUserAgent()` function.

```go
s := sitemap.New()
s = s.SetUserAgent("YourUserAgent")
```
... or ...
```go
s := sitemap.New().SetUserAgent("YourUserAgent")
```

#### Fetch timeout

To set the fetch timeout, use the `SetFetchTimeout()` function. It should be specified in seconds as a **uint16** value (max 65535 seconds).

```go
s := sitemap.New()
s = s.SetFetchTimeout(10)
```
... or ...
```go
s := sitemap.New().SetFetchTimeout(10)
```

#### Max response size

To set the maximum allowed HTTP response size, use the `SetMaxResponseSize()` function. It should be specified in bytes as an **int64** value. The default is 50 MB, matching the [sitemaps.org protocol](http://www.sitemaps.org/protocol.html) limit. Responses exceeding this limit will result in an error.

```go
s := sitemap.New()
s = s.SetMaxResponseSize(10 * 1024 * 1024) // 10 MB
```
... or ...
```go
s := sitemap.New().SetMaxResponseSize(10 * 1024 * 1024) // 10 MB
```

#### Max depth

To set the maximum recursion depth for following sitemap indexes, use the `SetMaxDepth()` function. A sitemap index may reference other sitemap indexes; this limits how many levels deep the parser will follow. The default is 10.

```go
s := sitemap.New()
s = s.SetMaxDepth(5)
```
... or ...
```go
s := sitemap.New().SetMaxDepth(5)
```

#### Multi-threading

By default, the package uses multi-threading to fetch and parse sitemaps concurrently.
To set the multi-thread flag on/off, use the `SetMultiThread()` function.

```go
s := sitemap.New()
s = s.SetMultiThread(false)
```
... or ...
```go
s := sitemap.New().SetMultiThread(false)
```

#### Follow rules

To set the follow rules, use the `SetFollow()` function. It should be specified a `[]string` value.
It is a list of regular expressions. When parsing a sitemap index, only sitemaps with a `loc` that matches one of these expressions will be followed and parsed.
If no follow rules are provided, all sitemaps in the index are followed.

```go
s := sitemap.New()
s.SetFollow([]string{
	`\.xml$`,
	`\.xml\.gz$`,
})
```
... or ...
```go
s := sitemap.New().SetFollow([]string{
	`\.xml$`,
	`\.xml\.gz$`,
})
```

#### URL rules

To set the URL rules, use the `SetRules()` function. It should be specified a `[]string` value.
It is a list of regular expressions. Only URLs that match one of these expressions will be included in the final result.
If no rules are provided, all URLs found are included.

```go
s := sitemap.New()
s.SetRules([]string{
	`product/`,
	`category/`,
})
```
... or ...
```go
s := sitemap.New().SetRules([]string{
	`product/`,
	`category/`,
})
```

#### Strict mode

By default, the parser operates in **tolerant mode**: relative URLs found in `<loc>` elements are automatically resolved against the parent sitemap URL. This handles real-world sitemaps that may not fully comply with the specification.

To enable **strict mode**, use the `SetStrict()` function. In strict mode, all `<loc>` URLs are validated per the [sitemaps.org protocol](http://www.sitemaps.org/protocol.html):
- Must be absolute HTTP or HTTPS URLs
- Must use the same host and protocol as the sitemap file
- Must not exceed 2,048 characters

URLs that fail validation are skipped and reported via `GetErrors()`.

```go
s := sitemap.New()
s = s.SetStrict(true)
```
... or ...
```go
s := sitemap.New().SetStrict(true)
```

#### Chaining methods

In both cases, the functions return a pointer to the main object of the package, allowing you to chain these setting methods in a fluent interface style:
```go
s := sitemap.New().SetUserAgent("YourUserAgent").SetFetchTimeout(10)
```

### Parse

Once you have properly initialized and configured your instance, you can parse sitemaps using the `Parse()` function.

The `Parse()` function takes in two parameters:
 - `url`: the URL of the sitemap to be parsed,
   - `url` can be a robots.txt or sitemapindex or sitemap (urlset)
 - `urlContent`: an optional string pointer for the content of the URL.

If you wish to provide the content yourself, pass the content as the second parameter. If not, simply pass nil and the function will fetch the content on its own.
The `Parse()` function performs concurrent parsing and fetching optimized by the use of Go's goroutines and sync package, ensuring efficient sitemap handling.

```go
s, err := s.Parse("https://www.sitemaps.org/sitemap.xml", nil)
```
In this example, sitemap is parsed from "https://www.sitemaps.org/sitemap.xml". The function fetches the content itself, as we passed nil as the urlContent.

### Parse with context

For new code, prefer `ParseContext()` so that callers can propagate cancellation
and deadlines to every HTTP request issued by the parser (the initial fetch as
well as the recursive sitemap-index/urlset fetches). The legacy `Parse()` is a
backward-compatible wrapper around `ParseContext()` that uses
`context.Background()`.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

s, err := sitemap.New().ParseContext(ctx, "https://www.sitemaps.org/sitemap.xml", nil)
```

Cancelling `ctx` aborts in-flight downloads and prevents new ones from starting.
Already-parsed URLs accumulated before cancellation remain available via
`GetURLs()`; the cancellation cause is also recorded in the error list and
returned by `ParseContext`.

See [`examples/context`](examples/context/main.go) for a runnable example.

### Results

After parsing, you can retrieve the results using the following methods:

#### GetURLs

Returns all parsed URLs as a `[]URL` slice.

```go
urls := s.GetURLs()
```

Each `URL` struct contains the following fields:
- `Loc` (`string`) — the URL location
- `LastMod` (`*lastModTime`) — last modification time (embeds `time.Time`), may be `nil`
- `ChangeFreq` (`*URLChangeFreq`) — change frequency hint, may be `nil`. Use the exported constants for comparison: `ChangeFreqAlways`, `ChangeFreqHourly`, `ChangeFreqDaily`, `ChangeFreqWeekly`, `ChangeFreqMonthly`, `ChangeFreqYearly`, `ChangeFreqNever`
- `Priority` (`*float32`) — crawl priority between 0.0 and 1.0, may be `nil`

#### GetURLCount

Returns the number of parsed URLs.

```go
count := s.GetURLCount()
```

#### GetRandomURLs

Returns a slice of `n` randomly selected URLs without duplicates.

```go
randomURLs := s.GetRandomURLs(5)
```

#### GetErrors

Returns all errors encountered during parsing.

```go
errs := s.GetErrors()
```

#### GetErrorsCount

Returns the number of errors encountered during parsing.

```go
errCount := s.GetErrorsCount()
```

## Examples

Examples can be found in [/examples](https://github.com/aafeher/go-sitemap-parser/tree/main/examples).

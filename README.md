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
 - multiThread: `true`

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

To set the fetch timeout, use the `SetFetchTimeout()` function. It should be specified in seconds as an **uint8** value.

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
- `ChangeFreq` (`*urlChangeFreq`) — change frequency hint (`"always"`, `"hourly"`, `"daily"`, `"weekly"`, `"monthly"`, `"yearly"`, `"never"`), may be `nil`
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

# go-sitemap-parser

[![codecov](https://codecov.io/gh/aafeher/go-sitemap-parser/graph/badge.svg?token=KEABI9UTQY)](https://codecov.io/gh/aafeher/go-sitemap-parser)
[![Go](https://github.com/aafeher/go-sitemap-parser/actions/workflows/go.yml/badge.svg)](https://github.com/aafeher/go-sitemap-parser/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/aafeher/go-sitemap-parser.svg)](https://pkg.go.dev/github.com/aafeher/go-sitemap-parser)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/aafeher/go-sitemap-parser/badge)](https://scorecard.dev/viewer/?uri=github.com/aafeher/go-sitemap-parser)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

A Go package to parse XML Sitemaps compliant with the [Sitemaps.org protocol](http://www.sitemaps.org/protocol.html).

> For information on reporting security vulnerabilities, see [SECURITY.md](SECURITY.md).

## Features
- Recursive parsing (sitemap index → sitemaps → URLs)
- Concurrent (multi-threaded) fetching and parsing
- Configurable follow rules to filter which sitemaps to parse
- Configurable URL rules to filter which URLs to include
- Configurable HTTP response size limit
- Limits on the sitemaps a call fetches and on the URLs it collects, so that a call comes to an end whatever a server lists
- Tolerant mode (default): resolves relative URLs in `<loc>` elements; rejects URLs exceeding 2,048 characters after resolution
- Strict mode: validates URLs per the sitemaps.org specification
- A value that cannot be parsed (e.g. a malformed `<lastmod>`) costs only itself, never the whole sitemap
- Google Image Sitemap extension (`<image:image>`)
- Google News Sitemap extension (`<news:news>`)
- Google Video Sitemap extension (`<video:video>`)
- XHTML hreflang extension (`<xhtml:link>`)
- XML documents in encodings other than UTF-8 (e.g. `ISO-8859-1`, `windows-1252`) are transcoded automatically
- Typed errors: `*ConfigError`, `*NetworkError`, `*ParseError`, `*ValidationError` — inspectable via `errors.As`
- Thread-safe

## Formats supported
- `robots.txt`
- XML `.xml`
- RSS 2.0
- Atom 1.0
- Plain text `.txt`
- Gzip compressed files (e.g., `.xml.gz`, `.txt.gz`)

XML documents do not have to be UTF-8 encoded, see [Character encoding](#character-encoding).

## Requirements

Go 1.26 or later.

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
 - maxConcurrency: `16`
 - maxSitemaps: `50000`
 - maxURLs: `10000000`
 - multiThread: `true`
 - strict: `false`
 - httpClient: `nil` (a default `*http.Client` is created per call with the configured `fetchTimeout`)

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

To set the fetch timeout, use the `SetFetchTimeout()` function. It should be specified in seconds as a **uint16** value (1–65535 seconds). A value of `0` is rejected and a `*ConfigError` is recorded. Note: when a custom HTTP client is set via `SetHTTPClient()`, this value has no effect — the client's own `Timeout` field controls the request deadline.

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

The same limit caps the **decompressed** size of gzip-compressed content, so a small `.gz` response cannot expand without bound in memory. This applies both to fetched content and to gzip content passed in through the `urlContent` argument of `Parse()`. Content that expands beyond the limit is rejected and reported via `GetErrors()` as a `*ParseError`. If it is the content of the URL passed to `Parse()`, the call fails with that error, see [Parse](#parse).

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

With `SetMaxDepth(1)` the sitemaps listed by the document passed to `Parse()` are fetched, but if one of them is a sitemap index itself, the sitemaps it lists are not. A `robots.txt` does not count as a level: the sitemaps it names are treated like the document passed to `Parse()`.

When the limit is reached, a `*ParseError` is recorded in `GetErrors()` for every sitemap index whose sitemaps are not followed. It names that sitemap index, by the URL it was served from:

```
parse "https://example.com/sitemap-index-2.xml": max recursion depth of 1 reached
```

Reaching the limit does not fail `Parse()`: the URLs collected up to that depth are returned.

See [`examples/maxdepth`](examples/maxdepth/main.go) for a runnable example.

#### Max sitemaps

A sitemap index may list sitemap indexes, which may list further ones, so how many sitemaps a call ends up fetching is up to the server. To limit the number of sitemaps a `Parse()` / `ParseContext()` call fetches, use the `SetMaxSitemaps()` function.

The value is an `int`:
- `0`: no limit.
- a positive value: the call fetches at most that many sitemaps. The default is `50000`, which is as many sitemaps as a sitemap index may list according to the sitemaps.org protocol.

```go
s := sitemap.New()
s = s.SetMaxSitemaps(1000)
```
... or ...
```go
s := sitemap.New().SetMaxSitemaps(1000)
```

The limit is one of the call, not of a sitemap index: the sitemaps a `robots.txt` or a sitemap index lists count on all levels together.

- Every sitemap the call requests counts, whether or not the request succeeds.
- A sitemap that is listed more than once is requested, and counted, once.
- The document passed to `Parse()` does not count.

Once the limit is reached, the sitemaps that are left are not fetched, and a `*ParseError` is recorded in `GetErrors()`, once for the call. It names the URL passed to `Parse()`:

```
parse "https://example.com/sitemap-index.xml": limit of 1000 sitemaps reached
```

Reaching the limit does not fail `Parse()`: the URLs of the sitemaps fetched until then are returned. With multi-threading off, the sitemaps are fetched in the order they are listed, so it is the first ones that are fetched. With multi-threading on, which ones are fetched depends on how fast the server answers. Nothing is recorded if no sitemap had to be left out.

Negative values are rejected and an error is recorded in `GetErrors()`. The limit of a call is the one set when the call starts.

See [`examples/maxsitemaps`](examples/maxsitemaps/main.go) for a runnable example.

#### Max URLs

The URLs of a call are kept in memory until the next call, and how many there are is up to the server as well. To limit the number of URLs a `Parse()` / `ParseContext()` call collects, use the `SetMaxURLs()` function.

The value is an `int`:
- `0`: no limit.
- a positive value: the call collects at most that many URLs. The default is `10000000`.

```go
s := sitemap.New()
s = s.SetMaxURLs(100000)
```
... or ...
```go
s := sitemap.New().SetMaxURLs(100000)
```

The limit is one of the call as well: the URLs of all the sitemaps count together. What counts are the URLs that `GetURLs()` returns: an entry that is not valid does not count, and neither does a URL that the patterns set with `SetRules()` leave out.

Once the limit is reached, the call stops collecting:

- what is left of the document that reached the limit is left out unread, so it adds no errors either;
- the sitemaps that are left are not fetched, and a sitemap that was under way is not parsed;
- a `*ParseError` is recorded in `GetErrors()`, once for the call. It names the URL passed to `Parse()`:

```
parse "https://example.com/sitemap-index.xml": limit of 100000 URLs reached
```

Reaching the limit does not fail `Parse()`: the URLs collected until then are returned. With multi-threading off, they are the first ones in the order the sitemaps list them. With multi-threading on, which ones they are depends on how fast the server answers. Nothing is recorded if nothing had to be left out.

The default is a last resort rather than a tight bound: ten million URLs take up gigabytes of memory. Set a lower limit when the documents are not trusted, see [SECURITY.md](SECURITY.md).

Negative values are rejected and an error is recorded in `GetErrors()`. The limit of a call is the one set when the call starts.

See [`examples/maxurls`](examples/maxurls/main.go) for a runnable example.

#### Max concurrency

When multi-threaded parsing is enabled, the sitemaps a sitemap index or a `robots.txt` lists are fetched and parsed concurrently. For very large sitemap indexes this can mean a large number of goroutines, HTTP connections and documents in memory at once. To bound the number of sitemaps that are worked on at the same time across the whole `Parse()` / `ParseContext()` call, use the `SetMaxConcurrency()` function.

The value is an `int`:
- `0`: unlimited concurrency.
- a positive value: at most that many sitemaps are being fetched or parsed at any time. The default is `16`.

A sitemap counts from the moment its request is sent until its content is parsed, so the limit bounds the connections that are open, the CPU cores that are busy and the documents that are held in memory at once. Parsing a sitemap takes several times the memory of the document itself; a lower limit trades speed for a smaller footprint.

A sitemap that waits for its turn takes up none of these, and no goroutine either: a goroutine is started for a sitemap only when there is a slot for it. However many sitemaps an index lists, the goroutines of the parser are those of the sitemaps being worked on, plus one for every sitemap index whose sitemaps are not finished yet. With `SetMaxConcurrency(0)` a goroutine is started, and a request sent, for every sitemap listed at once.

Negative values are rejected and an error is recorded in `GetErrors()`.

```go
s := sitemap.New()
s = s.SetMaxConcurrency(8)
```
... or ...
```go
s := sitemap.New().SetMaxConcurrency(8)
```

Cancelling the supplied `context.Context` while sitemaps wait for a slot ends the wait at once. The sitemaps that were still waiting are not fetched, and nothing is recorded for them: that the call was cut short is reported once for the call, see [Parse with context](#parse-with-context).

#### Multi-threading

By default, the package uses multi-threading to fetch and parse sitemaps concurrently.
To set the multi-thread flag on/off, use the `SetMultiThread()` function.

With multi-threading on, each sitemap is fetched, unzipped and decoded by a goroutine of its own, so the sitemaps of an index are parsed on as many CPU cores as `SetMaxConcurrency()` allows. The URLs of a sitemap stay together in `GetURLs()`, in the order the sitemap lists them; which sitemap comes first depends on which one is finished first.

With multi-threading off, the sitemaps are fetched and parsed one at a time and in the order they are listed, so no more than one request is in flight at any time. This goes for the sitemaps a `robots.txt` names as well as for those of a sitemap index. `SetMaxConcurrency()` has no effect in this case.

```go
s := sitemap.New()
s = s.SetMultiThread(false)
```
... or ...
```go
s := sitemap.New().SetMultiThread(false)
```

See [`examples/multithread`](examples/multithread/main.go) for a runnable example.

#### Follow rules

To set the follow rules, use the `SetFollow()` function. It should be specified a `[]string` value.
It is a list of regular expressions. Only the sitemaps whose URL matches one of these expressions are fetched and parsed, whether a sitemap index lists them or a `robots.txt` names them. A sitemap index that is not followed takes the sitemaps it lists with it. The URL passed to `Parse()` is always fetched.
If no follow rules are provided, all sitemaps are followed.
Patterns longer than 1,000 characters are rejected and reported via `GetErrors()`.

The expressions are matched against the absolute URL of a sitemap, a relative one being resolved first. They match anywhere in the URL unless they are anchored: `example\.com` also matches `https://example.com.evil.test/sitemap.xml`, `^https://example\.com/` does not.

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

See [`examples/follow`](examples/follow/main.go) for a runnable example.

#### URL rules

To set the URL rules, use the `SetRules()` function. It should be specified a `[]string` value.
It is a list of regular expressions. Only URLs that match one of these expressions will be included in the final result.
If no rules are provided, all URLs found are included.
Patterns longer than 1,000 characters are rejected and reported via `GetErrors()`.

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

#### HTTP client

To use a custom HTTP client for all requests, use the `SetHTTPClient()` function.
This is useful when you need a custom transport, proxy, TLS configuration, or
authentication via a custom `http.RoundTripper`.

When a custom client is provided, `SetFetchTimeout` has no effect — the client's
own `Timeout` field controls the request deadline. Pass `nil` to reset to the
default client behaviour.

```go
s := sitemap.New()
s = s.SetHTTPClient(&http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
    },
})
```
... or ...
```go
s := sitemap.New().SetHTTPClient(&http.Client{Timeout: 30 * time.Second})
```

See [`examples/httpclient`](examples/httpclient/main.go) for a runnable example.

#### Strict mode

By default, the parser operates in **tolerant mode**: relative URLs found in `<loc>` elements are automatically resolved against the parent sitemap URL. This handles real-world sitemaps that may not fully comply with the specification.

To enable **strict mode**, use the `SetStrict()` function. In strict mode, all URL entries are validated per the [sitemaps.org protocol](http://www.sitemaps.org/protocol.html):
- `<loc>` must be an absolute HTTP or HTTPS URL
- `<loc>` must use the same host and protocol as the sitemap file (for a sitemap reached through a redirect: as the URL it was served from, see [Redirects](#redirects))
- `<loc>` must not exceed 2,048 characters
- `<priority>` must be between `0.0` and `1.0` inclusive (if present)
- `<lastmod>` and `<priority>` must hold a value that can be parsed (if present)
- The document must be well-formed XML, otherwise it is rejected as a whole and reported as a `*ParseError`

In **tolerant mode** (the default):
- Relative `<loc>` URLs are resolved against the parent sitemap URL
- `<loc>` URLs exceeding 2,048 characters after resolution are rejected
- `<priority>` values outside `[0.0, 1.0]` are accepted as-is
- A `<lastmod>` or `<priority>` that cannot be parsed is left unset (`nil`) and reported, the entry itself is kept
- XML mistakes that can be read past are accepted: an unescaped `&`, an unknown entity such as `&nbsp;`, a missing end tag

Entries that fail validation are skipped and reported via `GetErrors()`.

An entry without a location is one of them in both modes. A `<url>` or `<sitemap>` whose `<loc>` is missing, empty or holds only whitespace is skipped and reported as a `*ValidationError`. The entry has no URL of its own, so the error names the sitemap it stands in:

```
validate "https://example.com/sitemap.xml": <loc> of an entry is empty or missing
```

An RSS `<item>` without a `<link>` and an Atom `<entry>` without a link are skipped without an error, since a feed item is not required to have one.

The sitemaps a `robots.txt` names are checked before they are fetched as well. The value of a `Sitemap:` line has to be an HTTP or HTTPS URL of at most 2,048 characters. Tolerant mode resolves a relative one against the URL of the `robots.txt`, strict mode requires an absolute one. The sitemap may be on another host than the `robots.txt` in both modes, which the protocol allows. A value that is rejected is skipped and reported as a `*ValidationError`:

```
validate "ftp://example.com/sitemap.xml": unsupported scheme "ftp"
```

A value that cannot be parsed never costs more than its own entry. The numbers and dates of the extensions (`<video:duration>`, `<video:rating>`, `<video:view_count>`, `<video:expiration_date>`, `<video:publication_date>`, `<news:publication_date>`) are treated the same way in both modes: the field is left unset (`nil`) and the entry is kept. Every such value is reported via `GetErrors()` as a `*ValidationError` for the page it belongs to:

```
validate "https://example.com/page": invalid <lastmod> value "2024-01-15 10:30:00"
```

An empty date is no such value. A `<lastmod>`, `<news:publication_date>`, `<video:expiration_date>` or `<video:publication_date>` that is empty or holds only whitespace is read as if the element were not there: the field is `nil`, not the zero time, and nothing is reported in either mode. The exception is the publication date of a news entry, which strict mode requires, see [GetURLs](#geturls). An empty number (`<priority>`, `<video:duration>`, `<video:rating>`, `<video:view_count>`) reads as `0`.

See [`examples/tolerant`](examples/tolerant/main.go) for a runnable example of how the two modes treat a sitemap with mistakes in it, an entry without a location among them.

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

### Read configuration

Each configuration setting can be read back via a corresponding `Get*` method. All getters are thread-safe.

| Getter | Return type | Description |
|---|---|---|
| `GetUserAgent()` | `string` | Current user agent string |
| `GetFetchTimeout()` | `uint16` | Fetch timeout in seconds |
| `GetMultiThread()` | `bool` | Whether multi-threaded fetching is enabled |
| `GetMaxResponseSize()` | `int64` | Maximum HTTP response size in bytes |
| `GetMaxDepth()` | `int` | Maximum sitemap index recursion depth |
| `GetMaxConcurrency()` | `int` | Maximum number of sitemaps fetched or parsed at the same time (`0` = unlimited) |
| `GetMaxSitemaps()` | `int` | Maximum number of sitemaps a call fetches (`0` = unlimited) |
| `GetMaxURLs()` | `int` | Maximum number of URLs a call collects (`0` = unlimited) |
| `GetFollow()` | `[]string` | Copy of the follow regex pattern list |
| `GetRules()` | `[]string` | Copy of the URL filter regex pattern list |
| `GetHTTPClient()` | `*http.Client` | Custom HTTP client, or `nil` if using the default |
| `GetStrict()` | `bool` | Whether strict validation mode is enabled |

`GetFollow()` and `GetRules()` return copies — mutating the returned slice does not affect the parser's internal state.

```go
s := sitemap.New().SetMaxConcurrency(8).SetStrict(true)
fmt.Println(s.GetMaxConcurrency()) // 8
fmt.Println(s.GetStrict())         // true
```

### Thread safety

All public methods on `*S` are safe to call from multiple goroutines. Internal state (configuration, collected URLs, errors) is protected by a mutex.

The mutex is not held while a sitemap is fetched, unzipped or decoded, only while what the sitemap yielded is added to the results. The getters can therefore be called while `Parse()` is running without waiting for it: `GetURLs()`, `GetURLCount()` and `GetErrors()` return what has been collected so far. The URLs of a sitemap appear all at once, when that sitemap has been parsed.

However, two important constraints apply:

- **Concurrent `Parse()` / `ParseContext()` calls on the same instance are serialised.** A second call blocks until the first completes. If you need to parse multiple sitemaps concurrently, create a separate `*S` instance per goroutine with `New()`.
- **Configure before parsing.** Calling a `Set*` method while `Parse()` is running on the same instance is safe (the write is mutex-protected), but the outcome is non-deterministic — the new value may or may not be picked up mid-parse. Set all options before calling `Parse()`.

**Deadlock note:** a goroutine gives its `SetMaxConcurrency` slot back as soon as its sitemap is fetched and parsed, before the sitemaps that one lists are followed. This prevents a goroutine from holding a slot while it waits for the slots of the child sitemaps, which would otherwise deadlock when a `robots.txt` or a sitemap index leads to further sitemap indexes.

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

The error `Parse()` returns is about the document at `url` itself. It is `nil` if that document was fetched and parsed, otherwise it tells why it was not:

| Error returned | Meaning |
|---|---|
| `*ValidationError` | `url` is not an HTTP or HTTPS URL with a host (checked when the content is to be fetched) |
| `*NetworkError` | The document could not be fetched |
| `*ParseError` | The document could not be parsed: it is not a sitemap, it is empty, its XML or gzip content is broken, it expands beyond the size limit |
| an untyped error | A configuration error is outstanding, so nothing was parsed, see [Reusing an instance](#reusing-an-instance) |

Except for the last, the error returned is the very one `GetErrors()` holds about the document.

What goes wrong further on does not fail the call and is reported via [`GetErrors()`](#geterrors) only: a sitemap the document lists that cannot be fetched or parsed, an entry that is not valid, a limit being reached (see [Max depth](#max-depth), [Max sitemaps](#max-sitemaps) and [Max URLs](#max-urls)). A `nil` error therefore does not mean that nothing was skipped, and `GetErrors()` is worth checking after every call:

```go
s, err := sitemap.New().Parse("https://www.sitemaps.org/sitemap.xml", nil)
if err != nil {
    // The document at the URL could not be fetched or parsed.
    log.Fatalf("parse error: %v", err)
}
for _, e := range s.GetErrors() {
    // A sitemap it lists, or an entry, was skipped, or a limit was reached.
    log.Printf("warning: %v", e)
}
```

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
`GetURLs()`.

A call without a deadline ends when the sitemaps are worked through, or when a
limit of the call is reached (see [Max sitemaps](#max-sitemaps) and
[Max URLs](#max-urls)). The limits bound what a call fetches and collects, not
how long it takes: a deadline is what bounds the time.

A call that was cut short returns a `*ParseError` that names the URL passed to
`ParseContext()` and wraps the error of the context, so `errors.Is` tells why
the call ended:

```go
if errors.Is(err, context.DeadlineExceeded) {
    // the deadline passed
}
if errors.Is(err, context.Canceled) {
    // ctx was cancelled
}
```

The same error is recorded in `GetErrors()`, once for the call: in
multi-threaded and in sequential mode alike, and however many sitemaps were
not fetched. Nothing is recorded for the sitemaps the call did not get to. A
request that was under way is reported in addition, as the `*NetworkError` of
its sitemap, which wraps the error of the context as well.

When it is the request for the URL passed to `ParseContext()` that is cut
short, the call returns and records that `*NetworkError` alone.

See [`examples/context`](examples/context/main.go) for a runnable example.

### Redirects

HTTP redirects are followed. A sitemap that is reached through a redirect is treated as located at the URL it was finally served from, not at the URL that was requested:

- relative URLs in it are resolved against that URL (tolerant mode),
- the URLs it lists have to use the host and protocol of that URL (strict mode),
- the errors about the document name that URL.

So when `http://example.com/sitemap.xml` redirects to `https://www.example.com/sitemap.xml`, a `<loc>/page</loc>` in it yields `https://www.example.com/page`. Strict mode accepts the URLs on `https://www.example.com` and rejects those on `http://example.com`, the sitemap file not being there.

This goes for the URL passed to `Parse()` and for every sitemap fetched on the way alike: the sitemaps of a sitemap index and those a `robots.txt` names. When the content is passed in through `urlContent`, nothing is fetched and the document is located at the URL given.

Which redirects are followed is up to the HTTP client. The default one stops after 10 consecutive requests, as Go's `http.Client` does by default; a client set with `SetHTTPClient()` decides in its `CheckRedirect`. A fetch that fails is reported as a `*NetworkError` naming the URL that was requested.

See [`examples/redirect`](examples/redirect/main.go) for a runnable example.

### Reusing an instance

An instance can be used for any number of `Parse()` / `ParseContext()` calls. Every call starts from a clean state: the URLs and errors collected by the previous call are discarded first, so `GetURLs()` and `GetErrors()` always describe the most recent call only. This also holds when a call returns early, for example because the input URL is invalid.

```go
s := sitemap.New()

for _, url := range []string{"https://example.com/sitemap.xml", "https://example.org/sitemap.xml"} {
    if _, err := s.Parse(url, nil); err != nil {
        log.Printf("%s: %v", url, err)
        continue
    }
    fmt.Printf("%s: %d URLs, %d errors\n", url, s.GetURLCount(), s.GetErrorsCount())
}
```

Configuration errors are the exception, because they belong to the instance rather than to a single call. A `*ConfigError` recorded by a `Set*` method stays in `GetErrors()`, and while any of them is outstanding `Parse()` does not parse anything and returns the error `errors occurred before parsing, see GetErrors() for details`. Calling the same setter again with a valid value clears its error:

```go
s := sitemap.New().SetMaxDepth(0) // invalid: recorded as a *ConfigError

_, err := s.Parse(url, nil) // err: errors occurred before parsing, see GetErrors() for details

s.SetMaxDepth(5)           // valid: clears the error recorded for maxDepth
_, err = s.Parse(url, nil) // parses normally
```

Each setter call replaces the errors recorded by the previous call for the same setting, so repeating an invalid value does not accumulate errors.

See [`examples/reuse`](examples/reuse/main.go) for a runnable example.

### Character encoding

The sitemaps.org protocol requires sitemaps to be UTF-8 encoded, but XML documents in other encodings are accepted as well. The parser honours the encoding named in the XML declaration and transcodes the document while reading it, so every string it returns is UTF-8:

```go
// "\xe9" is the letter "é" in ISO-8859-1
content := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" +
    "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n" +
    "  <url><loc>https://example.com/caf\xe9</loc></url>\n" +
    "</urlset>"

s, err := sitemap.New().Parse("https://example.com/sitemap.xml", &content)
// s.GetURLs()[0].Loc == "https://example.com/caf%C3%A9"
```

This applies to every XML format (sitemap index, urlset, RSS and Atom), in tolerant and in strict mode alike. The encodings of the [WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) are supported, among them the `ISO-8859` and `windows-125x` families, `US-ASCII`, `KOI8-R`, `Shift_JIS`, `EUC-JP`, `EUC-KR`, `GBK`, `gb18030` and `Big5`.

Limitations:
- A document that declares any other encoding is not parsed; a `*ParseError` naming the encoding is reported via `GetErrors()`, and returned by `Parse()` when it is the document `Parse()` was called for.
- Only the XML declaration is consulted. The `charset` parameter of the HTTP `Content-Type` header is ignored, and a document without a declared encoding is read as UTF-8.
- UTF-16 and UTF-32 documents are not supported.
- Plain text sitemaps and `robots.txt` files carry no encoding declaration and are not transcoded.

See [`examples/encoding`](examples/encoding/main.go) for a runnable example.

### Results

After parsing, you can retrieve the results using the following methods:

#### GetURLs

Returns all parsed URLs as a `[]URL` slice.

```go
urls := s.GetURLs()
```

Each `URL` struct contains the following fields:
- `Loc` (`string`) — the URL location; never empty, an entry without a `<loc>` is skipped
- `LastMod` (`*LastModTime`) — last modification time (embeds `time.Time`), may be `nil`; also `nil` when the element is empty or the value cannot be parsed, see [Strict mode](#strict-mode)
- `ChangeFreq` (`*URLChangeFreq`) — change frequency hint, may be `nil`. Use the exported constants for comparison: `ChangeFreqAlways`, `ChangeFreqHourly`, `ChangeFreqDaily`, `ChangeFreqWeekly`, `ChangeFreqMonthly`, `ChangeFreqYearly`, `ChangeFreqNever`
- `Priority` (`*float32`) — crawl priority between 0.0 and 1.0, may be `nil`; also `nil` when the value cannot be parsed
- `Images` (`[]Image`) — images associated with this URL via the Google Image Sitemap extension, may be `nil`
- `News` (`*News`) — news metadata associated with this URL via the Google News Sitemap extension, may be `nil`
- `Videos` (`[]Video`) — videos associated with this URL via the Google Video Sitemap extension, may be `nil`
- `Hreflangs` (`[]AlternateLink`) — alternate language/region versions of this URL via the XHTML extension, may be `nil`

Each `Image` struct contains the following fields (all `string`):
- `Loc` — image URL (required by the spec; images with an empty `Loc` are silently dropped in tolerant mode, or produce an error in strict mode)
- `Title` — image title (optional)
- `Caption` — image caption (optional)
- `GeoLocation` — geographic location of the image subject (optional)
- `License` — URL of the image licence (optional)

See [`examples/image`](examples/image/main.go) for a runnable example.

Each `News` struct contains:
- `Publication` (`NewsPublication`) — publication metadata:
  - `Name` (`string`) — publication name (required in strict mode)
  - `Language` (`string`) — BCP 47 language code, e.g. `"en"` (required in strict mode)
- `PublicationDate` (`*LastModTime`) — article publication date; embeds `time.Time`, `nil` if absent or empty (required in strict mode)
- `Title` (`string`) — article title (required in strict mode)

In strict mode, all four required fields (`Title`, `Publication.Name`, `Publication.Language`, `PublicationDate`) must be present; missing fields are each reported via `GetErrors()` and the `News` entry is still included with whatever data was parsed. A `<news:publication_date>` that is there but empty is reported like a missing one (`strict mode: news <publication_date> is empty`). In tolerant mode no validation is performed. A `PublicationDate` that cannot be parsed is left `nil` and reported in both modes.

See [`examples/news`](examples/news/main.go) for a runnable example.

Each `AlternateLink` struct contains:
- `Rel` (`string`) — relationship, should be `"alternate"`
- `Hreflang` (`string`) — language/region code (e.g. `"en"`, `"de-ch"`)
- `Href` (`string`) — the URL of the alternate version

See [`examples/hreflang`](examples/hreflang/main.go) for a runnable example.

Each `Video` struct contains:
- `ThumbnailLoc` (`string`) — thumbnail image URL (required; videos with an empty `ThumbnailLoc` are silently dropped in tolerant mode, or produce an error in strict mode)
- `Title` (`string`) — video title (required in strict mode)
- `Description` (`string`) — video description (required in strict mode)
- `ContentLoc` (`string`) — direct URL to the video file (at least one of `ContentLoc` or `PlayerLoc` required in strict mode)
- `PlayerLoc` (`string`) — URL of an embedded video player
- `Duration` (`*int`) — duration in seconds (1–28800); validated in strict mode if present
- `ExpirationDate` (`*LastModTime`) — date after which the video should not be shown; embeds `time.Time`, `nil` if absent or empty
- `Rating` (`*float32`) — rating between 0.0 and 5.0; validated in strict mode if present
- `ViewCount` (`*int`) — number of views
- `PublicationDate` (`*LastModTime`) — publication date; embeds `time.Time`, `nil` if absent or empty
- `FamilyFriendly` (`string`) — `"yes"` or `"no"`
- `Restriction` (`*VideoRestriction`) — country restriction with `Relationship` (`"allow"`/`"deny"`) and `Value` (space-separated country codes)
- `Platform` (`*VideoPlatform`) — platform restriction with `Relationship` and `Value` (e.g. `"web mobile tv"`)
- `RequiresSubscription` (`string`) — `"yes"` or `"no"`
- `Uploader` (`*VideoUploader`) — uploader name (`Value`) and optional profile URL (`Info`)
- `Live` (`string`) — `"yes"` or `"no"`
- `Tags` (`[]string`) — content tags; maximum 32 validated in strict mode

A `Duration`, `Rating`, `ViewCount`, `ExpirationDate` or `PublicationDate` that cannot be parsed is left `nil` and reported via `GetErrors()` in both modes; the video itself is kept.

See [`examples/video`](examples/video/main.go) for a runnable example.

#### GetURLCount

Returns the number of parsed URLs.

```go
count := s.GetURLCount()
```

#### GetRandomURLs

Returns a slice of `n` randomly selected URLs without duplicates. If `n` exceeds the number of parsed URLs, all of them are returned, in random order. If `n` is zero or negative, the result is an empty slice.

```go
randomURLs := s.GetRandomURLs(5)
```

#### GetErrors

Returns the errors encountered during the most recent `Parse()` / `ParseContext()` call, together with any outstanding configuration errors recorded by the `Set*` methods (see [Reusing an instance](#reusing-an-instance)).

```go
errs := s.GetErrors()
```

Errors are typed and can be inspected with `errors.As`:

| Type | When returned | Useful fields |
|---|---|---|
| `*ConfigError` | A `Set*` method received an invalid value | `Field` (setting name), `Err` (root cause) |
| `*NetworkError` | An HTTP fetch failed | `URL` (requested URL), `Err` (root cause) |
| `*ParseError` | A sitemap document could not be parsed (not a sitemap, empty, broken XML or gzip content, larger than the size limit) | `URL` (sitemap URL; after a redirect, the URL the sitemap was served from), `Err` (root cause) |
| `*ParseError` | The depth limit was reached, see [Max depth](#max-depth) | `URL` (the sitemap index whose sitemaps were not followed), `Err` (root cause) |
| `*ParseError` | The limit on the sitemaps or on the URLs of the call was reached, see [Max sitemaps](#max-sitemaps) and [Max URLs](#max-urls) | `URL` (the URL passed to `Parse()`), `Err` (root cause) |
| `*ParseError` | The call was cut short by its context, see [Parse with context](#parse-with-context) | `URL` (the URL passed to `ParseContext()`), `Err` (the error of the context) |
| `*ValidationError` | A URL or field value failed validation | `URL` (the rejected URL, or the page or sitemap the rejected value belongs to), `Err` (root cause) |

All types implement `Unwrap()`, enabling `errors.Is` traversal to the root cause.

The error that `Parse()` / `ParseContext()` return is one of these as well, see [Parse](#parse).

```go
for _, err := range s.GetErrors() {
    var netErr *sitemap.NetworkError
    if errors.As(err, &netErr) {
        fmt.Printf("fetch failed for %s: %v\n", netErr.URL, netErr.Err)
        continue
    }
    var valErr *sitemap.ValidationError
    if errors.As(err, &valErr) {
        fmt.Printf("validation error for %s: %v\n", valErr.URL, valErr.Err)
        continue
    }
}
```

See [`examples/errors`](examples/errors/main.go) for a runnable example.

#### GetErrorsCount

Returns the number of errors encountered during parsing.

```go
errCount := s.GetErrorsCount()
```

## Examples

Examples can be found in [/examples](https://github.com/aafeher/go-sitemap-parser/tree/main/examples).

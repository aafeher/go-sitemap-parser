# Security Policy

## Supported Versions

Only the latest released version receives security fixes.
Older versions are not backported.

| Version | Supported |
|---------|-----------|
| Latest  | ✅        |
| Older   | ❌        |

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Use GitHub's private vulnerability reporting instead:
**[Report a vulnerability](https://github.com/aafeher/go-sitemap-parser/security/advisories/new)**

Include:
- A description of the vulnerability and its potential impact
- Steps to reproduce or a minimal proof-of-concept
- Affected version(s)

You can expect an acknowledgement within **72 hours** and a status update within **7 days**.
If a fix is warranted, a patched release will be published and you will be credited in the changelog (unless you prefer to remain anonymous).

## Security Considerations

### Network requests

`Parse()` and `ParseContext()` issue HTTP requests to URLs found in the parsed document (sitemap indexes, `robots.txt` `Sitemap:` directives). In environments where the parser runs with access to internal networks, a malicious sitemap could direct it to probe internal endpoints (**SSRF**). Mitigations:

- Supply a custom `*http.Client` via `SetHTTPClient()` with a transport that restricts reachable hosts or uses an egress proxy.
- Use `SetFollow()` to restrict which sitemap URLs are followed. The patterns apply to every sitemap URL found in a document: the entries of a sitemap index and the `Sitemap:` lines of a `robots.txt`. Versions up to and including v1.1.0 did not apply them to the `Sitemap:` lines of a `robots.txt`.
  - Anchor the patterns. A pattern matches anywhere in the URL, so `example\.com` also matches `https://example.com.evil.test/sitemap.xml`; `^https://example\.com/` does not.
  - The patterns are not applied to the URL passed to `Parse()`, and not to the URL a request is redirected to. Restrict redirects with the `CheckRedirect` of a custom `*http.Client`.
- Use `SetMaxDepth()` to limit recursion depth (default: 10).
- Use `SetMaxSitemaps()` to limit the number of requests a document can make the parser send (default: 50,000 sitemaps per call).
- Use `SetMaxConcurrency()` to limit the number of concurrent outbound connections (default: 16).

A URL found in a document is requested only if it is an `http` or `https` URL of at most 2,048 characters.

### Resource exhaustion

A sitemap document can reference tens of thousands of child sitemaps or URLs. Without limits, parsing an adversarial document could exhaust memory or connections:

- `SetMaxResponseSize()` caps the response body size per fetch and the decompressed size of gzip content (default: 50 MB, matching the sitemaps.org protocol limit).
- `SetMaxDepth()` limits sitemap index recursion depth (default: 10). On its own it does not bound a call: an index may list up to 50,000 sitemaps on every level.
- `SetMaxSitemaps()` limits the number of sitemaps a call fetches, on all levels together (default: 50,000), and with it the number of requests a document can make the parser send. `0` lifts the limit.
- `SetMaxURLs()` limits the number of URLs a call collects (default: 10,000,000), and with it the memory the results take up. `0` lifts the limit.
- `SetMaxConcurrency()` bounds the number of sitemaps that are fetched and parsed at the same time (default: 16), and with it the number of documents held in memory and of goroutines running at once. A sitemap that waits for its turn takes up neither. `0` lifts the bound: a goroutine is then started, and a request sent, for every sitemap a document lists.
- Pass a `context.Context` with a deadline via `ParseContext()` to enforce a wall-clock time limit.

Versions up to and including v1.1.0 had no limit on the sitemaps and URLs of a call: against a server that lists sitemaps without end, a `Parse()` call without a deadline went on, and its results grew, for as long as the server answered.

The defaults make every call come to an end, but they are sized for the largest sitemaps the protocol allows, not for untrusted input: a call may still send 50,000 requests and collect 10,000,000 URLs, which take up gigabytes of memory. When the documents are not trusted:

- set `SetMaxSitemaps()` and `SetMaxURLs()` to what the application expects at most, and
- pass a deadline, as the limits bound what a call fetches and collects, not how long a server may take to answer.

The errors of a call are not limited. An entry that is not valid is recorded as an error and does not count towards `SetMaxURLs()`, so the number of errors is bounded only by the number of sitemaps and the response size limit. Lower `SetMaxSitemaps()` and `SetMaxResponseSize()` to bound it.

### XML security

Go's `encoding/xml` package does not expand XML external entities (XXE), so the parser is **not vulnerable to XXE attacks** by default.
Gzip-compressed sitemaps are decompressed with a size limit enforced by `SetMaxResponseSize()`: decompression stops as soon as the output exceeds the limit and the content is rejected, which mitigates zip-bomb style attacks. Versions up to and including v1.1.0 did not enforce this limit on decompressed data.

### TLS verification

By default the parser uses Go's standard `http.Client`, which enforces TLS certificate verification. Disabling verification via a custom transport is the caller's responsibility and is strongly discouraged in production.

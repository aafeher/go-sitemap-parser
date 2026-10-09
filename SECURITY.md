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
- Use `SetMaxConcurrency()` to limit the number of concurrent outbound connections (default: 16).

A URL found in a document is requested only if it is an `http` or `https` URL of at most 2,048 characters.

### Resource exhaustion

A sitemap document can reference tens of thousands of child sitemaps or URLs. Without limits, parsing an adversarial document could exhaust memory or connections:

- `SetMaxResponseSize()` caps the response body size per fetch and the decompressed size of gzip content (default: 50 MB, matching the sitemaps.org protocol limit).
- `SetMaxDepth()` limits sitemap index recursion depth (default: 10).
- `SetMaxConcurrency()` bounds the number of sitemaps that are fetched and parsed at the same time (default: 16), and with it the number of documents held in memory and of goroutines running at once. A sitemap that waits for its turn takes up neither. `0` lifts the bound: a goroutine is then started, and a request sent, for every sitemap a document lists.
- Pass a `context.Context` with a deadline via `ParseContext()` to enforce a wall-clock time limit.

### XML security

Go's `encoding/xml` package does not expand XML external entities (XXE), so the parser is **not vulnerable to XXE attacks** by default.
Gzip-compressed sitemaps are decompressed with a size limit enforced by `SetMaxResponseSize()`: decompression stops as soon as the output exceeds the limit and the content is rejected, which mitigates zip-bomb style attacks. Versions up to and including v1.1.0 did not enforce this limit on decompressed data.

### TLS verification

By default the parser uses Go's standard `http.Client`, which enforces TLS certificate verification. Disabling verification via a custom transport is the caller's responsibility and is strongly discouraged in production.

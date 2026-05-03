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
- Use `SetFollow()` to restrict which sitemap URLs are followed.
- Use `SetMaxDepth()` to limit recursion depth (default: 10).
- Use `SetMaxConcurrency()` to limit the number of concurrent outbound connections (default: 16).

### Resource exhaustion

A sitemap document can reference tens of thousands of child sitemaps or URLs. Without limits, parsing an adversarial document could exhaust memory or connections:

- `SetMaxResponseSize()` caps the response body size per fetch (default: 50 MB, matching the sitemaps.org protocol limit).
- `SetMaxDepth()` limits sitemap index recursion depth (default: 10).
- `SetMaxConcurrency()` bounds concurrent HTTP fetches (default: 16).
- Pass a `context.Context` with a deadline via `ParseContext()` to enforce a wall-clock time limit.

### XML security

Go's `encoding/xml` package does not expand XML external entities (XXE), so the parser is **not vulnerable to XXE attacks** by default.
Gzip-compressed sitemaps are decompressed with a size limit enforced by `SetMaxResponseSize()`, which mitigates zip-bomb style attacks.

### TLS verification

By default the parser uses Go's standard `http.Client`, which enforces TLS certificate verification. Disabling verification via a custom transport is the caller's responsibility and is strongly discouraged in production.

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
  - To keep the parser to the addresses of the public internet, check the address of every connection in the `Control` function of the `net.Dialer` of the client. The function is called with the IP address a connection is about to be made to, once the host of the URL is resolved and before the connection is made, so the check holds however a document spells a host, whatever a name resolves to, and wherever a request is redirected. [`examples/ssrf`](examples/ssrf/main.go) is a runnable example of such a client, with a function that tells the addresses of the public internet from the others.
  - A check of the URL, or of what its host resolves to at the time of the check, can be got round: a name may resolve to an internal address, or to a public one when it is checked and to an internal one when it is connected to, and an address has several spellings (`127.0.0.1`, `[::ffff:127.0.0.1]` and, to many resolvers, `2130706433` are the same machine).
  - Give such a transport no proxy. A client connects to its proxy, so the address `Control` sees would be that of the proxy; where an egress proxy is used, the check is for the proxy to make.
- Use `SetFollow()` to restrict which sitemap URLs are followed. The patterns apply to every sitemap URL found in a document: the entries of a sitemap index and the `Sitemap:` lines of a `robots.txt`. Versions up to and including v1.1.0 did not apply them to the `Sitemap:` lines of a `robots.txt`.
  - Anchor the patterns. A pattern matches anywhere in the URL, so `example\.com` also matches `https://example.com.evil.test/sitemap.xml`; `^https://example\.com/` does not.
  - The patterns are not applied to the URL passed to `Parse()`, and not to the URL a request is redirected to. Restrict redirects with the `CheckRedirect` of a custom `*http.Client`.
  - A pattern is matched against the URL, not against the address its host resolves to. It can keep the parser to the hosts it names; it cannot tell an internal address from a public one.
- `SetStrict(true)` keeps the entries of a sitemap index to the host the index is on: an entry on another host, another port or the other protocol is reported as a `*ValidationError` and is not requested, as the protocol has a sitemap index list the sitemaps of its own host only. It is a part of the protection, not the whole of it:
  - The sitemaps a `robots.txt` names are requested on whichever host they are, in strict mode too: the protocol lets a `robots.txt` name the sitemaps of other hosts.
  - A request that is redirected is followed to wherever it leads, and the entries of a sitemap index that was reached through a redirect are compared with the URL it was served from. An index that is redirected to an internal host may list the sitemaps of that host.
  - The host is compared by its name, not by the address the name resolves to, like a pattern of `SetFollow()`.
- Use `SetMaxDepth()` to limit recursion depth (default: 10).
- Use `SetMaxSitemaps()` to limit the number of requests a document can make the parser send (default: 50,000 sitemaps per call).
- Use `SetMaxConcurrency()` to limit the number of concurrent outbound connections (default: 16).

A URL found in a document is requested only if it is an `http` or `https` URL of at most 2,048 characters that names a host. Versions up to and including v1.2.0 also requested a URL that names a port but no host, such as `http://:8080/sitemap.xml`; Go's HTTP client sends the request for such a URL to the machine the parser runs on. The URL a request is redirected to is not checked by the parser, see above.

### Resource exhaustion

A sitemap document can reference tens of thousands of child sitemaps or URLs. Without limits, parsing an adversarial document could exhaust memory or connections:

- `SetMaxResponseSize()` caps the response body size per fetch and the decompressed size of gzip content (default: 50 MB, matching the sitemaps.org protocol limit). While a document is read it takes up its size in memory, or twice its size if it does not say how large it is or is larger than 50 MB; a document over the limit takes up the limit until it is rejected. A gzip file is in memory together with its decompressed content.
- `SetMaxDepth()` limits sitemap index recursion depth (default: 10). On its own it does not bound a call: an index may list up to 50,000 sitemaps on every level.
- `SetMaxSitemaps()` limits the number of sitemaps a call fetches, on all levels together (default: 50,000), and with it the number of requests a document can make the parser send. The same limit is on the sitemaps a call lists for `GetSitemaps()`, which are kept in memory until the next call like the URLs are. `0` lifts the limit.
- `SetMaxURLs()` limits the number of URLs a call collects (default: 10,000,000), and with it the memory the results take up. `0` lifts the limit.
- `SetMaxValidationErrors()` limits the number of validation errors a call records (default: 100,000), and with it the number of errors the entries of a document can make a call keep. The errors about a sitemap that could not be fetched or parsed are recorded whatever this limit is, and are bounded by `SetMaxSitemaps()`. `0` lifts the limit.
- `SetMaxConcurrency()` bounds the number of sitemaps that are fetched and parsed at the same time (default: 16), and with it the number of documents held in memory and of goroutines running at once. A sitemap that waits for its turn takes up neither. `0` lifts the bound: a goroutine is then started, and a request sent, for every sitemap a document lists.
- Pass a `context.Context` with a deadline via `ParseContext()` to enforce a wall-clock time limit.

Versions up to and including v1.1.0 had no limit on the sitemaps and URLs of a call: against a server that lists sitemaps without end, a `Parse()` call without a deadline went on, and its results grew, for as long as the server answered.

Versions up to and including v1.2.2 kept the location of every sitemap the sitemap indexes of a call listed, in a list of their own that no limit bounded: the list grew to the number of indexes fetched times the entries of each, and was kept until the next call. Against a server that answers every request with a large sitemap index, a call took up gigabytes of memory within the limits of `SetMaxSitemaps()` and `SetMaxURLs()`.

Versions up to and including v1.2.2 had no limit on the errors of a call either. An entry that is not valid is recorded as an error and does not count towards `SetMaxURLs()`, so a document of 50 MB could add some three million errors, and every sitemap of the call as many again: the errors were bounded only by the number of sitemaps and the response size limit.

The defaults make every call come to an end, but they are sized for the largest sitemaps the protocol allows, not for untrusted input: a call may still send 50,000 requests and collect 10,000,000 URLs, which take up gigabytes of memory. When the documents are not trusted:

- set `SetMaxSitemaps()`, `SetMaxURLs()` and `SetMaxValidationErrors()` to what the application expects at most, and
- pass a deadline, as the limits bound what a call fetches and collects, not how long a server may take to answer.

The limits on the URLs and on the validation errors are on their number, not on their size. A URL is returned with what the extensions say about the page, and an error names the URL, or quotes the value, it is about in full, however long that is: either can be as large as the entry it comes from. What bounds the size of everything a call reads is the number of sitemaps and the response size limit, so lower `SetMaxSitemaps()` and `SetMaxResponseSize()` to bound it.

### XML security

Go's `encoding/xml` package does not expand XML external entities (XXE), so the parser is **not vulnerable to XXE attacks** by default.
Gzip-compressed sitemaps are decompressed with a size limit enforced by `SetMaxResponseSize()`: decompression stops as soon as the output exceeds the limit and the content is rejected, which mitigates zip-bomb style attacks. The limit is on all the members of a gzip file together. Versions up to and including v1.1.0 did not enforce this limit on decompressed data.

### TLS verification

By default the parser uses Go's standard `http.Client`, which enforces TLS certificate verification. Disabling verification via a custom transport is the caller's responsibility and is strongly discouraged in production.

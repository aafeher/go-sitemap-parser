package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates how to keep the parser from requesting the addresses of
// an internal network (server-side request forgery, SSRF).
//
// The parser requests the URLs a document lists: the sitemaps of a sitemap
// index and of a robots.txt, and whatever such a request is redirected to. A
// document from a source that is not trusted can therefore direct a parser
// that runs within a network at what only that network reaches, such as a
// service of the machine itself or the metadata service of a cloud.
//
// The host of a URL does not tell where a request goes: a name resolves to
// whatever address its owner gives it, and an address has several spellings.
// What is checked is therefore the address a connection is about to be made
// to, in the Control function of the net.Dialer of a client that is set with
// SetHTTPClient, see publicOnlyClient. The function is called for every
// connection of the client: for the URL passed to Parse, for the sitemaps a
// document lists and for the URLs these are redirected to.
//
// The check tells nothing about which public hosts are requested. SetFollow
// restricts those.
//
// The example runs a local test server that stands for an internal service,
// so it needs no network access. The server counts the requests it receives.
func main() {
	var requests atomic.Int64
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>http://%s/internal-page</loc></url>
</urlset>`, r.Host)
	}))
	defer internal.Close()

	_, port, err := net.SplitHostPort(internal.Listener.Addr().String())
	if err != nil {
		log.Fatalf("address of the test server: %v", err)
	}
	fmt.Printf("internal service: %s\n", internal.URL)

	// The sitemap index of a site that is not trusted. It lists the internal
	// service three times: by its address, by a name that resolves to that
	// address, and by that address written as an IPv6 one.
	index := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>http://127.0.0.1:%[1]s/sitemap.xml</loc></sitemap>
  <sitemap><loc>http://localhost:%[1]s/sitemap.xml</loc></sitemap>
  <sitemap><loc>http://[::ffff:127.0.0.1]:%[1]s/sitemap.xml</loc></sitemap>
</sitemapindex>`, port)

	// The default client requests whatever the index lists, and what the
	// internal service answers is among the URLs of the call.
	fmt.Println("\n=== Default client ===")
	parse(sitemap.New(), index, &requests)

	// None of the three requests is sent: each is refused before a connection
	// is made, and reported as a *sitemap.NetworkError.
	client := publicOnlyClient()
	fmt.Println("\n=== Client that connects to public addresses only ===")
	parse(sitemap.New().SetHTTPClient(client), index, &requests)

	// The patterns of SetFollow are not applied to the URL the call is made
	// for. The check is, as it is made for every connection.
	fmt.Println("\n=== The URL passed to Parse ===")
	_, err = sitemap.New().SetHTTPClient(client).Parse(internal.URL+"/sitemap.xml", nil)
	fmt.Printf("refused: %t\n", errors.Is(err, errNotPublic))
	fmt.Printf("%v\n", err)
}

// parse parses index with s and prints what the sitemaps it lists led to.
// The index is handed to the call, so no request is made for it.
func parse(s *sitemap.S, index string, requests *atomic.Int64) {
	requests.Store(0)

	// The sitemaps are fetched one at a time, so that the URLs and the errors
	// are in the order of the index.
	sm, err := s.SetMultiThread(false).Parse("https://www.example.com/sitemap.xml", &index)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("requests the internal service received: %d\n", requests.Load())
	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", u.Loc)
	}
	for _, e := range sm.GetErrors() {
		// The error of the parser wraps the one of the client, which tells a
		// request that was refused from one that failed.
		var networkErr *sitemap.NetworkError
		if errors.As(e, &networkErr) && errors.Is(e, errNotPublic) {
			fmt.Printf("  - refused: %s\n", networkErr.URL)
			continue
		}
		fmt.Printf("  - %v\n", e)
	}
}

// errNotPublic is the error of a connection publicOnlyClient refused to make.
var errNotPublic = errors.New("not an address of the public internet")

// publicOnlyClient returns an HTTP client that connects to the addresses of
// the public internet only.
//
// Its dialer calls Control for every connection, once the host of the URL is
// resolved and before the connection is made, with the IP address the
// connection goes to. A check of the URL, or of what its host resolves to at
// the time of the check, can be got round: by a name that resolves to an
// internal address, by one that resolves to a public address when it is
// checked and to an internal one when it is connected to, or by a redirect.
// Here the address that is checked is the one that is connected to.
func publicOnlyClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			// An address that cannot be read is refused like one that is
			// not public.
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %q", errNotPublic, address)
			}
			if !isPublic(addrPort.Addr()) {
				return fmt.Errorf("%w: %s", errNotPublic, addrPort.Addr())
			}
			return nil
		},
	}

	// The transport is the default one of net/http but for its dialer and its
	// proxy. It uses none: a client connects to its proxy, so the address of
	// the proxy would be the one that is checked, while the proxy connects to
	// the host of the URL. Where a proxy is used, the check is for the proxy
	// to make.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil

	return &http.Client{
		// SetFetchTimeout has no effect on a client set with SetHTTPClient.
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

var (
	// globalUnicast is the range the public addresses of IPv6 are assigned
	// from. An IPv6 address outside it is of a special kind, as the loopback
	// address and the unique local, link-local and multicast ones are, or is
	// not assigned.
	globalUnicast = netip.MustParsePrefix("2000::/3")

	// nat64 is the range of the IPv4 addresses that are reached through a
	// NAT64 gateway, as they are in a network that has IPv6 addresses only.
	// The last four bytes of such an address are the IPv4 address.
	nat64 = netip.MustParsePrefix("64:ff9b::/96")
)

// notPublic lists the ranges of IPv4, and those within the global unicast
// range of IPv6, that are no part of the public internet. Most of them are
// entries of the special-purpose address registries of IANA.
//
// What is internal depends on the network, though: add the ranges yours uses
// for its own services that are not in the list.
var notPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // this network: 0.0.0.0 is the machine itself to many systems
	netip.MustParsePrefix("10.0.0.0/8"),      // private
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space, in use within the networks of providers
	netip.MustParsePrefix("127.0.0.0/8"),     // loopback: the machine the parser runs on
	netip.MustParsePrefix("169.254.0.0/16"),  // link-local: the metadata service of a cloud is here
	netip.MustParsePrefix("172.16.0.0/12"),   // private
	netip.MustParsePrefix("192.0.0.0/24"),    // protocol assignments of the IETF
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("192.168.0.0/16"),  // private
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("224.0.0.0/4"),     // multicast
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and broadcast
	netip.MustParsePrefix("2001::/23"),       // protocol assignments of the IETF, Teredo among them
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: holds an IPv4 address
}

// isPublic tells whether addr is an address of the public internet.
func isPublic(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}

	// The zone of an IPv6 address names a network interface and is no part of
	// a range. An IPv4 address that is written as an IPv6 one, such as
	// ::ffff:127.0.0.1, is in the ranges of IPv4.
	addr = addr.WithZone("").Unmap()

	// So is an address that is reached through a NAT64 gateway: the gateway
	// connects to the IPv4 address, which may be an internal one of its
	// network.
	if nat64.Contains(addr) {
		raw := addr.As16()
		addr = netip.AddrFrom4([4]byte(raw[12:]))
	}

	if addr.Is6() && !globalUnicast.Contains(addr) {
		return false
	}
	for _, prefix := range notPublic {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

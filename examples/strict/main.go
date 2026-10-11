package main

import (
	"errors"
	"fmt"
	"github.com/aafeher/go-sitemap-parser"
	"log"
)

func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// create new instance with strict mode enabled
	// In strict mode, all <loc> URLs must be absolute HTTP(S), on the same host
	// and protocol as the sitemap file, and no longer than 2,048 characters.
	// See rules() below for what that means for an entry.
	s := sitemap.New().SetStrict(true).SetFetchTimeout(5).SetMultiThread(false)
	sm, err := s.Parse(url, nil)
	if err != nil {
		log.Printf("%v", err)
	}

	// Print the errors (in strict mode, non-compliant URLs are reported here)
	if sm.GetErrorsCount() > 0 {
		log.Println("parsing has errors:")
		for i, err := range sm.GetErrors() {
			log.Printf("%d: %v", i+1, err)
		}
	}

	// GetURLCount()
	count := sm.GetURLCount()
	fmt.Printf("Sitemaps of %s contains %d valid URLs.\n\n", url, count)

	// GetURLs()
	for i, u := range sm.GetURLs() {
		fmt.Printf("%d. url -> Loc: %s\n", i, u.Loc)
	}

	fmt.Println()
	rules()

	fmt.Println()
	extensions()
}

// rules shows what strict mode requires of the entries of a sitemap, on a
// sitemap that is passed in directly, so this part runs without network access.
//
// A URL has to be on the host and protocol of the sitemap. The name of a host
// is not case-sensitive, and a URL that names no port is on the default port
// of its protocol, so neither capital letters nor ":443" make for another
// host. A subdomain and another port do.
//
// A URL must hold no space, which it has to give as "%20", and none of the
// characters " < > \ ^ ` { | }, which have no place in a URL either: a vertical
// bar has to be given as "%7C". A square bracket has a place only around an
// IPv6 address that is the host, and has to be given as "%5B" or "%5D"
// everywhere else. A percent sign has to begin such an encoding, so the sign
// itself has to be given as "%25". The <changefreq> and <priority> of an entry
// must hold a value of the protocol.
//
// An entry that does not meet a requirement is skipped and reported.
func rules() {
	url := "https://example.com/sitemap.xml"
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><changefreq>weekly</changefreq><priority>1.0</priority></url>
  <url><loc>https://EXAMPLE.com/capitals</loc></url>
  <url><loc>https://example.com:443/default-port</loc></url>
  <url><loc>https://example.com/search?q=summer%20sale</loc></url>
  <url><loc>https://example.com:8443/other-port</loc></url>
  <url><loc>https://www.example.com/subdomain</loc></url>
  <url><loc>http://example.com/other-protocol</loc></url>
  <url><loc>/relative</loc></url>
  <url><loc>https://example.com/search?q=summer sale</loc></url>
  <url><loc>https://example.com/search?tags=summer|sale</loc></url>
  <url><loc>https://example.com/search?tags[]=summer</loc></url>
  <url><loc>https://example.com/search?discount=100%</loc></url>
  <url><loc>https://example.com/change-frequency</loc><changefreq>Weekly</changefreq></url>
  <url><loc>https://example.com/priority</loc><priority>1.5</priority></url>
</urlset>`

	s := sitemap.New().SetStrict(true)
	sm, err := s.Parse(url, &content)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Strict mode accepts %d of the 14 entries of %s:\n", sm.GetURLCount(), url)
	for _, u := range sm.GetURLs() {
		fmt.Printf("  %s\n", u.Loc)
	}
	fmt.Printf("It rejects the other %d:\n", sm.GetErrorsCount())
	for _, e := range sm.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

// extensions shows what strict mode requires of the images, the videos and the
// alternate links of an entry, on a sitemap that is passed in directly as well.
//
// The URL an image, a video or an alternate link is known by, the location of
// the image, the thumbnail of the video and the target of the link, has to be
// an absolute HTTP or HTTPS URL. It may be on any host. An image, a video or a
// link that has no such URL is left out and reported.
//
// The other URLs of an image and of a video, the licence, the content, the
// player and the page about the uploader, need not be given. One that is given
// has to be such a URL too; one that is not is reported, and the image or the
// video is kept with it. So is a video that lacks a field or has one out of
// range.
//
// The page itself is kept in every case. An error names the URL that is
// rejected, or the page when it is not a URL that is wrong.
func extensions() {
	url := "https://example.com/sitemap.xml"
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>https://example.com/product</loc>
    <image:image>
      <image:loc>https://cdn.example.net/product.jpg</image:loc>
      <image:license>CC BY 4.0</image:license>
    </image:image>
    <image:image>
      <image:loc>/images/product-2.jpg</image:loc>
    </image:image>
    <video:video>
      <video:thumbnail_loc>https://cdn.example.net/product-video.jpg</video:thumbnail_loc>
      <video:title>Product video</video:title>
      <video:content_loc>ftp://cdn.example.net/product.mp4</video:content_loc>
      <video:duration>0</video:duration>
      <video:view_count>-1</video:view_count>
    </video:video>
    <xhtml:link rel="alternate" hreflang="de"/>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/product"/>
  </url>
</urlset>`

	s := sitemap.New().SetStrict(true)
	sm, err := s.Parse(url, &content)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		fmt.Printf("Strict mode keeps %s with %d of its 2 images, its video and %d of its 2 alternate links:\n",
			u.Loc, len(u.Images), len(u.Hreflangs))
		for _, image := range u.Images {
			fmt.Printf("  image %s, licence %q\n", image.Loc, image.License)
		}
		for _, video := range u.Videos {
			fmt.Printf("  video %s, content %s\n", video.ThumbnailLoc, video.ContentLoc)
		}
		for _, link := range u.Hreflangs {
			fmt.Printf("  alternate link %s\n", link.Href)
		}
	}
	fmt.Printf("It reports %d errors, each with the URL it is about:\n", sm.GetErrorsCount())
	for _, e := range sm.GetErrors() {
		var validationErr *sitemap.ValidationError
		if errors.As(e, &validationErr) {
			fmt.Printf("  - %-36q %v\n", validationErr.URL, validationErr.Err)
		}
	}
}

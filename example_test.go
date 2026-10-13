package sitemap_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// A document that is handed to the call is parsed as the one at the URL, without being
// fetched. With nil in its place, the document is fetched, see the example of Parse.
func Example() {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><lastmod>2024-03-01</lastmod></url>
  <url><loc>https://example.com/about</loc></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	fmt.Println(s.GetURLCount(), "URLs")
	for _, u := range s.GetURLs() {
		fmt.Println(u.Loc)
	}
	// Output:
	// 2 URLs
	// https://example.com/
	// https://example.com/about
}

// The Set methods return the instance, so they can be chained.
func ExampleNew() {
	s := sitemap.New().
		SetUserAgent("my-crawler/1.0").
		SetFetchTimeout(10).
		SetMaxDepth(3)

	fmt.Println(s.GetUserAgent())
	fmt.Println(s.GetFetchTimeout())
	fmt.Println(s.GetMaxDepth())
	fmt.Println(s.GetStrict())
	// Output:
	// my-crawler/1.0
	// 10
	// 3
	// false
}

// A call follows the sitemaps the document at its URL lists: here a robots.txt names a
// sitemap index, which lists two sitemaps. The site is served by a local test server, and
// "{site}" stands for its address.
func ExampleS_Parse() {
	documents := map[string]string{
		"/robots.txt": "User-agent: *\nSitemap: {site}/sitemap-index.xml\n",
		"/sitemap-index.xml": `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>{site}/sitemap-pages.xml</loc></sitemap>
  <sitemap><loc>{site}/sitemap-blog.xml</loc></sitemap>
</sitemapindex>`,
		"/sitemap-pages.xml": `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>{site}/</loc></url>
  <url><loc>{site}/about</loc></url>
</urlset>`,
		"/sitemap-blog.xml": `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>{site}/blog/first-post</loc></url>
</urlset>`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.ReplaceAll(documents[r.URL.Path], "{site}", "http://"+r.Host))
	}))
	defer server.Close()

	// The sitemaps are fetched one at a time, so that their URLs are in the same order
	// every time.
	s := sitemap.New().SetMultiThread(false)
	if _, err := s.Parse(server.URL+"/robots.txt", nil); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println(strings.TrimPrefix(u.Loc, server.URL))
	}
	// Output:
	// /
	// /about
	// /blog/first-post
}

// The pages of an RSS feed are its items. The date of a page is the <pubDate> of its item.
func ExampleS_Parse_rss() {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example blog</title>
    <item>
      <link>https://example.com/blog/first-post</link>
      <pubDate>Mon, 15 Jan 2024 10:30:00 GMT</pubDate>
    </item>
    <item>
      <link>https://example.com/blog/second-post</link>
    </item>
  </channel>
</rss>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/feed.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		if u.LastMod == nil {
			fmt.Println(u.Loc, "(no date)")
			continue
		}
		fmt.Println(u.Loc, u.LastMod.Format(time.RFC3339))
	}
	// Output:
	// https://example.com/blog/first-post 2024-01-15T10:30:00Z
	// https://example.com/blog/second-post (no date)
}

// A text sitemap has a URL on a line. Empty lines and comments are skipped.
func ExampleS_Parse_text() {
	content := `# pages
https://example.com/
https://example.com/about

https://example.com/contact
`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.txt", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println(u.Loc)
	}
	// Output:
	// https://example.com/
	// https://example.com/about
	// https://example.com/contact
}

// A call is cut short when its context is done. The error it returns then wraps the error of
// the context, so errors.Is matches it.
func ExampleS_ParseContext() {
	// The server does not answer before the request is given up.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	s := sitemap.New()
	_, err := s.ParseContext(ctx, server.URL+"/sitemap.xml", nil)

	fmt.Println(errors.Is(err, context.DeadlineExceeded))
	// Output:
	// true
}

// Only the sitemaps whose URL matches a pattern are fetched. The others are still among the
// sitemaps the call lists.
func ExampleS_SetFollow() {
	documents := map[string]string{
		"/sitemap-index.xml": `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>{site}/sitemap-products.xml</loc></sitemap>
  <sitemap><loc>{site}/sitemap-blog.xml</loc></sitemap>
</sitemapindex>`,
		"/sitemap-products.xml": `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>{site}/products/1</loc></url>
</urlset>`,
		"/sitemap-blog.xml": `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>{site}/blog/first-post</loc></url>
</urlset>`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.ReplaceAll(documents[r.URL.Path], "{site}", "http://"+r.Host))
	}))
	defer server.Close()

	s := sitemap.New().SetFollow([]string{`/sitemap-blog\.xml$`})
	if _, err := s.Parse(server.URL+"/sitemap-index.xml", nil); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println("URL:", strings.TrimPrefix(u.Loc, server.URL))
	}
	for _, listed := range s.GetSitemaps() {
		fmt.Println("sitemap:", strings.TrimPrefix(listed.Loc, server.URL))
	}
	// Output:
	// URL: /blog/first-post
	// sitemap: /sitemap-products.xml
	// sitemap: /sitemap-blog.xml
}

// Only the URLs that match a pattern are collected.
func ExampleS_SetRules() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><loc>https://example.com/blog/first-post</loc></url>
  <url><loc>https://example.com/products/1</loc></url>
  <url><loc>https://example.com/blog/second-post</loc></url>
</urlset>`

	s := sitemap.New().SetRules([]string{`^https://example\.com/blog/`})
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println(u.Loc)
	}
	// Output:
	// https://example.com/blog/first-post
	// https://example.com/blog/second-post
}

// Tolerant mode, the default, makes the best of a sitemap that does not keep to the protocol.
// Strict mode skips and reports the entries that do not.
func ExampleS_SetStrict() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><loc>/about</loc></url>
  <url><loc>https://example.com/summer sale</loc></url>
  <url><loc>https://other.example/page</loc></url>
</urlset>`

	for _, strict := range []bool{false, true} {
		s := sitemap.New().SetStrict(strict)
		if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
			log.Fatal(err)
		}

		fmt.Println("strict:", strict)
		for _, u := range s.GetURLs() {
			fmt.Println("  URL:", u.Loc)
		}
		for _, err := range s.GetErrors() {
			fmt.Println("  error:", err)
		}
	}
	// Output:
	// strict: false
	//   URL: https://example.com/
	//   URL: https://example.com/about
	//   URL: https://example.com/summer%20sale
	//   URL: https://other.example/page
	// strict: true
	//   URL: https://example.com/
	//   error: validate "/about": strict mode: unsupported scheme ""
	//   error: validate "https://example.com/summer sale": strict mode: URL contains a space
	//   error: validate "https://other.example/page": strict mode: host "other.example" does not match sitemap host "example.com"
}

// A call collects no more URLs than the limit allows, and records that it had to leave some
// out. errors.Is tells the error of a limit from the others.
func ExampleS_SetMaxURLs() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/1</loc></url>
  <url><loc>https://example.com/2</loc></url>
  <url><loc>https://example.com/3</loc></url>
</urlset>`

	s := sitemap.New().SetMaxURLs(2)
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	fmt.Println(s.GetURLCount(), "URLs")
	for _, err := range s.GetErrors() {
		if errors.Is(err, sitemap.ErrMaxURLs) {
			fmt.Println(err)
		}
	}
	// Output:
	// 2 URLs
	// parse "https://example.com/sitemap.xml": limit of 2 URLs reached
}

// A client of the caller's own is used for every request of the instance. Here it is the
// client of a test server that speaks HTTPS, which trusts the certificate of that server.
func ExampleS_SetHTTPClient() {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<urlset><url><loc>https://%s/page</loc></url></urlset>`, r.Host)
	}))
	defer server.Close()

	s := sitemap.New().SetHTTPClient(server.Client())
	if _, err := s.Parse(server.URL+"/sitemap.xml", nil); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println(strings.TrimPrefix(u.Loc, server.URL))
	}
	// Output:
	// /page
}

// The errors of a call are of four types, which errors.As tells apart. An entry or a value
// that is not valid does not fail the call: it is left out, and reported here.
func ExampleS_GetErrors() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><loc>ftp://example.com/files</loc></url>
  <url><loc>https://example.com/about</loc><lastmod>yesterday</lastmod></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	fmt.Println(s.GetURLCount(), "URLs,", s.GetErrorsCount(), "errors")
	for _, err := range s.GetErrors() {
		var validationErr *sitemap.ValidationError
		if errors.As(err, &validationErr) {
			fmt.Printf("%s: %v\n", validationErr.URL, validationErr.Err)
		}
	}
	// Output:
	// 2 URLs, 2 errors
	// ftp://example.com/files: unsupported scheme "ftp"
	// https://example.com/about: invalid <lastmod> value "yesterday"
}

// The iterator yields the errors GetErrors returns.
func ExampleS_Errors() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>ftp://example.com/files</loc></url>
  <url><loc>mailto:someone@example.com</loc></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for err := range s.Errors() {
		fmt.Println(err)
	}
	// Output:
	// validate "ftp://example.com/files": unsupported scheme "ftp"
	// validate "mailto:someone@example.com": unsupported scheme "mailto"
}

// The sitemaps a sitemap index lists can be read without any of them being fetched, with a
// follow pattern that no URL matches.
func ExampleS_GetSitemaps() {
	content := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-pages.xml</loc><lastmod>2024-03-01</lastmod></sitemap>
  <sitemap><loc>https://example.com/sitemap-blog.xml</loc></sitemap>
</sitemapindex>`

	s := sitemap.New().SetFollow([]string{`^$`})
	if _, err := s.Parse("https://example.com/sitemap-index.xml", &content); err != nil {
		log.Fatal(err)
	}

	fmt.Println(s.GetSitemapCount(), "sitemaps")
	for _, listed := range s.GetSitemaps() {
		if listed.LastMod == nil {
			fmt.Println(listed.Loc, "(no date)")
			continue
		}
		fmt.Println(listed.Loc, listed.LastMod.Format(time.DateOnly))
	}
	// Output:
	// 2 sitemaps
	// https://example.com/sitemap-pages.xml 2024-03-01
	// https://example.com/sitemap-blog.xml (no date)
}

// The iterator yields the URLs as copies, so they can be collected into a list that is the
// caller's own to sort, which the slice GetURLs returns is not.
func ExampleS_URLs() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/news</loc><priority>0.8</priority></url>
  <url><loc>https://example.com/about</loc><priority>0.3</priority></url>
  <url><loc>https://example.com/</loc><priority>1.0</priority></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for u := range s.URLs() {
		fmt.Println(u.Loc)
	}

	sorted := slices.SortedFunc(s.URLs(), func(a, b sitemap.URL) int {
		return strings.Compare(a.Loc, b.Loc)
	})
	fmt.Println("sorted:")
	for _, u := range sorted {
		fmt.Println(u.Loc)
	}
	// Output:
	// https://example.com/news
	// https://example.com/about
	// https://example.com/
	// sorted:
	// https://example.com/
	// https://example.com/about
	// https://example.com/news
}

// No URL is returned twice, and no more of them than there are.
func ExampleS_GetRandomURLs() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/1</loc></url>
  <url><loc>https://example.com/2</loc></url>
  <url><loc>https://example.com/3</loc></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	fmt.Println(len(s.GetRandomURLs(2)))
	for _, u := range s.GetRandomURLs(5) {
		fmt.Println(u.Loc)
	}
	// Unordered output:
	// 2
	// https://example.com/1
	// https://example.com/2
	// https://example.com/3
}

// The fields of a URL other than its location are pointers, which are nil for what the entry
// does not give.
func ExampleURL() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
    <lastmod>2024-03-01</lastmod>
    <changefreq>daily</changefreq>
    <priority>0.8</priority>
  </url>
  <url>
    <loc>https://example.com/about</loc>
  </url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		fmt.Println(u.Loc)
		if u.LastMod != nil {
			fmt.Println("  last modified:", u.LastMod.Format(time.DateOnly))
		}
		if u.ChangeFreq != nil {
			fmt.Println("  changes:", *u.ChangeFreq)
		}
		if u.Priority != nil {
			fmt.Println("  priority:", *u.Priority)
		}
	}
	// Output:
	// https://example.com/
	//   last modified: 2024-03-01
	//   changes: daily
	//   priority: 0.8
	// https://example.com/about
}

// A date is given as a year, a month, a day, or a day and a time with its time zone.
// LastModTime embeds time.Time, whose methods it therefore has.
func ExampleLastModTime() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/year</loc><lastmod>2024</lastmod></url>
  <url><loc>https://example.com/day</loc><lastmod>2024-03-01</lastmod></url>
  <url><loc>https://example.com/time</loc><lastmod>2024-03-01T10:30:00+01:00</lastmod></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	since := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	for _, u := range s.GetURLs() {
		fmt.Println(u.LastMod.Format(time.RFC3339), u.LastMod.After(since))
	}
	// Output:
	// 2024-01-01T00:00:00Z false
	// 2024-03-01T00:00:00Z true
	// 2024-03-01T10:30:00+01:00 true
}

// A change frequency is compared with the constants of its type. Tolerant mode reads a value
// of the protocol in whichever letter case it is written.
func ExampleURLChangeFreq() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/news</loc><changefreq>hourly</changefreq></url>
  <url><loc>https://example.com/blog</loc><changefreq>Daily</changefreq></url>
  <url><loc>https://example.com/about</loc><changefreq>yearly</changefreq></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		switch *u.ChangeFreq {
		case sitemap.ChangeFreqAlways, sitemap.ChangeFreqHourly, sitemap.ChangeFreqDaily:
			fmt.Println(u.Loc, "changes often")
		default:
			fmt.Println(u.Loc, "changes seldom")
		}
	}
	// Output:
	// https://example.com/news changes often
	// https://example.com/blog changes often
	// https://example.com/about changes seldom
}

// The images of a page are those of its entry that have a location.
func ExampleImage() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url>
    <loc>https://example.com/gallery</loc>
    <image:image>
      <image:loc>https://cdn.example.net/harbour.jpg</image:loc>
      <image:title>The harbour at dawn</image:title>
    </image:image>
    <image:image>
      <image:loc>https://cdn.example.net/lighthouse.jpg</image:loc>
    </image:image>
  </url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		for _, image := range u.Images {
			fmt.Printf("%s %q\n", image.Loc, image.Title)
		}
	}
	// Output:
	// https://cdn.example.net/harbour.jpg "The harbour at dawn"
	// https://cdn.example.net/lighthouse.jpg ""
}

// The news of a page is nil unless its entry has a <news:news> element.
func ExampleNews() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
  <url>
    <loc>https://example.com/news/harbour-reopens</loc>
    <news:news>
      <news:publication>
        <news:name>The Example Times</news:name>
        <news:language>en</news:language>
      </news:publication>
      <news:publication_date>2024-03-01</news:publication_date>
      <news:title>The harbour reopens</news:title>
    </news:news>
  </url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		if u.News == nil {
			continue
		}
		fmt.Println(u.News.Title)
		fmt.Println(u.News.Publication.Name, u.News.Publication.Language)
		fmt.Println(u.News.PublicationDate.Format(time.DateOnly))
	}
	// Output:
	// The harbour reopens
	// The Example Times en
	// 2024-03-01
}

// The numbers and the dates of a video are pointers, which are nil for what the video does
// not give.
func ExampleVideo() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:video="http://www.google.com/schemas/sitemap-video/1.1">
  <url>
    <loc>https://example.com/videos/harbour</loc>
    <video:video>
      <video:thumbnail_loc>https://cdn.example.net/harbour.jpg</video:thumbnail_loc>
      <video:title>A walk around the harbour</video:title>
      <video:description>The harbour at dawn, from the pier to the lighthouse.</video:description>
      <video:content_loc>https://cdn.example.net/harbour.mp4</video:content_loc>
      <video:duration>600</video:duration>
      <video:restriction relationship="allow">IE GB</video:restriction>
      <video:tag>harbour</video:tag>
      <video:tag>walk</video:tag>
    </video:video>
  </url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		for _, video := range u.Videos {
			fmt.Println(video.Title)
			fmt.Println(video.ContentLoc)
			if video.Duration != nil {
				fmt.Println(time.Duration(*video.Duration) * time.Second)
			}
			if video.Restriction != nil {
				fmt.Println(video.Restriction.Relationship, video.Restriction.Value)
			}
			if video.Rating == nil {
				fmt.Println("no rating")
			}
			fmt.Println(video.Tags)
		}
	}
	// Output:
	// A walk around the harbour
	// https://cdn.example.net/harbour.mp4
	// 10m0s
	// allow IE GB
	// no rating
	// [harbour walk]
}

// The alternate links of a page name its versions in other languages.
func ExampleAlternateLink() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>https://example.com/en/about</loc>
    <xhtml:link rel="alternate" hreflang="de" href="https://example.com/de/ueber-uns"/>
    <xhtml:link rel="alternate" hreflang="fr" href="https://example.com/fr/a-propos"/>
  </url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, u := range s.GetURLs() {
		for _, link := range u.Hreflangs {
			fmt.Println(link.Hreflang, link.Href)
		}
	}
	// Output:
	// de https://example.com/de/ueber-uns
	// fr https://example.com/fr/a-propos
}

// A sitemap that a sitemap index lists comes with the date the index gives for it, which
// tells the sitemaps that changed since an earlier visit from the rest.
func ExampleSitemap() {
	content := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-2023.xml</loc><lastmod>2023-12-31</lastmod></sitemap>
  <sitemap><loc>https://example.com/sitemap-2024.xml</loc><lastmod>2024-03-01</lastmod></sitemap>
  <sitemap><loc>https://example.com/sitemap-pages.xml</loc></sitemap>
</sitemapindex>`

	// No sitemap is fetched: no URL matches the follow pattern.
	s := sitemap.New().SetFollow([]string{`^$`})
	if _, err := s.Parse("https://example.com/sitemap-index.xml", &content); err != nil {
		log.Fatal(err)
	}

	lastVisit := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	for _, listed := range s.GetSitemaps() {
		// A sitemap without a date may have changed as well.
		if listed.LastMod == nil || listed.LastMod.After(lastVisit) {
			fmt.Println(listed.Loc)
		}
	}
	// Output:
	// https://example.com/sitemap-2024.xml
	// https://example.com/sitemap-pages.xml
}

// A Set method that is given a value it does not accept records an error. Nothing is parsed
// while such an error is outstanding; setting a valid value clears it.
func ExampleConfigError() {
	s := sitemap.New().SetMaxDepth(0)

	for _, err := range s.GetErrors() {
		var configErr *sitemap.ConfigError
		if errors.As(err, &configErr) {
			fmt.Println(configErr.Field, "-", configErr.Err)
		}
	}

	content := `<urlset><url><loc>https://example.com/</loc></url></urlset>`
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		fmt.Println(err)
		// The error of the call wraps the errors that are outstanding.
		var configErr *sitemap.ConfigError
		if errors.As(err, &configErr) {
			fmt.Println("because of", configErr.Field)
		}
	}

	s.SetMaxDepth(5)
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}
	fmt.Println(s.GetURLCount(), "URL")
	// Output:
	// maxDepth - must be greater than 0, got 0
	// errors occurred before parsing, see GetErrors() for details
	// because of maxDepth
	// 1 URL
}

// A document that cannot be fetched is reported by a NetworkError. It fails the call when
// it is the document the call was made for.
func ExampleNetworkError() {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	s := sitemap.New()
	_, err := s.Parse(server.URL+"/sitemap.xml", nil)

	var networkErr *sitemap.NetworkError
	if errors.As(err, &networkErr) {
		fmt.Println(strings.TrimPrefix(networkErr.URL, server.URL))
		fmt.Println(networkErr.Err)
	}
	// Output:
	// /sitemap.xml
	// received HTTP status 404
}

// A document that cannot be parsed is reported by a ParseError.
func ExampleParseError() {
	content := `<html><body>Not found</body></html>`

	s := sitemap.New()
	_, err := s.Parse("https://example.com/sitemap.xml", &content)

	var parseErr *sitemap.ParseError
	if errors.As(err, &parseErr) {
		fmt.Println(parseErr.URL)
		fmt.Println(parseErr.Err)
	}
	// Output:
	// https://example.com/sitemap.xml
	// unrecognized sitemap format (root element: "html")
}

// An entry that is not valid is reported by a ValidationError, which names the URL that was
// rejected, or the page a rejected value belongs to.
func ExampleValidationError() {
	content := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>ftp://example.com/files</loc></url>
  <url><loc>https://example.com/about</loc><priority>high</priority></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatal(err)
	}

	for _, err := range s.GetErrors() {
		var validationErr *sitemap.ValidationError
		if errors.As(err, &validationErr) {
			fmt.Println(validationErr.URL, "-", validationErr.Err)
		}
	}
	// Output:
	// ftp://example.com/files - unsupported scheme "ftp"
	// https://example.com/about - invalid <priority> value "high"
}

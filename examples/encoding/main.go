package main

import (
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing a sitemap that is not UTF-8 encoded.
//
// The sitemaps.org protocol requires UTF-8, but sitemaps in other encodings
// exist. The parser honours the encoding named in the XML declaration and
// transcodes the document while reading it, so every string it returns is
// UTF-8. This applies to sitemap indexes, urlsets, RSS and Atom feeds alike.
//
// A document may begin with a UTF-8 byte order mark (BOM). The mark tells the
// encoding and is no part of the content, so it is ignored, whatever the
// format of the document. The example shows it with a text sitemap, where the
// mark stands right before the first URL.
//
// A document that declares an encoding the parser cannot transcode is not
// parsed; a *ParseError naming the encoding is reported via GetErrors(), and
// returned by Parse() when it is the document Parse() was called for.
//
// The sitemap content is passed in directly, so the example runs without
// network access.
func main() {
	// A sitemap in ISO-8859-2 (Latin-2). The escaped bytes are the accented
	// letters of "tükörfúrógép" and "Árvíztűrő tükörfúrógép" in that encoding;
	// read as UTF-8 they would be invalid.
	latin2Content := "<?xml version=\"1.0\" encoding=\"ISO-8859-2\"?>\n" +
		"<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\"\n" +
		"        xmlns:image=\"http://www.google.com/schemas/sitemap-image/1.1\">\n" +
		"  <url>\n" +
		"    <loc>https://example.com/t\xfck\xf6rf\xfar\xf3g\xe9p</loc>\n" +
		"    <image:image>\n" +
		"      <image:loc>https://example.com/photo.jpg</image:loc>\n" +
		"      <image:title>\xc1rv\xedzt\xfbr\xf5 t\xfck\xf6rf\xfar\xf3g\xe9p</image:title>\n" +
		"    </image:image>\n" +
		"  </url>\n" +
		"</urlset>"

	fmt.Println("=== ISO-8859-2 ===")
	s := sitemap.New()
	sm, err := s.Parse("https://example.com/sitemap.xml", &latin2Content)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		// Non-ASCII characters of a location are percent-encoded as UTF-8.
		fmt.Printf("Page: %s\n", u.Loc)
		for _, img := range u.Images {
			fmt.Printf("  Image: %s\n", img.Loc)
			fmt.Printf("    Title: %s\n", img.Title)
		}
	}

	// A text sitemap that begins with a UTF-8 byte order mark ("\ufeff", the bytes
	// EF BB BF), as some editors save it. The first line is read as the URL it
	// holds, without the mark.
	bomContent := "\ufeff" +
		"https://example.com/first\n" +
		"https://example.com/second\n"

	fmt.Println("\n=== UTF-8 with a byte order mark ===")
	s = sitemap.New()
	sm, err = s.Parse("https://example.com/sitemap.txt", &bomContent)
	if err != nil {
		log.Fatalf("parse error: %v", err)
	}

	for _, u := range sm.GetURLs() {
		fmt.Printf("Page: %s\n", u.Loc)
	}

	// IBM437 is not among the supported encodings. The document is recognised
	// as a urlset, but it cannot be read.
	unsupportedContent := `<?xml version="1.0" encoding="IBM437"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/page</loc></url>
</urlset>`

	fmt.Println("\n=== Unsupported encoding ===")
	s = sitemap.New()
	sm, err = s.Parse("https://example.com/sitemap.xml", &unsupportedContent)
	if err != nil {
		// The document could not be parsed, so the call fails. The error is the
		// one in GetErrors().
		fmt.Printf("Parse failed: %v\n", err)
	}

	fmt.Printf("%d URLs, %d errors\n", sm.GetURLCount(), sm.GetErrorsCount())
	for _, e := range sm.GetErrors() {
		fmt.Printf("  - %v\n", e)
	}
}

package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates parsing gzip compressed sitemaps.
//
// A document that begins like gzip content is decompressed before it is
// parsed, whatever its format and whatever its URL ends with.
//
// A gzip file is a series of members, each of them compressed on its own. A
// file that was appended to holds several, and so does one that was put
// together with "cat a.gz b.gz". The content of the file is that of all its
// members, and that is what is parsed. What a server sends after the last
// member and is no member, such as a newline, is ignored.
//
// Content that cannot be decompressed to its end is not parsed. It is
// reported once, as a *ParseError, and yields no URLs, not even those of the
// members that could be read: what is missing may be the rest of the very
// document they begin.
//
// The sitemaps are passed in directly, so the example runs without network
// access. To have one fetched, pass nil in place of the content.
func main() {
	// A text sitemap that was written in one go: one member.
	whole := compress("https://example.com/\nhttps://example.com/about\n")
	report("One member", whole)

	// The same sitemap after a page was appended to it: two members.
	appended := whole + compress("https://example.com/contact\n")
	report("Two members", appended)

	// A newline after the last member, as some servers send it.
	report("Two members and a newline", appended+"\n")

	// A download that broke off in the second member.
	report("Second member cut short", appended[:len(appended)-4])
}

// report parses content as the sitemap of example.com and prints what the call
// yielded.
func report(title string, content string) {
	fmt.Printf("%s:\n", title)

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.txt.gz", &content); err != nil {
		fmt.Printf(" - Parse failed: %v\n", err)
	}
	for _, u := range s.GetURLs() {
		fmt.Printf(" - %s\n", u.Loc)
	}
	fmt.Printf(" - %d URLs, %d errors\n\n", s.GetURLCount(), s.GetErrorsCount())
}

// compress returns content as a gzip member.
func compress(content string) string {
	var member bytes.Buffer
	writer := gzip.NewWriter(&member)
	if _, err := writer.Write([]byte(content)); err != nil {
		log.Fatalf("compress: %v", err)
	}
	if err := writer.Close(); err != nil {
		log.Fatalf("compress: %v", err)
	}
	return member.String()
}

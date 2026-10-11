package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates URLs and Errors, the iterators over the results of a call.
//
// URLs yields the URLs GetURLs returns, in the same order, each as a copy
// that is the caller's own: whatever is done to it, the URLs the instance
// keeps are left as they are. The list is not copied for it, so a loop takes
// up the memory of one URL at a time. Errors yields the errors GetErrors
// returns.
//
// GetURLs and GetErrors return the lists the instance keeps, which costs the
// same however long they are. Those lists are to be read, not to be changed.
// A list to sort or to change is what slices.Collect, slices.SortedFunc and
// slices.AppendSeq make of an iterator.
//
// The sitemap is handed to the call, so the example runs without network
// access.
func main() {
	// The fifth entry is not a page to fetch, and the priority of the last one
	// is not a number: an error is recorded for each.
	content := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><lastmod>2024-03-01</lastmod><changefreq>weekly</changefreq><priority>1.0</priority></url>
  <url><loc>https://example.com/news</loc><lastmod>2024-03-20</lastmod><changefreq>daily</changefreq><priority>0.8</priority></url>
  <url><loc>https://example.com/about</loc><lastmod>2023-11-05</lastmod><changefreq>yearly</changefreq><priority>0.3</priority></url>
  <url><loc>https://example.com/blog</loc><lastmod>2024-03-12</lastmod><changefreq>daily</changefreq><priority>0.6</priority></url>
  <url><loc>ftp://example.com/files</loc></url>
  <url><loc>https://example.com/contact</loc><priority>high</priority></url>
</urlset>`

	s := sitemap.New()
	if _, err := s.Parse("https://example.com/sitemap.xml", &content); err != nil {
		log.Fatalf("parse error: %v", err)
	}

	fmt.Println("=== the URLs of the call ===")
	for u := range s.URLs() {
		fmt.Printf("  %s\n", u.Loc)
	}

	// A loop that ends early costs nothing for the URLs it does not get to.
	fmt.Println("\n=== the first page that changes daily ===")
	for u := range s.URLs() {
		if u.ChangeFreq != nil && *u.ChangeFreq == sitemap.ChangeFreqDaily {
			fmt.Printf("  %s\n", u.Loc)
			break
		}
	}

	// The iterator works with what the standard library has for iterators.
	// slices.SortedFunc collects the URLs and sorts them. The list is the
	// caller's own, so the order of the URLs of the instance is what it was.
	fmt.Println("\n=== the pages, the one that was modified last first ===")
	byDate := slices.SortedFunc(s.URLs(), func(a, b sitemap.URL) int {
		return lastModified(b).Compare(lastModified(a))
	})
	for _, u := range byDate {
		date := "no date"
		if u.LastMod != nil {
			date = u.LastMod.Format(time.DateOnly)
		}
		fmt.Printf("  %-27s %s\n", u.Loc, date)
	}
	fmt.Printf("first URL of the list: %s, first URL of the instance: %s\n", byDate[0].Loc, s.GetURLs()[0].Loc)

	// A URL the iterator yields is a copy down to what it points to, so a
	// value that is set through a pointer of it is set for the copy alone.
	fmt.Println("\n=== every priority is set to 0.5, in copies of the URLs ===")
	urls := slices.Collect(s.URLs())
	for _, u := range urls {
		if u.Priority != nil {
			*u.Priority = 0.5
		}
	}
	for i, u := range s.GetURLs() {
		if u.Priority != nil {
			fmt.Printf("  %-27s the copy: %.1f, the instance: %.1f\n", u.Loc, *urls[i].Priority, *u.Priority)
		}
	}

	// A list that is long is better given its capacity at once: slices.Collect
	// cannot tell how long the list is going to be, and grows it as it goes.
	sized := slices.AppendSeq(make([]sitemap.URL, 0, s.GetURLCount()), s.URLs())
	fmt.Printf("\n%d URLs in a list with a capacity of %d\n", len(sized), cap(sized))

	// GetURLs returns the list the instance keeps, without room to spare. What
	// is appended to it is therefore appended to a copy, and neither the
	// instance nor another caller sees it.
	mine := append(s.GetURLs(), sitemap.URL{Loc: "https://example.com/mine"})
	fmt.Printf("%d URLs after one was appended, %d URLs of the instance\n", len(mine), s.GetURLCount())

	// Errors yields the errors themselves, to be told apart as the ones
	// GetErrors returns are.
	fmt.Println("\n=== the errors of the call ===")
	for err := range s.Errors() {
		var validationErr *sitemap.ValidationError
		if errors.As(err, &validationErr) {
			fmt.Printf("  %s: %v\n", validationErr.URL, validationErr.Err)
			continue
		}
		fmt.Printf("  %v\n", err)
	}
}

// lastModified returns the date u was last modified, and the zero time if the
// sitemap gives none, which puts the page after the others.
func lastModified(u sitemap.URL) time.Time {
	if u.LastMod == nil {
		return time.Time{}
	}
	return u.LastMod.Time
}

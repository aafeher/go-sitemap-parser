package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aafeher/go-sitemap-parser"
)

// main demonstrates the use of ParseContext to propagate cancellation and
// deadlines to every HTTP request issued by the parser.
//
// A context with a short timeout is used so that, regardless of the size of
// the sitemap tree, the whole parse operation will be aborted if it does not
// complete in time. Already-parsed URLs accumulated before cancellation
// remain available via GetURLs(); the cancellation cause is also reported
// through the returned error and via GetErrors().
func main() {
	url := "https://www.sitemaps.org/sitemap.xml"

	// Bound the entire parse operation by a deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := sitemap.New()
	sm, err := s.ParseContext(ctx, url, nil)
	if err != nil {
		// errors.Is lets us distinguish a deadline/cancellation from other
		// failure modes (HTTP errors, malformed XML, ...).
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			log.Printf("parse aborted: deadline exceeded after %s", 5*time.Second)
		case errors.Is(err, context.Canceled):
			log.Printf("parse aborted: context cancelled")
		default:
			log.Printf("parse failed: %v", err)
		}
	}

	fmt.Printf("Sitemaps of %s contains %d URLs (partial results are still usable).\n",
		url, sm.GetURLCount())
}

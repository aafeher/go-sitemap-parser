package sitemap

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"golang.org/x/net/idna"
)

type (

	// S is a sitemap parser. An instance holds the configuration of the parser, which its Set
	// methods change and its Get methods of the same names return, and the results of its most
	// recent Parse or ParseContext call: the URLs the call collected, the sitemaps it found on
	// its way and the errors it recorded, see GetURLs, GetSitemaps and GetErrors.
	//
	// An instance is made with New, which gives the configuration its defaults. The zero value
	// has none of them and is not meant to be used.
	//
	// The methods of an instance may be called from several goroutines. An instance carries
	// out one call at a time, and can be used for any number of calls one after the other.
	S struct {
		parseMu              sync.Mutex
		mu                   sync.Mutex
		cfg                  config
		mainURL              string
		robotsTxtSitemapURLs []string
		// fetchedURLs tracks sitemap URLs already fetched in the current Parse
		// call to prevent duplicate HTTP requests when the same URL is referenced
		// from multiple sitemap indexes or robots.txt directives.
		fetchedURLs map[string]struct{}
		// sitemaps are the sitemaps the documents of the current Parse call list, each of
		// them once and in the order they were found, and listedURLs tells where in them the
		// sitemap of a URL is. There are no more of them than the limit set with
		// SetMaxSitemaps allows: how many sitemaps the documents list is up to the server.
		sitemaps   []Sitemap
		listedURLs map[string]int
		urls       []URL
		errs       []error
		// sem is a per-Parse-call semaphore that bounds the number of
		// sitemaps being fetched or parsed at the same time when
		// cfg.maxConcurrency > 0.
		// It is created at the start of ParseContext and is nil when
		// concurrency is unlimited.
		sem chan struct{}
		// limits holds the limits of the current Parse call on the sitemaps it
		// fetches and lists, on the URLs it collects and on the validation errors it
		// records, and whether they were reached.
		limits callLimits
	}

	// callLimits are the limits of a Parse call on what it fetches, collects and records.
	// They are the values set with SetMaxSitemaps, SetMaxURLs and SetMaxValidationErrors as
	// they stand when the call starts, so that one limit applies to the whole call; 0 means
	// no limit.
	// sitemapsLeftOut, urlsLeftOut and validationErrorsLeftOut tell that a limit was not only
	// reached, but kept the call from fetching or listing a sitemap, from collecting a URL or
	// from recording a validation error: the results are not complete.
	// validationErrors is the number of validation errors that count towards their limit, see
	// addError.
	callLimits struct {
		maxSitemaps             int
		maxURLs                 int
		maxValidationErrors     int
		validationErrors        int
		sitemapsLeftOut         bool
		urlsLeftOut             bool
		validationErrorsLeftOut bool
	}

	// config is a structure that holds configuration settings.
	// It contains a userAgent field of type string, which represents the User-Agent header value for HTTP requests.
	// The fetchTimeout field of type uint16 represents the timeout value (in seconds) for fetching data.
	// The multiThread field of type bool determines whether to use multi-threading for fetching URLs.
	// The follow field is a slice of strings that contains regular expressions to match URLs to follow.
	// The followRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the follow field.
	// The rules field is a slice of strings that contains regular expressions to match URLs to include.
	// The rulesRegexes field is a slice of *regexp.Regexp that stores the compiled regular expressions for the rules field.
	config struct {
		userAgent           string
		fetchTimeout        uint16
		maxResponseSize     int64
		maxDepth            int
		maxConcurrency      int
		maxSitemaps         int
		maxURLs             int
		maxValidationErrors int
		multiThread         bool
		strict              bool
		httpClient          *http.Client
		follow              []string
		followRegexes       []*regexp.Regexp
		rules               []string
		rulesRegexes        []*regexp.Regexp
	}

	// sitemapIndex is a structure of <sitemapindex>. The <lastmod> of an entry is read as
	// text, so that one that cannot be parsed costs only itself, see urlEntry.
	sitemapIndex struct {
		XMLName xml.Name `xml:"sitemapindex"`
		Sitemap []struct {
			Loc     string  `xml:"loc"`
			LastMod *string `xml:"lastmod"`
		} `xml:"sitemap"`
	}

	// urlEntry is the form in which a <url> element is decoded: a URL whose elements holding a
	// number or a date are read as text first. Decoded straight into its type, the first such
	// value that cannot be parsed would fail the whole document; read as text, it costs only
	// itself. The fields below take these elements over from the fields of the embedded
	// structure that name the same element, and convert fills those in from the text.
	urlEntry struct {
		URL
		LastModText  *string      `xml:"lastmod"`
		PriorityText *string      `xml:"priority"`
		NewsEntry    *newsEntry   `xml:"http://www.google.com/schemas/sitemap-news/0.9 news"`
		VideoEntries []videoEntry `xml:"http://www.google.com/schemas/sitemap-video/1.1 video"`
		// invalid lists the elements of the entry whose content could not be parsed.
		invalid []invalidValue
		// invalidOwn reports whether an element of the <url> itself, rather than of one of its
		// extensions, is among them.
		invalidOwn bool
	}

	// newsEntry is the form in which a <news:news> element is decoded, see urlEntry.
	newsEntry struct {
		News
		PublicationDateText *string `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication_date"`
	}

	// videoEntry is the form in which a <video:video> element is decoded, see urlEntry.
	videoEntry struct {
		Video
		DurationText        *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 duration"`
		ExpirationDateText  *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 expiration_date"`
		RatingText          *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 rating"`
		ViewCountText       *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 view_count"`
		PublicationDateText *string `xml:"http://www.google.com/schemas/sitemap-video/1.1 publication_date"`
	}

	// invalidValue is an element whose content could not be parsed.
	invalidValue struct {
		// element names the element the way error messages do, e.g. "<lastmod>".
		element string
		text    string
	}

	// dateElement is an optional element that holds a date, as it was read from a document.
	// Its zero value stands for a date that the document has no element for.
	dateElement struct {
		// name names the element the way error messages do, e.g. "<pubDate>".
		name string
		// text is the content of the element. It is nil if the element is absent.
		text *string
		// parse parses the content, in the format the element has to be in.
		parse func(text string) (LastModTime, error)
	}

	// rss is a structure of <rss> for RSS 2.0 feeds.
	rss struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Item []rssItem `xml:"item"`
		} `xml:"channel"`
	}

	// rssItem is an <item> of an RSS feed. It is read by its UnmarshalXML method, which takes
	// the elements of the feed and leaves those of other formats, see decodeOwnChildren.
	rssItem struct {
		// Link is the content of <link>.
		Link string
		// PubDate is the content of <pubDate>, nil if the item has none. It is read as
		// text, so that one that cannot be parsed costs only itself, see urlEntry.
		PubDate *string
	}

	// atom is a structure of <feed> for Atom 1.0 feeds.
	atom struct {
		XMLName xml.Name    `xml:"feed"`
		Entry   []atomEntry `xml:"entry"`
	}

	// atomEntry is an <entry> of an Atom feed. It is read by its UnmarshalXML method, like an
	// RSS item.
	atomEntry struct {
		// Link holds the <link> elements of the entry, in the order they stand in it.
		Link []atomLink
		// Updated is the content of <updated>, nil if the entry has none. It is read as
		// text, like the <pubDate> of an RSS item.
		Updated *string
	}

	// atomLink is a <link> of an Atom entry.
	atomLink struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	}

	// Image is a structure of <image:image> in <url>, per the Google Image Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/image-sitemaps
	//
	// An image is known by its Loc, and one without a Loc is not among the images of a URL.
	// Every other field is empty when the image has no element for it.
	Image struct {
		// Loc is the URL of the image, the content of <image:loc>. It is never empty, and it
		// may be on another host than the page. Strict mode requires an absolute HTTP or
		// HTTPS URL: an image whose Loc is not one is left out and reported.
		Loc string `xml:"http://www.google.com/schemas/sitemap-image/1.1 loc"`
		// Title is the title of the image, the content of <image:title>.
		Title string `xml:"http://www.google.com/schemas/sitemap-image/1.1 title"`
		// Caption is the caption of the image, the content of <image:caption>.
		Caption string `xml:"http://www.google.com/schemas/sitemap-image/1.1 caption"`
		// GeoLocation is the place the image shows, the content of <image:geo_location>,
		// such as "Limerick, Ireland".
		GeoLocation string `xml:"http://www.google.com/schemas/sitemap-image/1.1 geo_location"`
		// License is the URL of the licence of the image, the content of <image:license>.
		// Strict mode reports one that is no absolute HTTP or HTTPS URL; the image is
		// returned all the same, with the value as the document gives it.
		License string `xml:"http://www.google.com/schemas/sitemap-image/1.1 license"`
	}

	// VideoRestriction is a structure of <video:restriction> in <video:video>.
	// It captures the element text and the required "relationship" attribute.
	VideoRestriction struct {
		// Relationship tells whether the video may be shown in the countries Value names or
		// may not be shown there, the value of the relationship attribute: "allow" or
		// "deny" by the extension. Neither mode checks the value.
		Relationship string `xml:"relationship,attr"`
		// Value names the countries, as the content of the element gives them: country
		// codes with a space between them by the extension, such as "IE GB US".
		Value string `xml:",chardata"`
	}

	// VideoPlatform is a structure of <video:platform> in <video:video>.
	// It captures the element text and the required "relationship" attribute.
	VideoPlatform struct {
		// Relationship tells whether the video may be shown on the platforms Value names or
		// may not be shown there, the value of the relationship attribute: "allow" or
		// "deny" by the extension. Neither mode checks the value.
		Relationship string `xml:"relationship,attr"`
		// Value names the platforms, as the content of the element gives them: "web",
		// "mobile" and "tv" with a space between them by the extension, such as "web tv".
		Value string `xml:",chardata"`
	}

	// VideoUploader is a structure of <video:uploader> in <video:video>.
	// It captures the uploader name and the optional "info" URL attribute.
	VideoUploader struct {
		// Info is the URL of a page that tells more about the uploader, the value of the
		// info attribute. It is empty if the element has no such attribute. Strict mode
		// reports one that is no absolute HTTP or HTTPS URL; the video is returned all the
		// same, with the value as the document gives it.
		Info string `xml:"info,attr"`
		// Value is the name of the uploader, the content of the element.
		Value string `xml:",chardata"`
	}

	// Video is a structure of <video:video> in <url>, per the Google Video Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/video-sitemaps
	//
	// A video is known by its ThumbnailLoc, and one without a ThumbnailLoc is not among the
	// videos of a URL. A text field is empty, and a pointer is nil, when the video has no
	// element for it. A number or a date that cannot be parsed is nil as well, and is reported
	// in both modes; the video is returned without it.
	//
	// Strict mode reports what the extension requires and a video lacks or has out of range,
	// as the fields below tell. A video whose ThumbnailLoc is accepted is returned whatever
	// else is reported about it.
	Video struct {
		// ThumbnailLoc is the URL of the thumbnail image of the video, the content of
		// <video:thumbnail_loc>. It is never empty, and it may be on another host than the
		// page. Strict mode requires an absolute HTTP or HTTPS URL: a video whose
		// ThumbnailLoc is not one is left out and reported.
		ThumbnailLoc string `xml:"http://www.google.com/schemas/sitemap-video/1.1 thumbnail_loc"`
		// Title is the title of the video, the content of <video:title>. Strict mode
		// reports a video without one.
		Title string `xml:"http://www.google.com/schemas/sitemap-video/1.1 title"`
		// Description is the description of the video, the content of
		// <video:description>. Strict mode reports a video without one.
		Description string `xml:"http://www.google.com/schemas/sitemap-video/1.1 description"`
		// ContentLoc is the URL of the video file itself, the content of
		// <video:content_loc>. Strict mode reports a video that has neither a ContentLoc
		// nor a PlayerLoc, and a ContentLoc that is no absolute HTTP or HTTPS URL.
		ContentLoc string `xml:"http://www.google.com/schemas/sitemap-video/1.1 content_loc"`
		// PlayerLoc is the URL of a player for the video, the content of
		// <video:player_loc>. Strict mode reports one that is no absolute HTTP or HTTPS
		// URL, see ContentLoc.
		PlayerLoc string `xml:"http://www.google.com/schemas/sitemap-video/1.1 player_loc"`
		// Duration is the length of the video in seconds, the content of <video:duration>.
		// Strict mode reports a value that is not between 1 and 28800, which is eight
		// hours.
		Duration *int `xml:"http://www.google.com/schemas/sitemap-video/1.1 duration"`
		// ExpirationDate is the date after which the video is no longer available, the
		// content of <video:expiration_date>. It is nil for an element that is empty as
		// well.
		ExpirationDate *LastModTime `xml:"http://www.google.com/schemas/sitemap-video/1.1 expiration_date"`
		// Rating is the rating of the video, the content of <video:rating>. Strict mode
		// reports a value that is not between 0.0 and 5.0.
		Rating *float32 `xml:"http://www.google.com/schemas/sitemap-video/1.1 rating"`
		// ViewCount is the number of times the video was viewed, the content of
		// <video:view_count>. Strict mode reports a negative value.
		ViewCount *int `xml:"http://www.google.com/schemas/sitemap-video/1.1 view_count"`
		// PublicationDate is the date the video was first published, the content of
		// <video:publication_date>. It is nil for an element that is empty as well.
		PublicationDate *LastModTime `xml:"http://www.google.com/schemas/sitemap-video/1.1 publication_date"`
		// FamilyFriendly tells whether the video is fit to be shown with a filter for
		// adult content on, the content of <video:family_friendly>: "yes" or "no" by the
		// extension. Neither mode checks the value.
		FamilyFriendly string `xml:"http://www.google.com/schemas/sitemap-video/1.1 family_friendly"`
		// Restriction tells the countries the video may or may not be shown in, the
		// <video:restriction> of the video.
		Restriction *VideoRestriction `xml:"http://www.google.com/schemas/sitemap-video/1.1 restriction"`
		// Platform tells the platforms the video may or may not be shown on, the
		// <video:platform> of the video.
		Platform *VideoPlatform `xml:"http://www.google.com/schemas/sitemap-video/1.1 platform"`
		// RequiresSubscription tells whether the video can only be viewed with a
		// subscription, the content of <video:requires_subscription>: "yes" or "no" by the
		// extension. Neither mode checks the value.
		RequiresSubscription string `xml:"http://www.google.com/schemas/sitemap-video/1.1 requires_subscription"`
		// Uploader is who uploaded the video, the <video:uploader> of the video.
		Uploader *VideoUploader `xml:"http://www.google.com/schemas/sitemap-video/1.1 uploader"`
		// Live tells whether the video is a live stream, the content of <video:live>:
		// "yes" or "no" by the extension. Neither mode checks the value.
		Live string `xml:"http://www.google.com/schemas/sitemap-video/1.1 live"`
		// Tags are the tags of the video, one for each <video:tag> and in the order of
		// the document. It is nil if the video has none. Strict mode reports a video with
		// more than 32 of them.
		Tags []string `xml:"http://www.google.com/schemas/sitemap-video/1.1 tag"`
	}

	// NewsPublication is a structure of <news:publication> in <news:news>.
	NewsPublication struct {
		// Name is the name of the publication, the content of <news:name>. It is empty if
		// the element is absent. Strict mode reports news without one.
		Name string `xml:"http://www.google.com/schemas/sitemap-news/0.9 name"`
		// Language is the language of the publication, the content of <news:language>: a
		// language code such as "en". It is empty if the element is absent. Strict mode
		// reports news without one.
		Language string `xml:"http://www.google.com/schemas/sitemap-news/0.9 language"`
	}

	// News is a structure of <news:news> in <url>, per the Google News Sitemap extension.
	// Reference: https://developers.google.com/search/docs/crawling-indexing/sitemaps/news-sitemap
	//
	// Strict mode reports each of the fields the extension requires and the entry lacks: the
	// name and the language of the publication, the publication date and the title. The news
	// is returned all the same, with what the entry does give.
	News struct {
		// Publication is the publication the article appears in, the <news:publication> of
		// the entry.
		Publication NewsPublication `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication"`
		// PublicationDate is the date the article was published, the content of
		// <news:publication_date>. It is nil if the element is absent, if it is empty, and
		// if what it holds cannot be parsed, which is reported in both modes. Strict mode
		// reports news without one.
		PublicationDate *LastModTime `xml:"http://www.google.com/schemas/sitemap-news/0.9 publication_date"`
		// Title is the title of the article, the content of <news:title>. It is empty if
		// the element is absent. Strict mode reports news without one.
		Title string `xml:"http://www.google.com/schemas/sitemap-news/0.9 title"`
	}

	// AlternateLink represents an alternate version of a page (hreflang)
	// per the XHTML standard used in sitemaps.
	// Reference: https://developers.google.com/search/docs/specialty/international/localized-versions#sitemap
	//
	// A link is known by its Href, and one without an Href is not among the links of a URL.
	AlternateLink struct {
		// Rel is the relation of the link to the page, the value of the rel attribute:
		// "alternate" for another version of the page. Strict mode leaves out and reports
		// a link with any other value, an empty one included.
		Rel string `xml:"rel,attr"`
		// Hreflang names the language of the version, and the region it is for if it is
		// for one, the value of the hreflang attribute: "de", "de-CH", or "x-default" for
		// the version of no language in particular. Strict mode leaves out and reports a
		// link without one.
		Hreflang string `xml:"hreflang,attr"`
		// Href is the URL of the version, the value of the href attribute. It is never
		// empty, and it may be on another host than the page. Strict mode requires an
		// absolute HTTP or HTTPS URL: a link whose Href is not one is left out and
		// reported.
		Href string `xml:"href,attr"`
	}

	// URL is a structure of <url> in <urlset>. It is also what a page of a sitemap in another
	// format is returned as: an item of an RSS feed, an entry of an Atom feed, a line of a
	// text sitemap.
	//
	// LastMod is the date the page was last modified: the <lastmod> of a <url>, the <pubDate>
	// of an RSS item and the <updated> of an Atom entry. A text sitemap gives no date. No
	// other field but Loc is set for a page of a feed or of a text sitemap.
	//
	// In the URLs that Parse and ParseContext collect, no text value has whitespace around it:
	// the whitespace that surrounds the content of an element or the value of an attribute is
	// not taken for a part of it. This goes for the fields of the extensions as well.
	//
	// In tolerant mode Loc is a URL that can be requested as it is: a space or a character
	// that is not in ASCII is percent-encoded in it, and an internationalised host name is in
	// its Punycode form. A URL of an extension comes back the same way if it is an absolute
	// HTTP or HTTPS URL, and as the document gives it otherwise: Image.Loc and Image.License,
	// Video.ThumbnailLoc, Video.ContentLoc, Video.PlayerLoc and VideoUploader.Info, and
	// AlternateLink.Href. Strict mode returns every URL as the document gives it.
	URL struct {
		// Loc is the URL of the page: the content of the <loc> of a <url>, the <link> of an
		// RSS item, the link of an Atom entry, or a line of a text sitemap. It is never
		// empty: an entry without a location is reported, not returned.
		Loc string `xml:"loc"`
		// LastMod is the date the page was last modified. It is nil if the entry gives no
		// date, if the element is empty, and if what it holds cannot be parsed, which is
		// reported. Strict mode skips a <url> whose <lastmod> cannot be parsed.
		LastMod *LastModTime `xml:"lastmod"`
		// ChangeFreq tells how often the page is likely to change, the content of the
		// <changefreq> of a <url>. It is nil if the element is absent or empty. A value
		// that is set is to be compared with the ChangeFreq constants, see URLChangeFreq
		// for what the two modes make of a value that is none of them.
		ChangeFreq *URLChangeFreq `xml:"changefreq"`
		// Priority is the priority of the page among the pages of its site, the content of
		// the <priority> of a <url>: between 0.0 and 1.0 by the protocol. It is nil if the
		// element is absent, and if what it holds is no number, which is reported. Tolerant
		// mode returns a value outside that range as it is. Strict mode skips and reports a
		// <url> whose <priority> is outside it or is no number.
		Priority *float32 `xml:"priority"`
		// Images are the images on the page, one for each <image:image> of the <url> that
		// has a location, in the order of the document. It is empty or nil if there is
		// none.
		Images []Image `xml:"http://www.google.com/schemas/sitemap-image/1.1 image"`
		// News is what the <url> tells about the page as a news article, its <news:news>.
		// It is nil if the <url> has none.
		News *News `xml:"http://www.google.com/schemas/sitemap-news/0.9 news"`
		// Videos are the videos on the page, one for each <video:video> of the <url> that
		// has a thumbnail location, in the order of the document. It is empty or nil if
		// there is none.
		Videos []Video `xml:"http://www.google.com/schemas/sitemap-video/1.1 video"`
		// Hreflangs are the versions of the page in other languages and for other regions,
		// one for each <xhtml:link> of the <url> that has an href, in the order of the
		// document. It is empty or nil if there is none.
		Hreflangs []AlternateLink `xml:"http://www.w3.org/1999/xhtml link"`
	}

	// Sitemap is a sitemap that a document lists: an entry of a sitemap index, or a Sitemap
	// line of a robots.txt. GetSitemaps returns the ones a call found.
	//
	// Loc is the URL the sitemap is located at, the one it is requested by. It is an absolute
	// URL that passed the checks a URL has to pass to be requested. In tolerant mode it can be
	// requested as it is: a relative URL is resolved against the URL of the document that
	// lists it, a space or a character that is not in ASCII is percent-encoded, and an
	// internationalised host name is in its Punycode form. Strict mode returns it as the
	// document gives it. A sitemap that is reached through a redirect is served from another
	// URL than this one.
	//
	// LastMod is the date the sitemap was last modified, as the sitemap index that lists it
	// gives it: the <lastmod> of its entry. It is nil if the entry has none, if the element is
	// empty, and if what it holds cannot be parsed, which is reported in GetErrors. It is nil
	// for a sitemap that only a robots.txt names as well, which gives no date. If the
	// documents of a call give more than one date for a sitemap, it is the latest of them.
	Sitemap struct {
		// Loc is the URL the sitemap is requested by. It is never empty.
		Loc string
		// LastMod is the date the sitemap was last modified according to the sitemap index
		// that lists it. It is nil if no date is given, or none that can be parsed.
		LastMod *LastModTime
	}

	// LastModTime is the date, or the date and time, that an element of a document holds: a
	// <lastmod> of a sitemap or of a sitemap index, a date element of an extension, the
	// <pubDate> of an RSS item or the <updated> of an Atom entry. It embeds time.Time.
	//
	// In the URLs and the sitemaps that Parse and ParseContext collect, a field of this type
	// is nil when the element is absent, when it is empty, and when what it holds cannot be
	// parsed. A field that is set therefore holds the date the document gives.
	LastModTime struct {
		// Time is the date and time the element gives. Where the element gives a day and no
		// time, the time is midnight in UTC; where it gives a month or a year and no more,
		// the date is the first day of that month or of that year.
		time.Time
	}

	// URLChangeFreq represents the frequency at which a URL should be crawled and indexed.
	// Possible values are: "always", "hourly", "daily", "weekly", "monthly", "yearly", and "never".
	//
	// In the URLs that Parse and ParseContext collect, a field of this type is nil when the
	// <changefreq> element is absent or empty. In tolerant mode one of the values above that is
	// written in another letter case, such as "Daily", is replaced by the value itself, so that
	// it equals its constant, and any other value is kept as the document gives it. Strict mode
	// skips and reports an entry whose value is not one of the values above as they are written
	// here, so a field that is set equals one of the constants there.
	URLChangeFreq string
)

const (
	// ChangeFreqAlways represents the "always" value for URLChangeFreq.
	ChangeFreqAlways URLChangeFreq = "always"

	// ChangeFreqHourly represents the "hourly" value for URLChangeFreq.
	ChangeFreqHourly URLChangeFreq = "hourly"

	// ChangeFreqDaily represents the "daily" value for URLChangeFreq.
	ChangeFreqDaily URLChangeFreq = "daily"

	// ChangeFreqWeekly represents the "weekly" value for URLChangeFreq.
	ChangeFreqWeekly URLChangeFreq = "weekly"

	// ChangeFreqMonthly represents the "monthly" value for URLChangeFreq.
	ChangeFreqMonthly URLChangeFreq = "monthly"

	// ChangeFreqYearly represents the "yearly" value for URLChangeFreq.
	ChangeFreqYearly URLChangeFreq = "yearly"

	// ChangeFreqNever represents the "never" value for URLChangeFreq.
	ChangeFreqNever URLChangeFreq = "never"
)

// changeFreqs are the values a <changefreq> may hold according to the sitemaps.org protocol.
var changeFreqs = [...]URLChangeFreq{
	ChangeFreqAlways,
	ChangeFreqHourly,
	ChangeFreqDaily,
	ChangeFreqWeekly,
	ChangeFreqMonthly,
	ChangeFreqYearly,
	ChangeFreqNever,
}

// New creates a new instance of the S structure.
// It initializes the structure with default configuration values
// and returns a pointer to the created instance.
func New() *S {
	s := &S{}

	s.setConfigDefaults()

	return s
}

// setConfigDefaults sets the default configuration values for the S structure.
// It initializes the cfg field with the default values for userAgent and fetchTimeout.
// The default userAgent is "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
// the default fetchTimeout is 3 seconds and multi-thread flag is true.
// The follow and rules fields are empty slices.
// This method does not return any value.
func (s *S) setConfigDefaults() {
	s.cfg = config{
		userAgent:           "go-sitemap-parser (+https://github.com/aafeher/go-sitemap-parser/blob/main/README.md)",
		fetchTimeout:        3,
		maxResponseSize:     defaultMaxResponseSize,
		maxDepth:            10,
		maxConcurrency:      defaultMaxConcurrency,
		maxSitemaps:         defaultMaxSitemaps,
		maxURLs:             defaultMaxURLs,
		maxValidationErrors: defaultMaxValidationErrors,
		multiThread:         true,
		follow:              []string{},
		rules:               []string{},
	}
}

// SetUserAgent sets the user agent for the Sitemap Parser.
// The user agent is used for making HTTP requests when parsing and fetching URLs.
// It should be a string representing the user agent header value.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetUserAgent(userAgent string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.userAgent = userAgent

	return s
}

// SetFetchTimeout sets the fetch timeout for the Sitemap Parser.
// The fetch timeout determines how long the parser will wait for an HTTP request to complete.
// It should be specified in seconds as a uint16 value and must be greater than 0.
// Invalid values are ignored and a *ConfigError is recorded; a later call with a valid
// value clears it.
// Note: when a custom HTTP client is set via SetHTTPClient, this value has no effect.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFetchTimeout(fetchTimeout uint16) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("fetchTimeout")
	if fetchTimeout == 0 {
		s.errs = append(s.errs, &ConfigError{Field: "fetchTimeout", Err: fmt.Errorf("must be greater than 0, got %d", fetchTimeout)})
		return s
	}
	s.cfg.fetchTimeout = fetchTimeout

	return s
}

// SetMultiThread sets the multi-threading for the Sitemap Parser.
// The multi-threading flag determines whether the parser should fetch and parse the sitemaps concurrently using goroutines.
// When it is off, the sitemaps are fetched one at a time and in the order they are listed, those
// a robots.txt names as well as those of a sitemap index, and SetMaxConcurrency has no effect.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMultiThread(multiThread bool) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.multiThread = multiThread

	return s
}

// SetMaxResponseSize sets the maximum allowed HTTP response size in bytes.
// Responses exceeding this limit are rejected with a *NetworkError.
// The same limit caps the decompressed size of gzip-compressed content, of all the members
// of a gzip file together, whether it was fetched or supplied through the urlContent argument
// of Parse; content that expands beyond it is rejected with a *ParseError.
// Both errors wrap ErrMaxResponseSize, which errors.Is matches.
// The default is 50 MB, matching the sitemaps.org protocol limit.
// The value must be greater than 0; invalid values are ignored and a *ConfigError is recorded.
// A later call with a valid value clears it.
// Every value greater than 0 is a limit, math.MaxInt64 included: nothing can exceed that one,
// so it is the way to have no limit in effect.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxResponseSize(maxResponseSize int64) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxResponseSize")
	if maxResponseSize <= 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxResponseSize", Err: fmt.Errorf("must be greater than 0, got %d", maxResponseSize)})
		return s
	}
	s.cfg.maxResponseSize = maxResponseSize

	return s
}

// SetMaxDepth sets the maximum recursion depth for following sitemap indexes.
// A sitemap index may reference other sitemap indexes; this limits how many levels deep
// the parser will follow. The default is 10.
// When the limit is reached, a *ParseError is recorded for every sitemap index whose sitemaps
// are not followed. It names that sitemap index and wraps ErrMaxDepth, which errors.Is matches.
// The value must be greater than 0; invalid values are ignored and a *ConfigError is recorded.
// A later call with a valid value clears it.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxDepth(maxDepth int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxDepth")
	if maxDepth <= 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxDepth", Err: fmt.Errorf("must be greater than 0, got %d", maxDepth)})
		return s
	}
	s.cfg.maxDepth = maxDepth

	return s
}

// SetMaxConcurrency sets the maximum number of sitemaps that are worked on at the
// same time when multi-threaded parsing is enabled. The default is 16. A value of 0 means
// unlimited concurrency. A positive value caps the number of sitemaps that are being
// fetched or parsed at any one time across the recursive sitemap-index traversal:
// a sitemap counts from the moment its request is sent until its content is parsed.
// That bounds the connections in use and the documents held in memory, which is
// recommended for very large sitemap indexes to avoid goroutine and connection blow-up.
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxConcurrency(maxConcurrency int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxConcurrency")
	if maxConcurrency < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxConcurrency", Err: fmt.Errorf("must be >= 0, got %d", maxConcurrency)})
		return s
	}
	s.cfg.maxConcurrency = maxConcurrency

	return s
}

// SetMaxSitemaps sets the maximum number of sitemaps a Parse or ParseContext call fetches.
// The default is 50,000, which is as many sitemaps as a sitemap index may list according to
// the sitemaps.org protocol. A value of 0 means no limit.
//
// The sitemaps a robots.txt or a sitemap index lists count, on every level together: each one
// the call requests, whether or not the request succeeds. A sitemap that is listed more than
// once is requested and counted once. The document the call is made for does not count.
//
// Once the limit is reached, the sitemaps that are left are not fetched, and a *ParseError
// naming the URL the call was made for is recorded, once for the call. It wraps
// ErrMaxSitemaps, which errors.Is matches. The call does not fail: what the sitemaps fetched
// until then yielded is returned. With multi-threading off, the sitemaps are fetched in the
// order they are listed, so it is the first ones that are fetched; with multi-threading on,
// which ones are depends on how fast the server answers.
// Nothing is recorded if no sitemap had to be left out.
//
// The same limit is on the sitemaps GetSitemaps returns, which are the ones the documents
// list whether or not they are fetched: no more of them are kept than the limit allows, and
// the same error is recorded if one had to be left out of them. A call that fetches fewer
// sitemaps than it finds, as one with patterns set with SetFollow does, can therefore reach
// the limit by the sitemaps it finds.
//
// The limit bounds the number of requests a document can make the parser send, and the memory
// the sitemaps of a call take up. Without it, a server can keep a call busy for as long as it
// likes by listing sitemaps that list ever further sitemaps.
//
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it. The value that applies to a call is the one set when the call starts.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxSitemaps(maxSitemaps int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxSitemaps")
	if maxSitemaps < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxSitemaps", Err: fmt.Errorf("must be >= 0, got %d", maxSitemaps)})
		return s
	}
	s.cfg.maxSitemaps = maxSitemaps

	return s
}

// SetMaxURLs sets the maximum number of URLs a Parse or ParseContext call collects.
// The default is 10,000,000. A value of 0 means no limit.
//
// The URLs that GetURLs returns count. An entry that is not valid does not, and neither does
// a URL that the patterns set with SetRules leave out.
//
// Once the limit is reached, the call stops collecting: what is left of the document that
// reached the limit is left out unread, so it adds no errors either, the sitemaps that are
// left are not fetched, and a *ParseError naming the URL the call was made for is recorded,
// once for the call. It wraps ErrMaxURLs, which errors.Is matches. The call does not fail:
// the URLs collected until then are returned. With multi-threading off, they are the first
// ones in the order the sitemaps list them; with multi-threading on, which ones they are
// depends on how fast the server answers.
// Nothing is recorded if nothing had to be left out.
//
// The limit bounds the memory the URLs of a call take up, whatever the documents list. The
// default is a last resort rather than a tight bound: ten million URLs take up gigabytes.
// Set a lower limit for documents that are not trusted.
//
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it. The value that applies to a call is the one set when the call starts.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxURLs(maxURLs int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxURLs")
	if maxURLs < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxURLs", Err: fmt.Errorf("must be >= 0, got %d", maxURLs)})
		return s
	}
	s.cfg.maxURLs = maxURLs

	return s
}

// SetMaxValidationErrors sets the maximum number of validation errors a Parse or ParseContext
// call records. The default is 100,000. A value of 0 means no limit.
//
// What is wrong with an entry of a document is recorded as a *ValidationError: a location
// that is no URL, a value that cannot be parsed, a field strict mode requires. How many
// entries of a document are not valid is up to the server, and such an entry does not count
// towards the limit set with SetMaxURLs. The errors of this type that GetErrors returns
// count, whatever they are about.
//
// Once the limit is reached, the validation errors that are left are not recorded, and a
// *ParseError naming the URL the call was made for is recorded, once for the call. It wraps
// ErrMaxValidationErrors, which errors.Is matches. Nothing else changes: the documents are
// read to their end, and the URLs and the sitemaps of the call are the ones a call without
// the limit returns. With multi-threading off, the errors recorded are the first ones in the
// order the sitemaps list the entries; with multi-threading on, which ones they are depends
// on how fast the server answers.
// Nothing is recorded if no error had to be left out.
//
// The errors of the other types do not count and are recorded whatever the limit is: a
// *NetworkError and a *ParseError are about a sitemap or about the call as a whole, so there
// is at most one of them for a sitemap the call fetches, and a few for the call. A call
// therefore still tells every sitemap it could not fetch or parse.
//
// The limit bounds the memory the errors of a call take up, whatever the documents hold,
// together with the limit set with SetMaxSitemaps.
//
// Negative values are rejected and a *ConfigError is recorded; a later call with a valid
// value clears it. The value that applies to a call is the one set when the call starts.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetMaxValidationErrors(maxValidationErrors int) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearConfigErrors("maxValidationErrors")
	if maxValidationErrors < 0 {
		s.errs = append(s.errs, &ConfigError{Field: "maxValidationErrors", Err: fmt.Errorf("must be >= 0, got %d", maxValidationErrors)})
		return s
	}
	s.cfg.maxValidationErrors = maxValidationErrors

	return s
}

// SetFollow sets the follow patterns using the provided list of regex strings and compiles them into regex objects.
// When patterns are set, only the sitemaps whose URL matches one of them are fetched, whether a
// sitemap index lists them or a robots.txt names them. The URL passed to Parse is always fetched.
// Patterns longer than maxRegexPatternLength characters are rejected with a *ConfigError.
// Any errors encountered during compilation are recorded as *ConfigError values.
// Each call replaces both the patterns and the errors recorded by the previous call.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetFollow(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.follow = regexes
	s.cfg.followRegexes = s.compilePatterns("follow", regexes)

	return s
}

// SetRules sets the rules patterns using the provided list of regex strings and compiles them into regex objects.
// Patterns longer than maxRegexPatternLength characters are rejected with a *ConfigError.
// Any errors encountered during compilation are recorded as *ConfigError values.
// Each call replaces both the patterns and the errors recorded by the previous call.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetRules(regexes []string) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.rules = regexes
	s.cfg.rulesRegexes = s.compilePatterns("rules", regexes)

	return s
}

// compilePatterns compiles the patterns of a setting that takes regular expressions, and
// returns the compiled ones in the order of the patterns. field names the setting: "follow"
// or "rules".
// A pattern that is longer than maxRegexPatternLength characters, or that does not compile, is
// left out and recorded as a *ConfigError for field. The errors an earlier call recorded for
// the setting are dropped first, see clearConfigErrors.
// Must be called with s.mu held.
func (s *S) compilePatterns(field string, patterns []string) []*regexp.Regexp {
	s.clearConfigErrors(field)
	var regexes []*regexp.Regexp
	for _, pattern := range patterns {
		if len(pattern) > maxRegexPatternLength {
			s.errs = append(s.errs, &ConfigError{Field: field, Err: fmt.Errorf("pattern exceeds maximum length of %d characters (%d)", maxRegexPatternLength, len(pattern))})
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			s.errs = append(s.errs, &ConfigError{Field: field, Err: err})
			continue
		}
		regexes = append(regexes, re)
	}
	return regexes
}

// SetHTTPClient sets a custom HTTP client for the Sitemap Parser.
// When set, the provided client is used for all HTTP requests instead of the
// internally created default client. This allows callers to configure custom
// transports, proxies, TLS settings, authentication, or timeout strategies.
// When a custom client is provided, SetFetchTimeout has no effect; the
// client's own Timeout field controls the request deadline.
// Pass nil to reset to the default client behaviour.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetHTTPClient(client *http.Client) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.httpClient = client

	return s
}

// SetStrict enables or disables strict mode for URL validation.
// In strict mode, all URLs in sitemap <loc> elements must be absolute HTTP(S) URLs
// on the same host and protocol as the sitemap file, must hold neither a space, nor a control
// character, nor one of the characters " < > \ ^ ` { | }, which a URL has to give
// percent-encoded, and must not exceed 2048 characters, as required by the sitemaps.org
// specification.
// The host is compared without regard to letter case, and the default port of the protocol is
// the same as no port.
// In tolerant mode (default), relative URLs are resolved against the parent sitemap URL, and
// a space in a URL is percent-encoded.
// In both modes a URL has to name a host: one that names none is skipped and reported.
// The function returns a pointer to the S structure to allow method chaining.
func (s *S) SetStrict(strict bool) *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.strict = strict

	return s
}

// clearConfigErrors drops the configuration errors recorded for field, so that every setter
// call replaces the outcome of the previous call for the same setting instead of leaving a
// stale error behind. Must be called with s.mu held.
func (s *S) clearConfigErrors(field string) {
	s.errs = filterErrors(s.errs, func(err error) bool {
		cfgErr, ok := err.(*ConfigError)
		return !ok || cfgErr.Field != field
	})
}

// isConfigError reports whether err was recorded by a configuration setter.
func isConfigError(err error) bool {
	_, ok := err.(*ConfigError)
	return ok
}

// filterErrors returns the errors for which keep reports true, in their original order.
// It builds a new slice instead of filtering in place: GetErrors hands the backing array of
// the error list out to callers, and that copy must not change underneath them.
func filterErrors(errs []error, keep func(error) bool) []error {
	var kept []error
	for _, err := range errs {
		if keep(err) {
			kept = append(kept, err)
		}
	}
	return kept
}

// GetUserAgent returns the current user agent string used for HTTP requests.
func (s *S) GetUserAgent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.userAgent
}

// GetFetchTimeout returns the current fetch timeout in seconds.
func (s *S) GetFetchTimeout() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.fetchTimeout
}

// GetMultiThread returns whether multi-threaded fetching and parsing is enabled.
func (s *S) GetMultiThread() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.multiThread
}

// GetMaxResponseSize returns the maximum allowed HTTP response size in bytes.
func (s *S) GetMaxResponseSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxResponseSize
}

// GetMaxDepth returns the maximum recursion depth for following sitemap indexes.
func (s *S) GetMaxDepth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxDepth
}

// GetMaxConcurrency returns the maximum number of sitemaps fetched or parsed at the same time.
// A value of 0 means unlimited concurrency.
func (s *S) GetMaxConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxConcurrency
}

// GetMaxSitemaps returns the maximum number of sitemaps a Parse or ParseContext call fetches,
// which is the maximum number of sitemaps it lists as well, see SetMaxSitemaps.
// A value of 0 means no limit.
func (s *S) GetMaxSitemaps() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxSitemaps
}

// GetMaxURLs returns the maximum number of URLs a Parse or ParseContext call collects.
// A value of 0 means no limit.
func (s *S) GetMaxURLs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxURLs
}

// GetMaxValidationErrors returns the maximum number of validation errors a Parse or
// ParseContext call records, see SetMaxValidationErrors.
// A value of 0 means no limit.
func (s *S) GetMaxValidationErrors() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.maxValidationErrors
}

// GetFollow returns a copy of the current follow regex pattern strings.
func (s *S) GetFollow() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.cfg.follow))
	copy(result, s.cfg.follow)
	return result
}

// GetRules returns a copy of the current URL filter regex pattern strings.
func (s *S) GetRules() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.cfg.rules))
	copy(result, s.cfg.rules)
	return result
}

// GetHTTPClient returns the custom HTTP client, or nil if the default client behaviour is used.
func (s *S) GetHTTPClient() *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.httpClient
}

// GetStrict returns whether strict URL validation mode is enabled.
func (s *S) GetStrict() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.strict
}

// Parse is a method of the S structure. It parses the given URL and its content.
//
// Parse is a backward-compatible wrapper around ParseContext that uses
// context.Background(). For new code, prefer ParseContext so that callers
// can propagate cancellation and deadlines.
//
// An instance can be reused: every call first discards the URLs, sitemaps and
// errors collected by the previous call. Configuration errors recorded by the Set*
// methods are the exception. They persist across calls, and while any of them
// is outstanding Parse does not parse anything and returns an error with the
// message "errors occurred before parsing, see GetErrors() for details".
// Calling the same setter again with a valid value clears its error.
//
// A sitemap that is reached through a redirect is located at the URL it was
// served from, not at the URL that was requested: relative URLs in it are
// resolved against that URL, strict mode compares the URLs it lists with that
// URL, and the errors about the document name it.
//
// It sets the mainURL field to the given URL. The content is the given URL
// content or, if that is nil, what the URL serves.
// If the URL is that of a robots.txt, it parses the robots.txt file and
// fetches URLs from the sitemap files mentioned in the robots.txt. A URL is
// that of a robots.txt when the last segment of its path is "robots.txt", in
// whichever letter case it is written and whether or not a query or a fragment
// follows it: "https://example.com/robots.txt?v=2" is one.
// If the URL is not that of a robots.txt, the content is checked
// and unzipped if necessary, then parsed and fetched.
// The content of a document is not kept once the document is parsed.
//
// A call fetches no more sitemaps than SetMaxSitemaps allows, and collects no
// more URLs than SetMaxURLs allows, so that it comes to an end whatever the
// documents list. It records no more validation errors than
// SetMaxValidationErrors allows.
//
// The sitemaps the documents of the call list are returned by GetSitemaps,
// whether or not they were fetched.
//
// It returns the S structure, and a nil error if the document at the given
// URL was fetched and parsed. Otherwise the error tells why it was not:
//
//   - a configuration error is outstanding, see above;
//   - the URL is not valid: a *ValidationError;
//   - the document could not be fetched: a *NetworkError;
//   - the document could not be parsed: a *ParseError.
//
// Except for the first, the error returned is the one GetErrors() holds about
// it. What goes wrong further on is in GetErrors() only and does not fail the
// call: a sitemap the document lists that cannot be fetched or parsed, a limit
// that is reached, an entry that is not valid. The error of a limit wraps the
// error that stands for it, such as ErrMaxURLs, so errors.Is tells which limit
// was reached.
func (s *S) Parse(url string, urlContent *string) (*S, error) {
	return s.ParseContext(context.Background(), url, urlContent)
}

// validateInputURL parses url and verifies it uses http or https and names a host. A port is
// no host: "https://:8080/sitemap.xml" names none.
func (s *S) validateInputURL(url string) error {
	parsedURL, parseErr := neturl.Parse(url)
	if parseErr != nil {
		return &ValidationError{URL: url, Err: parseErr}
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{URL: url, Err: fmt.Errorf("invalid URL scheme %q: only http and https are supported", parsedURL.Scheme)}
	}
	if parsedURL.Hostname() == "" {
		return &ValidationError{URL: url, Err: errors.New("missing host")}
	}
	return nil
}

// ParseContext parses the given URL and its content, honoring the supplied
// context for cancellation and deadlines.
//
// The context is propagated through every HTTP request issued by the parser
// (both the initial fetch and the recursive sitemap-index/urlset fetches),
// so cancelling ctx aborts in-flight downloads and prevents new ones from
// starting. Already-parsed URLs accumulated in s.urls before cancellation
// remain available via GetURLs().
//
// If ctx is done by the time the call ends, ParseContext returns a *ParseError
// that names the given URL and wraps the error of ctx, so errors.Is matches
// context.Canceled and context.DeadlineExceeded. The same error is recorded in
// the error list, once for the call: whether the sitemaps are fetched
// concurrently or one at a time, and however many of them were not fetched. A
// request that was cut short is recorded as the *NetworkError of that sitemap
// in addition.
//
// A call that fails for the given URL itself, as described at Parse, returns
// and records that error alone. This is the case when it is the request for
// the given URL that is cut short: the error is the *NetworkError of that
// request, which wraps the error of ctx as well.
//
// All other semantics match Parse.
func (s *S) ParseContext(ctx context.Context, url string, urlContent *string) (*S, error) {
	s.parseMu.Lock()
	defer s.parseMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	// Every call starts from a clean state, so nothing collected by a previous call carries
	// over into this one, whichever way this call ends. Configuration errors are the
	// exception: they belong to the instance rather than to a call, and keep blocking
	// parsing until the offending setting is corrected.
	s.mainURL = ""
	s.robotsTxtSitemapURLs = nil
	s.fetchedURLs = make(map[string]struct{})
	s.sitemaps = nil
	s.listedURLs = nil
	s.urls = nil
	s.errs = filterErrors(s.errs, isConfigError)
	// The limits of the call are those set by now: a SetMaxSitemaps, SetMaxURLs or
	// SetMaxValidationErrors call made while this one runs applies to the next one.
	s.limits = callLimits{
		maxSitemaps:         s.cfg.maxSitemaps,
		maxURLs:             s.cfg.maxURLs,
		maxValidationErrors: s.cfg.maxValidationErrors,
	}

	if len(s.errs) > 0 {
		s.mu.Unlock()
		return s, errors.New("errors occurred before parsing, see GetErrors() for details")
	}

	if urlContent == nil {
		if vErr := s.validateInputURL(url); vErr != nil {
			s.addError(vErr)
			s.mu.Unlock()
			return s, vErr
		}
	}

	if s.cfg.maxConcurrency > 0 {
		s.sem = make(chan struct{}, s.cfg.maxConcurrency)
	} else {
		s.sem = nil
	}
	// Whether the sitemaps are fetched concurrently is decided here, with the lock held and
	// once for the whole call: a SetMultiThread call made while this one runs applies to the
	// next one.
	parseAndFetchUrls := s.parseAndFetchUrlsSequential
	if s.cfg.multiThread {
		parseAndFetchUrls = s.parseAndFetchUrlsMultiThread
	}
	s.mu.Unlock()

	s.mainURL = url
	content, servedFrom, err := s.setContent(ctx, urlContent)
	if err != nil {
		s.mu.Lock()
		s.addError(err)
		s.mu.Unlock()
		return s, err
	}

	if namesRobotsTXT(s.mainURL) {
		s.mu.Lock()
		s.parseRobotsTXT(content)
		locations := s.robotsTXTSitemapLocations(servedFrom)
		s.mu.Unlock()

		// The sitemaps a robots.txt lists are fetched the way those of a sitemap index are,
		// so that every setting governing the fetches applies to them as well.
		parseAndFetchUrls(ctx, servedFrom, locations, robotsTXTDepth)
	} else {
		locations, err := s.parseDocument(servedFrom, content)
		if err != nil {
			// The document the call is about cannot be parsed, which fails the call the way
			// its fetch failing does. The error is in the error list already.
			return s, err
		}

		parseAndFetchUrls(ctx, servedFrom, locations, 0)
	}

	s.mu.Lock()
	s.reportLimits(url)
	s.mu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// The cancellation is recorded here, and nowhere else: once for the call, whichever
		// way the sitemaps are fetched and wherever the cancellation caught them.
		err := &ParseError{URL: url, Err: ctxErr}
		s.mu.Lock()
		s.addError(err)
		s.mu.Unlock()
		return s, err
	}

	return s, nil
}

// GetErrorsCount returns the number of errors GetErrors returns.
// It returns 0 for a nil *S.
func (s *S) GetErrorsCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.errs))
}

// GetErrors returns the errors of the most recent Parse or ParseContext call, together with
// the configuration errors that are outstanding: those recorded by a Set method that was
// called with a value it does not accept, and not again with a valid one since.
// The errors of a call are discarded when the next call starts; configuration errors are kept
// until the setting is corrected.
//
// Each error is a *ConfigError, a *NetworkError, a *ParseError or a *ValidationError, to be
// told apart with errors.As. They are in the order they were recorded in. With multi-threading
// on, see SetMultiThread, that order is not the same from one call to the next for the errors
// of different sitemaps.
//
// An error that tells a limit of the call was reached wraps the error that stands for the
// limit, to be told with errors.Is: ErrMaxResponseSize, ErrMaxDepth, ErrMaxSitemaps,
// ErrMaxURLs or ErrMaxValidationErrors.
//
// No more *ValidationError values are among them than the limit set with
// SetMaxValidationErrors allows. If a call had more to record, it left the later ones out and
// recorded the error of that limit.
//
// The error Parse or ParseContext returned is among them, unless it tells that configuration
// errors are outstanding: then the configuration errors are. A call that returns a nil error
// may have recorded errors as well, about sitemaps and entries it had to skip and about limits
// it reached.
//
// While a call is running, GetErrors returns the errors recorded so far; those recorded
// afterwards are not added to the slice it returned.
//
// The slice that is returned is the list the instance keeps, not a copy of it. It must not be
// modified: an error that is set or moved in it is set or moved for every other caller as
// well. Appending to it is safe, as the slice has no capacity to spare and append therefore
// copies it. Errors goes through the same errors without handing the list out, and
// slices.Collect(s.Errors()) returns a list the caller may sort or change.
// The slice is nil for a nil *S.
func (s *S) GetErrors() []error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clip(s.errs)
}

// Errors returns an iterator over the errors GetErrors returns, in the same order:
//
//	for err := range s.Errors() {
//		log.Print(err)
//	}
//
// It goes through the errors as they are when the loop starts. Errors that are recorded while
// the loop runs are not among them, and a Parse or ParseContext call that starts in the
// meantime does not disturb it. No lock is held while the loop runs, so the loop may call
// every method of s. The iterator can be used for any number of loops, each of which starts
// anew.
//
// The errors are the values the instance keeps, the very error Parse or ParseContext
// returned among them, so that they can be compared with it. The list is not handed out:
// nothing a loop does changes which errors the instance returns, or their order.
// The iterator yields nothing for a nil *S.
func (s *S) Errors() iter.Seq[error] {
	return func(yield func(error) bool) {
		for _, err := range s.GetErrors() {
			if !yield(err) {
				return
			}
		}
	}
}

// GetSitemaps returns the sitemaps that the documents of the most recent Parse or
// ParseContext call list: the entries of the sitemap indexes the call read, and the Sitemap
// lines of the robots.txt it was made for.
//
// A sitemap is among them whether or not the call fetched it. One that the patterns set with
// SetFollow leave out is, and so is one that could not be fetched, and one that a limit of
// the call kept from being fetched. A location that is no URL to request is not: it is
// reported in GetErrors instead. Neither is the document the call was made for, unless a
// document of the call lists it.
//
// Each sitemap is returned once, however many documents list it, and by the URL it is
// requested by, see Sitemap. The sitemaps of a document are in the order the document lists
// them. With multi-threading off, the documents are read one after the other, so the order is
// the same from one call to the next; with multi-threading on, see SetMultiThread, it is not
// for the sitemaps of different documents.
//
// No more sitemaps are returned than the limit set with SetMaxSitemaps allows. If the
// documents list more, the first ones the call found are returned, and the error that tells
// the limit was reached is recorded. The sitemaps that are listed and the ones that are
// fetched are counted apart, so the ones returned then need not be the ones that were fetched.
//
// A sitemap that a sitemap index lists comes with the date the index gives for it, see
// Sitemap.LastMod.
//
// While a call is running, GetSitemaps returns the sitemaps found so far.
// The slice that is returned is a copy the caller may modify, the dates in it included. It is
// never nil, and empty for a nil *S.
func (s *S) GetSitemaps() []Sitemap {
	if s == nil {
		return []Sitemap{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sitemaps := make([]Sitemap, len(s.sitemaps))
	for i, sitemap := range s.sitemaps {
		if sitemap.LastMod != nil {
			// The date is copied too: left as it is, it would be the one s keeps, and a caller
			// that changes it would change what the next caller is given.
			lastMod := *sitemap.LastMod
			sitemap.LastMod = &lastMod
		}
		sitemaps[i] = sitemap
	}
	return sitemaps
}

// GetSitemapCount returns the number of sitemaps GetSitemaps returns.
// It returns 0 for a nil *S.
func (s *S) GetSitemapCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.sitemaps))
}

// GetURLs returns the URLs of the most recent Parse or ParseContext call: the pages that the
// sitemaps it read list, less the ones the patterns set with SetRules leave out.
//
// The URLs of a document are in the order the document lists them. With multi-threading off,
// the documents are read one after the other, so the order is the same from one call to the
// next; with multi-threading on, see SetMultiThread, it is not for the URLs of different
// documents. No more URLs are returned than the limit set with SetMaxURLs allows.
//
// While a call is running, GetURLs returns the URLs collected so far; those collected
// afterwards are not added to the slice it returned.
//
// The slice that is returned is the list the instance keeps, not a copy of it, so a call
// costs the same however many URLs there are. It must not be modified: a URL that is set or
// moved in it, as sorting it does, or changed through one of its pointers, is changed for
// every other caller as well, and for GetRandomURLs and URLs. Appending to it is safe, as
// the slice has no capacity to spare and append therefore copies it. URLs goes through the
// same URLs as copies, and slices.Collect(s.URLs()) returns a list the caller may sort or
// change.
// The slice is never nil, and empty for a nil *S.
func (s *S) GetURLs() []URL {
	if s == nil {
		return []URL{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) <= 0 {
		return []URL{}
	}
	return slices.Clip(s.urls)
}

// URLs returns an iterator over the URLs GetURLs returns, in the same order:
//
//	for u := range s.URLs() {
//		fmt.Println(u.Loc)
//	}
//
// Each URL is a copy that is the caller's own. Everything it points to is copied with it,
// its dates and values and what the extensions say about the page, so nothing that is done
// to it reaches the URLs the instance keeps. The list itself is not copied: a loop takes up
// the memory of one URL at a time, however many there are.
//
// slices.Collect(s.URLs()) returns the URLs as a list of such copies, which the caller may
// sort and change. It cannot tell how long the list is going to be and grows it as it goes,
// which takes several times the memory of the list where the list is long. Giving the list
// its capacity spares that:
//
//	urls := slices.AppendSeq(make([]sitemap.URL, 0, s.GetURLCount()), s.URLs())
//
// It goes through the URLs as they are when the loop starts. URLs that are collected while
// the loop runs are not among them, and a Parse or ParseContext call that starts in the
// meantime does not disturb it. No lock is held while the loop runs, so the loop may call
// every method of s. The iterator can be used for any number of loops, each of which starts
// anew.
// The iterator yields nothing for a nil *S.
func (s *S) URLs() iter.Seq[URL] {
	return func(yield func(URL) bool) {
		for _, u := range s.GetURLs() {
			if !yield(cloneURL(u)) {
				return
			}
		}
	}
}

// cloneURL returns a copy of u that shares nothing with it that can be changed: every
// pointer of u and every slice, down to the ones of its videos, points to a copy of its own.
// A pointer that is nil and a slice that is nil or empty are that in the copy as well.
func cloneURL(u URL) URL {
	u.LastMod = clonePointer(u.LastMod)
	u.ChangeFreq = clonePointer(u.ChangeFreq)
	u.Priority = clonePointer(u.Priority)
	u.Images = slices.Clone(u.Images)
	if u.News != nil {
		news := *u.News
		news.PublicationDate = clonePointer(news.PublicationDate)
		u.News = &news
	}
	u.Videos = slices.Clone(u.Videos)
	for i := range u.Videos {
		video := &u.Videos[i]
		video.Duration = clonePointer(video.Duration)
		video.ExpirationDate = clonePointer(video.ExpirationDate)
		video.Rating = clonePointer(video.Rating)
		video.ViewCount = clonePointer(video.ViewCount)
		video.PublicationDate = clonePointer(video.PublicationDate)
		video.Restriction = clonePointer(video.Restriction)
		video.Platform = clonePointer(video.Platform)
		video.Uploader = clonePointer(video.Uploader)
		video.Tags = slices.Clone(video.Tags)
	}
	u.Hreflangs = slices.Clone(u.Hreflangs)
	return u
}

// clonePointer returns a pointer to a copy of the value p points to, and nil if p is nil.
func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}

// GetURLCount returns the count of URLs in the S struct.
func (s *S) GetURLCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.urls) <= 0 {
		return 0
	}
	return int64(len(s.urls))
}

// GetRandomURLs returns a slice of randomly selected URLs from the S object's URL list. The number of URLs to select is specified by the parameter n.
// If n exceeds the number of URLs in the list, all of them are returned, in random order.
// If n is zero or negative, or the S object is nil, an empty slice is returned.
// The function creates a copy of the original URLs list and randomly selects n URLs from it, removing them to avoid duplicates.
// The selected URLs are returned as a new slice. Each of them is a copy that is the caller's
// own, with everything it points to, like the ones URLs yields.
func (s *S) GetRandomURLs(n int) []URL {
	if s == nil || n <= 0 {
		return []URL{}
	}

	s.mu.Lock()
	originalURLs := make([]URL, len(s.urls))
	copy(originalURLs, s.urls)
	s.mu.Unlock()

	// No more URLs can be selected than there are. Without this cap the result would be
	// allocated for n elements, however large n is.
	n = min(n, len(originalURLs))
	randURLs := make([]URL, 0, n)

	for i := 0; i < n; i++ {
		index := rand.IntN(len(originalURLs))
		randURLs = append(randURLs, cloneURL(originalURLs[index]))

		// Remove the selected URL from the original list to avoid duplicates
		originalURLs[index] = originalURLs[len(originalURLs)-1] // Replace it with the last one.
		originalURLs = originalURLs[:len(originalURLs)-1]       // Remove last element.
	}

	return randURLs
}

// setContent extracts the main URL content or returns the provided URL content if not nil.
// It returns the extracted content as a string or an error if there was a problem fetching the content.
// Next to the content it returns the URL the content was served from: the main URL, or the URL
// the request for it was redirected to.
// The supplied context is propagated to the underlying HTTP request when fetching is required.
func (s *S) setContent(ctx context.Context, urlContent *string) (string, string, error) {
	if urlContent != nil {
		return *urlContent, s.mainURL, nil
	}

	return s.fetch(ctx, s.mainURL)
}

// robotsTXTName is the name of the file a URL has to name to be that of a robots.txt.
const robotsTXTName = "robots.txt"

// namesRobotsTXT tells whether rawURL is the URL of a robots.txt: whether the last segment of
// its path is "robots.txt". The query and the fragment of the URL are no part of its path, so
// "https://example.com/robots.txt?v=2" is one, and "https://example.com/sitemap.xml?from=/robots.txt"
// is none. The letter case makes no difference: a server that does not tell the cases apart
// serves the file as "/ROBOTS.TXT" as well, and what it serves is a robots.txt whatever it is
// asked for as. A host of that name is no file: "https://robots.txt" has no path. A URL that
// cannot be parsed, which only a call that passes the content in can have, is none either.
func namesRobotsTXT(rawURL string) bool {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}
	// The segment is cut from the path as the URL gives it and decoded then, so that a slash
	// that is percent-encoded ends no segment: "/a%2Frobots.txt" names another file. What
	// EscapedPath returns can be decoded, so there is no error to look at.
	path := parsed.EscapedPath()
	name, _ := neturl.PathUnescape(path[strings.LastIndexByte(path, '/')+1:])
	// A letter that is not in ASCII takes more than one byte, so the length rules out the
	// letters of other alphabets that fold to these: "ſ", the long s, is no "s" here.
	return len(name) == len(robotsTXTName) && strings.EqualFold(name, robotsTXTName)
}

// parseRobotsTXT retrieves the sitemap URLs from the provided robots.txt content.
// It splits the content into lines and checks for lines beginning with "Sitemap:"
// (case-insensitive). UTF-8 BOM at the beginning of the file is stripped, lines
// starting with "#" are treated as comments and skipped, and any inline comment
// (text following an unescaped "#") is removed before extracting the URL.
// A value that is not empty is appended to the robotsTxtSitemapURLs slice as it stands in the
// file; whether it is a URL to fetch is decided by robotsTXTSitemapLocations.
// The method does not return any values, but it updates the robotsTxtSitemapURLs field of the S struct.
func (s *S) parseRobotsTXT(robotsTXTContent string) {
	// Strip UTF-8 BOM if present at the very beginning of the file.
	robotsTXTContent = strings.TrimPrefix(robotsTXTContent, utf8BOM)

	for line := range strings.SplitSeq(robotsTXTContent, "\n") {
		line = strings.TrimRight(line, "\r")
		// Trim leading whitespace so that indented directives are still recognised.
		line = strings.TrimLeft(line, " \t")
		// Skip blank lines and full-line comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 8 || !strings.EqualFold(line[:8], "sitemap:") {
			continue
		}
		value := line[8:]
		// Strip inline comments: anything after a "#" is considered a comment.
		if idx := strings.IndexByte(value, '#'); idx >= 0 {
			value = value[:idx]
		}
		url := strings.TrimSpace(value)
		if url != "" {
			// The value is copied: as a part of the content it would keep the whole file in
			// memory for as long as it is kept itself.
			s.robotsTxtSitemapURLs = append(s.robotsTxtSitemapURLs, strings.Clone(url))
		}
	}
}

// robotsTXTSitemapLocations returns the sitemaps to fetch of the ones a robots.txt names.
// What its Sitemap lines hold comes from the document just like the <loc> of a sitemap index
// entry does, and is treated the same way before anything is fetched: it is resolved and
// validated, a value that is rejected is skipped and reported, and a URL that matches none of
// the patterns set with SetFollow is skipped.
// Every sitemap that is not rejected is added to the sitemaps the call found, see listSitemap,
// whether it is to be fetched or not.
// url is the URL the robots.txt was served from.
// Must be called with s.mu held.
func (s *S) robotsTXTSitemapLocations(url string) []string {
	var locations []string
	for _, sitemapURL := range s.robotsTxtSitemapURLs {
		// A robots.txt may name a sitemap of any host: that is how the protocol has the
		// owner of a host approve of a sitemap kept elsewhere.
		location, err := s.resolveAndValidate(sitemapURL, url, false)
		if err != nil {
			s.addError(err)
			continue
		}
		s.listSitemap(Sitemap{Loc: location})
		if !s.matchesFollowFilter(location) {
			continue
		}
		locations = append(locations, location)
	}
	return locations
}

// acquireSlot blocks until a concurrency slot is available, or returns the
// context error if ctx is cancelled while waiting. When the semaphore is nil
// (unlimited concurrency) it still honours ctx so that callers receive a
// deterministic cancellation error.
func (s *S) acquireSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.sem == nil {
		return nil
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseSlot frees a previously acquired concurrency slot. It is a no-op when
// the semaphore is nil.
func (s *S) releaseSlot() {
	if s.sem == nil {
		return
	}
	<-s.sem
}

// fetch retrieves the content of the specified URL using an HTTP GET request.
// It returns the content, the URL the content was served from and an error if
// there was a problem fetching the URL.
// The URL the content was served from is url itself, unless the request was redirected: then
// it is the URL the last redirect led to.
// The HTTP status must be 200 (OK) for the request to be successful.
// The response body is automatically closed after reading using a defer statement.
// The supplied context is attached to the HTTP request, so cancelling it aborts
// the in-flight transfer.
func (s *S) fetch(ctx context.Context, url string) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	fetchTimeout := s.cfg.fetchTimeout
	userAgent := s.cfg.userAgent
	maxResponseSize := s.cfg.maxResponseSize
	httpClient := s.cfg.httpClient
	s.mu.Unlock()

	var client *http.Client
	if httpClient != nil {
		client = httpClient
	} else {
		client = &http.Client{
			Timeout: time.Duration(fetchTimeout) * time.Second,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}

	req.Header.Set("User-Agent", userAgent)

	response, err := client.Do(req)
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		return "", "", &NetworkError{URL: url, Err: fmt.Errorf("received HTTP status %d", response.StatusCode)}
	}

	// The length the response says its body has is what the body is read with, see readAtMost.
	// It is -1 if the response says none.
	body, exceeded, err := readAtMost(response.Body, maxResponseSize, response.ContentLength)
	if err != nil {
		return "", "", &NetworkError{URL: url, Err: err}
	}

	if exceeded {
		return "", "", &NetworkError{URL: url, Err: limitErrorf(ErrMaxResponseSize, "response size exceeds limit of %d bytes", maxResponseSize)}
	}

	return body, finalURL(url, req, response), nil
}

// readBuffers holds the buffers readAtMost reads through, and reads content of unknown size
// into. A read needs them for as long as it lasts. Allocated for every read, the buffers of a
// call that fetches many small sitemaps would take up more memory than the sitemaps themselves,
// and those of a large sitemap of unknown size as much memory again as the sitemap.
var readBuffers = sync.Pool{
	New: func() any {
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

// chunks is content in the buffers it was read into, in the order it was read: buffers of
// readBuffers, each of them full but the last.
//
// This is how content of unknown size is read. Read into a string, it has the string allocated
// anew and copied every time the string turns out too small for what follows, which allocates
// five times the memory of the content by the time all of it is read. Read into buffers, it is
// copied once, into a string of its size, when the size is known.
type chunks struct {
	// buffers is never empty.
	buffers []*[]byte
	// filled is the number of bytes in the last buffer.
	filled int
}

// newChunks returns chunks without content. They have to be released once they are not needed.
func newChunks() chunks {
	return chunks{buffers: []*[]byte{readBuffers.Get().(*[]byte)}}
}

// last returns the buffer that is being read into.
func (c *chunks) last() []byte {
	return *c.buffers[len(c.buffers)-1]
}

// readFrom reads r to its end, and adds what it read to the content.
func (c *chunks) readFrom(r io.Reader) error {
	for {
		if c.filled == len(c.last()) {
			c.buffers = append(c.buffers, readBuffers.Get().(*[]byte))
			c.filled = 0
		}
		n, err := r.Read(c.last()[c.filled:])
		c.filled += n
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// len returns the number of bytes of the content.
func (c *chunks) len() int {
	return (len(c.buffers)-1)*len(c.last()) + c.filled
}

// appendTo appends the content to content.
func (c *chunks) appendTo(content *strings.Builder) {
	for _, buffer := range c.buffers[:len(c.buffers)-1] {
		content.Write(*buffer)
	}
	content.Write(c.last()[:c.filled])
}

// release gives the buffers back for other reads to use. The chunks must not be used after it.
func (c *chunks) release() {
	for _, buffer := range c.buffers {
		readBuffers.Put(buffer)
	}
}

// fill reads r into content, through buffer, until content is size bytes long or r comes to its
// end. It reports whether r came to its end, and is not to be read any further.
func fill(content *strings.Builder, size int, r io.Reader, buffer []byte) (bool, error) {
	for content.Len() < size {
		n, err := r.Read(buffer[:min(len(buffer), size-content.Len())])
		content.Write(buffer[:n])
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}

// presizeLimit is the largest size a string is allocated in before there is content to fill it:
// the size of the largest sitemap the protocol allows. The size is the word of whoever sends
// the content, and a string is allocated on it at once. Content that is said to be larger is
// read like content of unknown size, so that what a response says of itself cannot have more
// memory allocated than this, however high the limit on the size of a response is set.
const presizeLimit = defaultMaxResponseSize

// readAtMost reads r to its end and returns what it read, if that is no more than limit bytes.
// If it is more, exceeded is true and no content is returned: r is read one byte past the limit
// then, and no further. The one byte tells content that exceeds the limit from content that
// fits it exactly.
// A limit of math.MaxInt64 has no byte past it, and nothing can exceed it: limit+1 would
// overflow to a negative number, and a reader limited to that reads nothing at all.
// If reading fails, the error is returned, and no content with it.
//
// size is the number of bytes r is said to hold, and is not positive if that is not known. It
// decides how the content is read and never what is returned, as it need not be true:
//   - Content of the size it is said to have is read into the string that is returned, which
//     is allocated once, in that size. Reading it takes up no more memory than the content.
//   - Content of unknown size is read into buffers, and copied into a string of its size once
//     that is known, see chunks. Reading it takes up twice the memory of the content, half of
//     it buffers that the next read uses again. What follows content that turns out longer
//     than it is said to be is read the same way.
//   - Content that turns out shorter than it is said to be is copied into a string of its own
//     size, so that the memory allocated for the rest is not kept for as long as the content is.
//   - A size above limit or presizeLimit is taken for unknown.
//
// The content is copied once, into the string that is returned, and is not copied again on its
// way through the parser. Content that exceeds the limit, or that fails to be read, is not
// copied at all.
func readAtMost(r io.Reader, limit, size int64) (content string, exceeded bool, err error) {
	if limit < math.MaxInt64 {
		r = io.LimitReader(r, limit+1)
	}

	rest := newChunks()
	defer rest.release()

	// What r is said to hold is read into head, through the buffer of rest, which is not in
	// use until then. What follows it is read into rest: everything if no size is given,
	// and nothing if r came to its end already.
	var head strings.Builder
	sized := 0
	if 0 < size && size <= min(limit, presizeLimit) {
		sized = int(size)
		head.Grow(sized)
	}
	ended, err := fill(&head, sized, r, rest.last())
	if err == nil && !ended {
		err = rest.readFrom(r)
	}
	if err != nil {
		return "", false, err
	}

	total := head.Len() + rest.len()
	if int64(total) > limit {
		return "", true, nil
	}
	// Content of the size it is said to have is all in head, which is as large as the content.
	if total == sized {
		return head.String(), false, nil
	}

	var whole strings.Builder
	whole.Grow(total)
	whole.WriteString(head.String())
	rest.appendTo(&whole)
	return whole.String(), false, nil
}

// finalURL returns the URL the response to req was served from: url, the URL req was made for,
// or the URL the request ended at when it was redirected.
// A document is located where it was served from, not where the request for it began.
// Relative URLs in it are resolved against that URL (RFC 3986, section 5.1.3), and it is the
// location the URLs a sitemap lists are compared with in strict mode.
// When no redirect took place, url is returned as it was passed in, so that the URL of a
// document stays spelled the way the caller or the sitemap naming it spelled it.
func finalURL(url string, req *http.Request, response *http.Response) string {
	// A RoundTripper is not required to name the request it answered; http.Transport does.
	last := response.Request
	if last == nil || last.URL == nil {
		return url
	}
	if final := last.URL.String(); final != req.URL.String() {
		return final
	}
	return url
}

// checkAndUnzipContent checks if the content is a gzip file and unzips it if necessary.
// If the content is a gzip file, it returns the uncompressed content, and the content as it is
// otherwise.
// The decompressed size is capped at cfg.maxResponseSize, so a small compressed payload
// cannot expand without bound.
// If an error occurs during unzipping, or the decompressed size exceeds the cap, it appends
// a *ParseError (with the provided url) and returns ok as false. There is nothing to parse
// then: content that begins like a gzip file is no sitemap of any other format, so parsing it
// as it is would only report the same document a second time.
//
// Param url: The URL the content was fetched from (used for error context)
// Param content: The content to be checked and possibly unzipped
// Return string: The checked and possibly uncompressed content
// Return ok: Whether there is content to parse
func (s *S) checkAndUnzipContent(url string, content string) (unzipped string, ok bool) {
	if !strings.HasPrefix(content, gzipPrefix) {
		return content, true
	}
	maxSize := s.cfg.maxResponseSize
	if maxSize <= 0 {
		// A zero-value S (one not created via New) has no configured limit.
		// Fall back to the default rather than leaving decompression unbounded.
		maxSize = defaultMaxResponseSize
	}
	uncompressed, err := unzip(content, maxSize)
	if err != nil {
		s.addError(&ParseError{URL: url, Err: err})
		return "", false
	}
	return uncompressed, true
}

// claimSitemap decides whether the sitemap at url is to be fetched, and counts it as fetched
// if it is. It returns fetch as false for a sitemap that was fetched already: a sitemap is
// fetched once in a call, however many documents list it.
//
// It returns limited as true if a limit of the call is reached, which goes for every sitemap
// that is still to come as well, and notes that a sitemap had to be left out for it. The limit
// is the one set with SetMaxSitemaps on the sitemaps the call fetches, or the one set with
// SetMaxURLs: a call that cannot collect any more URLs has no use for further sitemaps.
//
// If fetchedURLs has not been initialised (e.g. in direct unit tests), it is initialised here.
func (s *S) claimSitemap(url string) (fetch, limited bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, fetched := s.fetchedURLs[url]; fetched {
		return false, false
	}
	if s.limits.maxSitemaps > 0 && len(s.fetchedURLs) >= s.limits.maxSitemaps {
		s.limits.sitemapsLeftOut = true
		return false, true
	}
	if !s.hasRoomForURL() {
		return false, true
	}
	if s.fetchedURLs == nil {
		s.fetchedURLs = make(map[string]struct{})
	}
	s.fetchedURLs[url] = struct{}{}
	return true, false
}

// listSitemap adds sitemap to the sitemaps the call found, unless it is among them already:
// a sitemap is listed once in a call, however many documents list it.
//
// Of the dates the documents give for a sitemap, the latest is kept, and a date takes the
// place of none. Which document is read first is a matter of timing when the documents are
// read side by side, and a robots.txt that names a sitemap gives no date for it; this way the
// date depends on neither.
//
// If there are as many of them as the limit set with SetMaxSitemaps allows, the sitemap is
// left out, and it is noted that one had to be. The limit is the one on the sitemaps the call
// fetches, but the two are counted apart: a sitemap that is listed need not be fetched.
//
// Must be called with s.mu held, unless no other goroutine has access to s.
func (s *S) listSitemap(sitemap Sitemap) {
	if i, listed := s.listedURLs[sitemap.Loc]; listed {
		if isLater(sitemap.LastMod, s.sitemaps[i].LastMod) {
			s.sitemaps[i].LastMod = sitemap.LastMod
		}
		return
	}
	if s.limits.maxSitemaps > 0 && len(s.sitemaps) >= s.limits.maxSitemaps {
		s.limits.sitemapsLeftOut = true
		return
	}
	if s.listedURLs == nil {
		s.listedURLs = make(map[string]int)
	}
	s.listedURLs[sitemap.Loc] = len(s.sitemaps)
	s.sitemaps = append(s.sitemaps, sitemap)
}

// isLater reports whether date is a later date than other. Nil stands for no date, and a date
// is later than none.
func isLater(date, other *LastModTime) bool {
	return date != nil && (other == nil || date.After(other.Time))
}

// hasRoomForURL reports whether s may collect another URL. If the limit set with SetMaxURLs
// is reached, it notes that something had to be left out for it.
// Must be called with s.mu held, unless no other goroutine has access to s.
func (s *S) hasRoomForURL() bool {
	if s.limits.maxURLs > 0 && len(s.urls) >= s.limits.maxURLs {
		s.limits.urlsLeftOut = true
		return false
	}
	return true
}

// addError records err as an error of the call.
//
// What is wrong with an entry of a document is a *ValidationError, and a document may hold
// any number of entries. No more errors of that type are therefore recorded than the limit
// set with SetMaxValidationErrors allows: one there is no room for is left out, and it is
// noted that one had to be. An error of another type is about a sitemap or about the call as
// a whole, so the sitemaps the call fetches bound their number, and it is recorded in any
// case: which sitemaps a call could not fetch or parse is not lost for the entries of another.
//
// Must be called with s.mu held, unless no other goroutine has access to s.
func (s *S) addError(err error) {
	if _, ok := err.(*ValidationError); ok {
		if s.limits.maxValidationErrors > 0 && s.limits.validationErrors >= s.limits.maxValidationErrors {
			s.limits.validationErrorsLeftOut = true
			return
		}
		s.limits.validationErrors++
	}
	s.errs = append(s.errs, err)
}

// addErrors records errs as errors of the call, see addError.
func (s *S) addErrors(errs []error) {
	for _, err := range errs {
		s.addError(err)
	}
}

// reportLimits records the limits that kept the call for url from fetching or listing a
// sitemap, from collecting a URL or from recording a validation error. Each is recorded once,
// however many sitemaps, URLs and errors were left out, and names the URL the call was made
// for: it is the call as a whole that the limits are about.
// Must be called with s.mu held.
func (s *S) reportLimits(url string) {
	if s.limits.sitemapsLeftOut {
		s.addError(&ParseError{URL: url, Err: limitErrorf(ErrMaxSitemaps, "limit of %d sitemaps reached", s.limits.maxSitemaps)})
	}
	if s.limits.urlsLeftOut {
		s.addError(&ParseError{URL: url, Err: limitErrorf(ErrMaxURLs, "limit of %d URLs reached", s.limits.maxURLs)})
	}
	if s.limits.validationErrorsLeftOut {
		s.addError(&ParseError{URL: url, Err: limitErrorf(ErrMaxValidationErrors, "limit of %d validation errors reached", s.limits.maxValidationErrors)})
	}
}

// withinMaxDepth reports whether the sitemaps found at the given depth may still be fetched.
// If the limit set with SetMaxDepth is reached, it records the error and returns false.
// The error names url, the document that lists the sitemaps: it is that document whose sitemaps
// are not followed.
// The limit is read with the lock held, like every other setting: a setter may be called
// while Parse is running.
func (s *S) withinMaxDepth(url string, depth int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if depth < s.cfg.maxDepth {
		return true
	}
	s.addError(&ParseError{URL: url, Err: limitErrorf(ErrMaxDepth, "max recursion depth of %d reached", s.cfg.maxDepth)})
	return false
}

// parseAndFetchUrlsMultiThread fetches and parses the sitemaps at locations concurrently, see
// walkSitemaps.
func (s *S) parseAndFetchUrlsMultiThread(ctx context.Context, url string, locations []string, depth int) {
	s.walkSitemaps(ctx, url, locations, depth, true)
}

// parseAndFetchUrlsSequential fetches and parses the sitemaps at locations one at a time, see
// walkSitemaps.
func (s *S) parseAndFetchUrlsSequential(ctx context.Context, url string, locations []string, depth int) {
	s.walkSitemaps(ctx, url, locations, depth, false)
}

// walkSitemaps fetches and parses the sitemaps at locations, and the sitemaps those list in
// turn, see walkSitemap. url is the document that lists the locations, as it was served, and
// depth is the depth they are found at, see withinMaxDepth.
//
// Without multiThread the sitemaps are worked on one at a time and in the order of locations,
// each of them together with the sitemaps it lists before the next one is begun.
//
// With multiThread each of them is worked on by a goroutine of its own, and the call returns
// when all of them are done. A goroutine holds a concurrency slot while it fetches and parses
// its sitemap, which makes the limit set with SetMaxConcurrency the number of documents that
// are being fetched or parsed at any time.
// The slot is taken before the goroutine is started, not by the goroutine. A sitemap that has
// to wait for a slot therefore costs no goroutine: the ones that exist are those working on a
// sitemap, and those waiting to go on with the sitemaps a sitemap index of theirs lists.
//
// If the context is done, also while a slot is waited for, the remaining locations are left
// alone. Nothing is recorded for them: that the call was cut short is recorded by ParseContext.
// The same goes for the locations that are left when a limit of the call is reached, see
// claimSitemap.
func (s *S) walkSitemaps(ctx context.Context, url string, locations []string, depth int, multiThread bool) {
	if !s.withinMaxDepth(url, depth) {
		return
	}
	var wg sync.WaitGroup
	for _, location := range locations {
		if ctx.Err() != nil {
			break
		}
		fetch, limited := s.claimSitemap(location)
		if limited {
			break
		}
		if !fetch {
			continue
		}
		if !multiThread {
			s.walkSitemap(ctx, location, depth, multiThread)
			continue
		}
		// acquireSlot also honours ctx cancellation, so a single check
		// here covers both the unlimited-concurrency and bounded paths.
		if s.acquireSlot(ctx) != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.walkSitemap(ctx, location, depth, multiThread)
		}()
	}
	wg.Wait()
}

// walkSitemap fetches and parses the sitemap at location, one of the sitemaps found at depth,
// with fetchAndParse, and goes on with the sitemaps it lists, see walkSitemaps.
// If there is an error during the fetch operation, the error is appended to the "errs" field
// of the S structure.
//
// With multiThread it is run by a goroutine that holds a concurrency slot. It gives the slot
// back before it follows the sitemaps the document lists: the goroutines started for those
// need slots themselves, and would wait for this one forever if it kept its slot.
func (s *S) walkSitemap(ctx context.Context, location string, depth int, multiThread bool) {
	servedFrom, parsedLocations, err := s.fetchAndParse(ctx, location)
	if multiThread {
		s.releaseSlot()
	}
	if err != nil {
		s.mu.Lock()
		s.addError(err)
		s.mu.Unlock()
		return
	}
	if len(parsedLocations) > 0 {
		s.walkSitemaps(ctx, servedFrom, parsedLocations, depth+1, multiThread)
	}
}

// fetchAndParse fetches the sitemap at url and parses it.
// It returns the URL the sitemap was served from and the sitemaps it lists, or the error of the
// fetch. A sitemap that cannot be parsed lists none, and what is wrong with it is in the error
// list: of the sitemaps a document lists, one that cannot be parsed costs only itself.
func (s *S) fetchAndParse(ctx context.Context, url string) (string, []string, error) {
	content, servedFrom, err := s.fetch(ctx, url)
	if err != nil {
		return "", nil, err
	}
	locations, _ := s.parseDocument(servedFrom, content)

	return servedFrom, locations, nil
}

// parseDocument unzips the content served from url if it is gzip content, and parses it. Gzip
// content that cannot be unzipped is not parsed.
// It returns the sitemaps the document lists, and the error that tells the document could not
// be parsed if it could not. Like everything else that is wrong with the document, that error
// is added to the error list too.
//
// The work is done without holding s.mu. Unzipping and decoding a large document takes long, and
// with the lock held for it the documents would be processed one at a time however many were
// fetched concurrently, while every getter waited for the one being processed. The document is
// therefore processed by an instance of its own, which no other goroutine has access to. That
// instance has the settings of s as they stand when the work begins, so they are the same for
// the whole document. What it collects is added to s with the lock held and in one step, which
// keeps the URLs of a document together and in the order the document lists them.
//
// A call that has collected as many URLs as the limit set with SetMaxURLs allows has no use
// for the document: it is not parsed then, and nothing is returned.
//
// Must be called without s.mu held.
func (s *S) parseDocument(url string, content string) ([]string, error) {
	document := s.newDocument()
	if document == nil {
		return nil, nil
	}

	var locations []string
	if unzipped, ok := document.checkAndUnzipContent(url, content); ok {
		locations = document.parse(url, unzipped)
	}
	s.addDocument(document)

	return locations, documentError(document.errs)
}

// newDocument returns the instance a document is processed by, see parseDocument. It returns
// nil if s has no room for another URL.
//
// The instance may collect as many URLs as s has room for at this point, so that a document
// does not pile up URLs only to have them dropped when they are added to s. It may list as
// many sitemaps as the call may in all: which of them s has listed already is only known when
// they are added to it, and one that is listed already takes up no room there. It counts its
// validation errors on from those s has recorded at this point, for the reason it has for
// the URLs: it records as many as s has room for.
//
// Must be called without s.mu held.
func (s *S) newDocument() *S {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasRoomForURL() {
		return nil
	}

	document := &S{cfg: s.cfg}
	document.limits.maxSitemaps = s.limits.maxSitemaps
	if s.limits.maxURLs > 0 {
		document.limits.maxURLs = s.limits.maxURLs - len(s.urls)
	}
	document.limits.maxValidationErrors = s.limits.maxValidationErrors
	document.limits.validationErrors = s.limits.validationErrors
	return document
}

// addDocument adds what document, an instance made by newDocument, collected to s.
//
// Must be called without s.mu held.
func (s *S) addDocument(document *S) {
	s.mu.Lock()
	defer s.mu.Unlock()

	urls := document.urls
	if room := s.limits.maxURLs - len(s.urls); s.limits.maxURLs > 0 && len(urls) > room {
		// There was room for these URLs when the document was begun, but other documents
		// were added in the meantime. The URLs there is no room for any more are given up.
		clear(urls[room:])
		urls = urls[:room]
		s.limits.urlsLeftOut = true
	}
	if s.urls == nil {
		// Nothing is collected yet, so the URLs of the document are taken over as they are
		// rather than copied: a call that parses a single sitemap is spared a second copy of
		// all of its URLs.
		s.urls = urls
	} else {
		s.urls = append(s.urls, urls...)
	}
	s.limits.urlsLeftOut = s.limits.urlsLeftOut || document.limits.urlsLeftOut
	for _, sitemap := range document.sitemaps {
		s.listSitemap(sitemap)
	}
	s.limits.sitemapsLeftOut = s.limits.sitemapsLeftOut || document.limits.sitemapsLeftOut
	// There was room for these errors when the document was begun as well, and the ones there
	// is no room for any more are left out here.
	s.addErrors(document.errs)
	s.limits.validationErrorsLeftOut = s.limits.validationErrorsLeftOut || document.limits.validationErrorsLeftOut
}

// documentError returns the error that tells a document could not be parsed, out of errs, the
// errors the document yielded. It returns nil if the document was parsed.
// A document that cannot be parsed is reported as a *ParseError, what is wrong with one of its
// entries as a *ValidationError. Of several, the first is returned.
func documentError(errs []error) error {
	for _, err := range errs {
		var parseErr *ParseError
		if errors.As(err, &parseErr) {
			return parseErr
		}
	}
	return nil
}

// newXMLDecoder returns a decoder for content that honours the encoding named in its XML
// declaration, so that a document in an encoding other than UTF-8 is transcoded while it is
// read. Every XML read in this package goes through it: a decoder without a CharsetReader
// rejects any document that declares another encoding.
func newXMLDecoder(content string) *xml.Decoder {
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder
}

// newDecoder returns the decoder the parsers read content with. Strict mode requires
// well-formed XML. Tolerant mode puts up with the mistakes encoding/xml can read past: an
// unescaped "&" or an unknown entity such as "&nbsp;" is taken literally, and a missing end
// tag is made up for.
func (s *S) newDecoder(content string) *xml.Decoder {
	decoder := newXMLDecoder(content)
	decoder.Strict = s.cfg.strict
	return decoder
}

// detectionCharsetReader is the CharsetReader of root element detection. It transcodes like
// the one newXMLDecoder installs, but reads the bytes as they are when the declared encoding
// is not supported. An encoding that cannot be transcoded must not hide the document type:
// the parser of the detected type then reports the unsupported encoding, instead of the
// document being taken for an unknown format.
func detectionCharsetReader(label string, input io.Reader) (io.Reader, error) {
	if reader, err := charset.NewReaderLabel(label, input); err == nil {
		return reader, nil
	}
	return input, nil
}

// detectRootElement reads the first XML start element from the content
// to determine the document type without fully parsing it.
// Returns the local name of the root element, or an empty string if detection fails.
func detectRootElement(content string) string {
	decoder := newXMLDecoder(content)
	decoder.CharsetReader = detectionCharsetReader
	// Detection is as lenient as the most lenient parser, for the same reason: what is wrong
	// with a document is for the parser of its type to report.
	decoder.Strict = false
	root, err := rootElement(decoder)
	if err != nil {
		return ""
	}
	return root.Name.Local
}

// rootElement reads decoder up to the first start element, which is the root element of the
// document when nothing has been read from decoder yet, and returns it.
func rootElement(decoder *xml.Decoder) (xml.StartElement, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if element, ok := token.(xml.StartElement); ok {
			return element, nil
		}
	}
}

// parse parses the provided URL and its content.
// The URL is the one the content was served from, which is not the one that was requested
// when the request was redirected.
// It determines whether the content is a sitemap index or a sitemap by inspecting
// the root XML element, then only invokes the appropriate parser.
// If it is a sitemap index, it adds the sitemaps the index lists to the sitemaps the call
// found, see listSitemap, and returns the locations of the ones that are to be fetched. These
// are returned and not kept: a call that fetches many indexes would otherwise hold every
// entry of every one of them until the next call.
// If it is a sitemap, it adds the URLs from the sitemap to the URL list, and returns nil.
// Parsing errors are added to the error list.
func (s *S) parse(url string, content string) []string {
	rootElement := detectRootElement(content)
	switch rootElement {
	case "sitemapindex":
		return s.parseSitemapIndexContent(url, content)
	case "urlset":
		s.parseURLSetContent(url, content)
	case "rss":
		s.parseRSSContent(url, content)
	case "feed":
		s.parseFeedContent(url, content)
	default:
		s.parseTextContent(url, rootElement, content)
	}
	return nil
}

func (s *S) parseSitemapIndexContent(url, content string) []string {
	var added []string
	smIndex, err := s.parseSitemapIndex(content)
	if err != nil {
		s.addError(&ParseError{URL: url, Err: err})
		return added
	}
	for _, sm := range smIndex.Sitemap {
		sm.Loc = strings.TrimSpace(sm.Loc)
		resolvedLoc, err := s.resolveAndValidateLoc(sm.Loc, url)
		if err != nil {
			s.addError(err)
			continue
		}
		sm.Loc = resolvedLoc
		lastMod := s.lastMod(sm.Loc, dateElement{name: "<lastmod>", text: sm.LastMod, parse: parseLastModTime})
		s.listSitemap(Sitemap{Loc: sm.Loc, LastMod: lastMod})
		if !s.matchesFollowFilter(sm.Loc) {
			continue
		}
		added = append(added, sm.Loc)
	}
	return added
}

func (s *S) parseURLSetContent(url, content string) {
	// The entries are dealt with one by one while the document is read, yet a document that
	// cannot be read to its end yields nothing but the error: what its entries added until
	// then is dropped again, and with it the note that a URL or an error of it had to be left
	// out.
	urls, errs, limits := s.urls, s.errs, s.limits
	err := s.parseURLSet(content, func(entry *urlEntry) {
		s.addURLEntry(entry, url)
	})
	if err != nil {
		s.urls, s.errs, s.limits = urls, errs, limits
		s.addError(&ParseError{URL: url, Err: err})
	}
}

// addURLEntry resolves, validates and filters an entry of the <urlset> served from baseURL,
// and appends the URL it stands for to s.urls. What is wrong with the entry is added to s.errs.
// Once s has no room for another URL, the entries are left out as they come: unread, so that
// they add no errors either.
func (s *S) addURLEntry(entry *urlEntry, baseURL string) {
	if !s.hasRoomForURL() {
		return
	}
	u := entry.URL
	u.Loc = strings.TrimSpace(u.Loc)
	resolvedLoc, err := s.resolveAndValidateLoc(u.Loc, baseURL)
	if err != nil {
		s.addError(err)
		return
	}
	u.Loc = resolvedLoc
	// Tolerant mode puts up with a change frequency that is written in another letter case.
	// Strict mode does not, see validateChangeFreq.
	if !s.cfg.strict && u.ChangeFreq != nil {
		*u.ChangeFreq = canonicalChangeFreq(*u.ChangeFreq)
	}
	// A value that cannot be parsed is left out and reported. Strict mode also skips the
	// entry when the value is one of its own, as it does for a priority out of range; a
	// value of an extension costs the entry nothing more in either mode.
	for _, invalid := range entry.invalid {
		s.addError(&ValidationError{URL: u.Loc, Err: invalid.err()})
	}
	if s.cfg.strict && entry.invalidOwn {
		return
	}
	// Strict mode skips an entry with a value of its own that the protocol does not allow
	// either. Each such value is reported.
	changeFreqErr := s.validateChangeFreq(u.Loc, u.ChangeFreq)
	if changeFreqErr != nil {
		s.addError(changeFreqErr)
	}
	priorityErr := s.validatePriority(u.Loc, u.Priority)
	if priorityErr != nil {
		s.addError(priorityErr)
	}
	if changeFreqErr != nil || priorityErr != nil {
		return
	}
	// Tolerant mode returns the URLs of the extensions the way it returns the location.
	// Strict mode returns them as the document gives them, as it does the location.
	if !s.cfg.strict {
		u.encodeExtensionURLs()
	}
	validImages, imageErrs := s.validateAndFilterImages(u.Loc, u.Images)
	u.Images = validImages
	s.addErrors(imageErrs)
	validNews, newsErrs := s.validateNews(u.Loc, u.News, entry.newsDateText())
	u.News = validNews
	s.addErrors(newsErrs)
	validVideos, videoErrs := s.validateAndFilterVideos(u.Loc, u.Videos)
	u.Videos = validVideos
	s.addErrors(videoErrs)
	validHreflangs, hreflangErrs := s.validateAndFilterHreflangs(u.Loc, u.Hreflangs)
	u.Hreflangs = validHreflangs
	s.addErrors(hreflangErrs)
	if !s.matchesRulesFilter(u.Loc) {
		return
	}
	s.urls = append(s.urls, u)
}

func (s *S) parseRSSContent(url, content string) {
	rssFeed, err := s.parseRSS(content)
	if err != nil {
		s.addError(&ParseError{URL: url, Err: err})
		return
	}
	for _, item := range rssFeed.Channel.Item {
		s.addURL(item.Link, url, dateElement{name: "<pubDate>", text: item.PubDate, parse: parseRFC822Time})
	}
}

func (s *S) parseFeedContent(url, content string) {
	atomFeed, err := s.parseAtom(content)
	if err != nil {
		s.addError(&ParseError{URL: url, Err: err})
		return
	}
	for _, entry := range atomFeed.Entry {
		var loc string
		for _, l := range entry.Link {
			// The whitespace around the value of the attribute is no part of the relation.
			if rel := strings.TrimSpace(l.Rel); rel == "" || rel == "alternate" {
				loc = l.Href
				break
			}
		}
		s.addURL(loc, url, dateElement{name: "<updated>", text: entry.Updated, parse: parseLastModTime})
	}
}

// parseTextContent reads content as a text sitemap, which lists one URL on a line. A line is
// an entry when it begins with "http://" or "https://". Whitespace around a line is ignored,
// and so is the UTF-8 byte order mark the content may begin with. Empty lines, comment lines
// (the ones beginning with "#") and every other line are skipped.
// It is the parser of every content that is in none of the XML formats, hence content without
// a single entry is not taken for a text sitemap: a *ParseError is added for it, which tells
// that the content is empty, or that its format is not known, with the root element that was
// found in it.
func (s *S) parseTextContent(url, rootElement, content string) {
	// The byte order mark is no part of the first line. Left there, it would keep that line
	// from being taken for an entry, and the first URL of the sitemap would be lost unnoticed.
	content = strings.TrimPrefix(content, utf8BOM)

	found := false
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			found = true
			// The line is copied: as a part of the content it would keep the whole document
			// in memory for as long as the URL or an error about it is kept.
			s.addURL(strings.Clone(line), url, dateElement{})
		}
	}
	if found {
		return
	}
	if len(content) == 0 {
		s.addError(&ParseError{URL: url, Err: errors.New("sitemap content is empty")})
	} else {
		s.addError(&ParseError{URL: url, Err: fmt.Errorf("unrecognized sitemap format (root element: %q)", rootElement)})
	}
}

// matchesFollowFilter reports whether the sitemap at loc is one to fetch by the patterns set
// with SetFollow.
func (s *S) matchesFollowFilter(loc string) bool {
	return matchesAny(s.cfg.followRegexes, loc)
}

// matchesRulesFilter reports whether the page at loc is one to collect by the patterns set
// with SetRules.
func (s *S) matchesRulesFilter(loc string) bool {
	return matchesAny(s.cfg.rulesRegexes, loc)
}

// matchesAny reports whether loc matches one of regexes. Without any there is nothing to
// restrict, and every loc matches.
func matchesAny(regexes []*regexp.Regexp, loc string) bool {
	if len(regexes) == 0 {
		return true
	}
	for _, re := range regexes {
		if re.MatchString(loc) {
			return true
		}
	}
	return false
}

// addURL resolves, validates, filters, and appends a single location to s.urls, with the date
// the document gives for it, see lastMod.
// Used by RSS, Atom, and Text parsers.
// An empty location is skipped without an error: the link of a feed item is optional, so an
// item without one is not a mistake, it merely names no page.
// Once s has no room for another URL, the locations are left out as they come, see addURLEntry.
func (s *S) addURL(loc string, baseURL string, date dateElement) {
	loc = strings.TrimSpace(loc)
	if loc == "" || !s.hasRoomForURL() {
		return
	}
	resolvedLoc, err := s.resolveAndValidateLoc(loc, baseURL)
	if err != nil {
		s.addError(err)
		return
	}
	// A date that cannot be parsed is reported whether or not the rules take the URL, as the
	// values of a <url> entry are.
	lastMod := s.lastMod(resolvedLoc, date)
	if s.matchesRulesFilter(resolvedLoc) {
		s.urls = append(s.urls, URL{Loc: resolvedLoc, LastMod: lastMod})
	}
}

// lastMod returns the date that the element date holds for the page or the sitemap at loc. It
// returns nil for an element that is absent, and for one that is empty: such an element names
// no date, see parseDate.
//
// It returns nil for a date that cannot be parsed as well, and adds a *ValidationError for
// loc to s.errs. Such a date costs only itself, in strict mode too, which skips a <url> for a
// <lastmod> like that: the page is returned, and the sitemap is listed and fetched. A sitemap
// would be lost with all of its pages for the date of its entry, and the protocol does not
// say how the date of a feed is written.
func (s *S) lastMod(loc string, date dateElement) *LastModTime {
	if date.text == nil || emptyElement(date.text) {
		return nil
	}
	parsed, err := date.parse(*date.text)
	if err != nil {
		s.addError(&ValidationError{URL: loc, Err: invalidValue{element: date.name, text: *date.text}.err()})
		return nil
	}
	return &parsed
}

// parseSitemapIndex parses the sitemap index data and returns a sitemapIndex object and an error.
// The data parameter contains the XML data of the sitemap index.
// If the data is empty, it returns an error with the message "sitemapindex is empty".
// See decodeDocument.
func (s *S) parseSitemapIndex(data string) (sitemapIndex, error) {
	return decodeDocument[sitemapIndex](s, "sitemapindex", data)
}

// decodeDocument decodes data, the XML of a document of the format name, into a value of the
// type T the format is read as, and returns it with any decoding error that occurred.
// If the data is empty, it returns an error with a message that says so of the format, such
// as "rss is empty".
// It uses an xml.Decoder with charset support, see newDecoder, which is why it takes s.
func decodeDocument[T any](s *S, name, data string) (T, error) {
	var document T
	if len(data) == 0 {
		return document, fmt.Errorf("%s is empty", name)
	}

	err := s.newDecoder(data).Decode(&document)
	return document, err
}

// parseURLSet takes a string of XML data representing a sitemap and hands its <url> entries
// to handle, one at a time and in the order they stand in the document.
// If the data is empty, it returns an error with the message "sitemap is empty".
// It uses an xml.Decoder with charset support to decode the XML data.
// If there is an error during decoding, it returns the decode error. The entries that precede
// the place of the error have been handed to handle by then.
//
// The entries are not collected. Decoded into a slice first, the entries of a large sitemap
// take up several times the memory of the URLs they end up as, for as long as the document is
// being read. The <urlset> is therefore read element by element, the way encoding/xml reads
// it into a structure that has a field for <url>: an element of that name is decoded,
// whatever its namespace, any other element is skipped together with what it contains, and
// what follows the end of the <urlset> is not read.
func (s *S) parseURLSet(data string, handle func(entry *urlEntry)) error {
	if len(data) == 0 {
		return fmt.Errorf("sitemap is empty")
	}

	decoder := s.newDecoder(data)
	root, err := rootElement(decoder)
	if err != nil {
		return err
	}
	if root.Name.Local != "urlset" {
		return xml.UnmarshalError("expected element type <urlset> but have <" + root.Name.Local + ">")
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "url" {
				if err := decoder.Skip(); err != nil {
					return err
				}
				continue
			}
			var entry urlEntry
			if err := decoder.DecodeElement(&entry, &element); err != nil {
				return err
			}
			entry.convert()
			handle(&entry)
		case xml.EndElement:
			return nil
		}
	}
}

// convert parses the elements that were read as text into the typed fields of the embedded
// URL. An element whose content is invalid leaves its field unset and is added to e.invalid.
//
// It also drops the whitespace around the text values of the entry, those of its extensions
// included: the content of an element may be indented or stand on a line of its own, and what
// surrounds it is no more a part of the value than it is of a number or of a date. The location
// of the entry is left to addURLEntry, which resolves it.
func (e *urlEntry) convert() {
	e.LastMod = parseDate(&e.invalid, "<lastmod>", e.LastModText)
	e.ChangeFreq = trimChangeFreq(e.ChangeFreq)
	e.Priority = parseElement(&e.invalid, "<priority>", e.PriorityText, parseFloat32)
	e.invalidOwn = len(e.invalid) > 0

	for i := range e.Images {
		e.Images[i].trimSpace()
	}

	if e.NewsEntry != nil {
		news := e.NewsEntry.News
		news.trimSpace()
		news.PublicationDate = parseDate(&e.invalid, "news <publication_date>", e.NewsEntry.PublicationDateText)
		e.News = &news
	}

	for _, entry := range e.VideoEntries {
		video := entry.Video
		video.trimSpace()
		video.Duration = parseElement(&e.invalid, "video <duration>", entry.DurationText, parseInt)
		video.ExpirationDate = parseDate(&e.invalid, "video <expiration_date>", entry.ExpirationDateText)
		video.Rating = parseElement(&e.invalid, "video <rating>", entry.RatingText, parseFloat32)
		video.ViewCount = parseElement(&e.invalid, "video <view_count>", entry.ViewCountText, parseInt)
		video.PublicationDate = parseDate(&e.invalid, "video <publication_date>", entry.PublicationDateText)
		e.Videos = append(e.Videos, video)
	}

	for i := range e.Hreflangs {
		e.Hreflangs[i].trimSpace()
	}
}

// trimSpaces drops the whitespace around each of the given text values.
func trimSpaces(values ...*string) {
	for _, value := range values {
		*value = strings.TrimSpace(*value)
	}
}

// trimSpace drops the whitespace around the text values of the image.
func (i *Image) trimSpace() {
	trimSpaces(&i.Loc, &i.Title, &i.Caption, &i.GeoLocation, &i.License)
}

// trimSpace drops the whitespace around the text values of the news entry.
func (n *News) trimSpace() {
	trimSpaces(&n.Publication.Name, &n.Publication.Language, &n.Title)
}

// trimSpace drops the whitespace around the text values of the video, the attributes of its
// elements and its tags included.
func (v *Video) trimSpace() {
	trimSpaces(&v.ThumbnailLoc, &v.Title, &v.Description, &v.ContentLoc, &v.PlayerLoc,
		&v.FamilyFriendly, &v.RequiresSubscription, &v.Live)
	if v.Restriction != nil {
		trimSpaces(&v.Restriction.Relationship, &v.Restriction.Value)
	}
	if v.Platform != nil {
		trimSpaces(&v.Platform.Relationship, &v.Platform.Value)
	}
	if v.Uploader != nil {
		trimSpaces(&v.Uploader.Info, &v.Uploader.Value)
	}
	for i := range v.Tags {
		v.Tags[i] = strings.TrimSpace(v.Tags[i])
	}
}

// trimSpace drops the whitespace around the values of the attributes of the link.
func (l *AlternateLink) trimSpace() {
	trimSpaces(&l.Rel, &l.Hreflang, &l.Href)
}

// trimChangeFreq drops the whitespace around the content of a <changefreq> element. It returns
// nil for an element that is empty: such an element names no frequency, so it is read as an
// absent one, the way an empty date is.
func trimChangeFreq(changeFreq *URLChangeFreq) *URLChangeFreq {
	if changeFreq == nil {
		return nil
	}
	*changeFreq = URLChangeFreq(strings.TrimSpace(string(*changeFreq)))
	if *changeFreq == "" {
		return nil
	}
	return changeFreq
}

// canonicalChangeFreq returns the value of the sitemaps.org protocol that changeFreq is when
// letter case is disregarded, "daily" for "Daily". A value that is none of them is returned
// as it is.
func canonicalChangeFreq(changeFreq URLChangeFreq) URLChangeFreq {
	for _, known := range changeFreqs {
		if strings.EqualFold(string(changeFreq), string(known)) {
			return known
		}
	}
	return changeFreq
}

// newsDateText returns the content of the publication date element of the news of the entry
// as it was read. It is nil if the entry has no news, or its news no publication date.
func (e *urlEntry) newsDateText() *string {
	if e.NewsEntry == nil {
		return nil
	}
	return e.NewsEntry.PublicationDateText
}

// parseElement parses the text of an optional element. It returns nil for an element that is
// absent, which is what a nil text stands for, and for one whose content parse rejects. The
// latter is also added to invalid under the given name.
func parseElement[T any](invalid *[]invalidValue, element string, text *string, parse func(string) (T, error)) *T {
	if text == nil {
		return nil
	}
	value, err := parse(*text)
	if err != nil {
		*invalid = append(*invalid, invalidValue{element: element, text: *text})
		return nil
	}
	return &value
}

// parseDate parses the text of an optional element that holds a date, by the rules of
// parseElement. In addition it returns nil for an element that is empty: such an element
// names no date, so it is read as an absent one rather than as the zero time, and it is not
// invalid either.
func parseDate(invalid *[]invalidValue, element string, text *string) *LastModTime {
	if emptyElement(text) {
		return nil
	}
	return parseElement(invalid, element, text, parseLastModTime)
}

// emptyElement reports whether text is the content of an element that is there, but holds
// nothing, or nothing but whitespace. A nil text stands for an element that is absent.
func emptyElement(text *string) bool {
	return text != nil && strings.TrimSpace(*text) == ""
}

// parseFloat32 parses the content of an element holding a decimal number. As when encoding/xml
// decodes into a numeric field, surrounding whitespace is ignored and an empty element reads
// as zero.
func parseFloat32(text string) (float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(text, 32)
	return float32(value), err
}

// parseInt parses the content of an element holding an integer, by the rules of parseFloat32.
func parseInt(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	return strconv.Atoi(text)
}

// err returns the error that reports v.
func (v invalidValue) err() error {
	return fmt.Errorf("invalid %s value %q", v.element, strings.TrimSpace(v.text))
}

// decodeOwnChildren reads the children of the element start, which d has just read the start
// of, up to the end of the element. It is what the UnmarshalXML method of an item of a feed
// reads the item with.
//
// A child that is in the namespace of start is decoded into what target returns for its local
// name, a pointer. Every other child is skipped together with what it contains: one that
// target returns nil for, and one of another namespace.
//
// A feed holds elements of other formats beside its own, which are told apart by their
// namespace alone: an RSS item may have an <atom:link> next to its <link>. encoding/xml reads
// an element into a field by its local name, whatever its namespace, and the last element of
// a name counts for a field that holds one. Read that way, the <atom:link>, which has no
// content, took the place of the <link> that stood before it, and the item was left out for
// having no link.
//
// The feed is taken at its word for its namespace, which is why that of the item decides and
// not the one the format prescribes: none for RSS, that of Atom for Atom. A feed that puts
// its elements into another namespace, or that has a prefix, is read as it was before the
// namespaces were looked at.
func decodeOwnChildren(d *xml.Decoder, start xml.StartElement, target func(name string) any) error {
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			var into any
			if element.Name.Space == start.Name.Space {
				into = target(element.Name.Local)
			}
			if into == nil {
				err = d.Skip()
			} else {
				err = d.DecodeElement(into, &element)
			}
			if err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}

// UnmarshalXML reads the <link> and the <pubDate> of the item, see decodeOwnChildren. The last
// element of a name counts if the item has more than one.
func (item *rssItem) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	return decodeOwnChildren(d, start, func(name string) any {
		switch name {
		case "link":
			return &item.Link
		case "pubDate":
			return &item.PubDate
		}
		return nil
	})
}

// UnmarshalXML reads the <link> elements and the <updated> of the entry, see
// decodeOwnChildren. The last <updated> counts if the entry has more than one.
func (entry *atomEntry) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	return decodeOwnChildren(d, start, func(name string) any {
		switch name {
		case "link":
			entry.Link = append(entry.Link, atomLink{})
			return &entry.Link[len(entry.Link)-1]
		case "updated":
			return &entry.Updated
		}
		return nil
	})
}

// parseRSS parses the RSS 2.0 data and returns an rss object and an error, see decodeDocument.
func (s *S) parseRSS(data string) (rss, error) {
	return decodeDocument[rss](s, "rss", data)
}

// parseAtom parses the Atom 1.0 data and returns an atom object and an error, see
// decodeDocument.
func (s *S) parseAtom(data string) (atom, error) {
	return decodeDocument[atom](s, "atom", data)
}

// maxLocLength is the maximum URL length allowed in a sitemap <loc> element per the sitemaps.org specification.
const maxLocLength = 2048

// maxVideoDuration is the maximum allowed <video:duration> in seconds per the Google specification.
const maxVideoDuration = 28800

// maxVideoTags is the maximum number of <video:tag> elements allowed per video per the Google specification.
const maxVideoTags = 32

// maxVideoRating is the maximum allowed <video:rating> value per the Google specification.
const maxVideoRating = float32(5.0)

// maxRegexPatternLength is the maximum allowed length of a regex pattern string passed to SetFollow or SetRules.
// Go's regexp package uses RE2 semantics and is therefore not vulnerable to catastrophic backtracking,
// but arbitrarily long patterns can still produce large compiled automata and consume significant memory.
const maxRegexPatternLength = 1000

// defaultMaxResponseSize is the default maximum size in bytes of an HTTP response body and of
// decompressed gzip content. 50 MB is the uncompressed sitemap size limit of the sitemaps.org
// specification.
const defaultMaxResponseSize = 50 * 1024 * 1024

// defaultMaxConcurrency is the default maximum number of sitemaps that are fetched or parsed at
// the same time in a Parse call.
// Limiting concurrency by default prevents unbounded goroutine and connection growth when parsing
// large sitemap indexes. Pass 0 to SetMaxConcurrency to restore unlimited concurrency.
const defaultMaxConcurrency = 16

// defaultMaxSitemaps is the default maximum number of sitemaps a Parse call fetches. 50,000 is
// the number of sitemaps a sitemap index may list according to the sitemaps.org specification.
// Pass 0 to SetMaxSitemaps to lift the limit.
const defaultMaxSitemaps = 50000

// defaultMaxURLs is the default maximum number of URLs a Parse call collects. It is more than
// all but the largest sites list, and there to keep a call from collecting URLs without end.
// Pass 0 to SetMaxURLs to lift the limit.
const defaultMaxURLs = 10000000

// defaultMaxValidationErrors is the default maximum number of validation errors a Parse call
// records. It is more than a call has a use for one by one, and there to keep the errors of a
// call from growing without end.
// Pass 0 to SetMaxValidationErrors to lift the limit.
const defaultMaxValidationErrors = 100000

// robotsTXTDepth is the depth at which the sitemaps listed in a robots.txt are fetched: one
// level above 0, the depth of the sitemaps a sitemap index names. A sitemap listed in a
// robots.txt stands where the main URL of a call does when that is a sitemap itself, and
// fetching the main URL does not count towards the limit set with SetMaxDepth either.
const robotsTXTDepth = -1

// utf8BOM is the byte order mark a UTF-8 document may begin with. It tells the encoding of the
// document and is no part of its text. The XML decoder reads past it by itself; the parsers of
// the formats that are read line by line take it off the first line.
const utf8BOM = "\ufeff"

// validateChangeFreq validates the <changefreq> value of a URL entry.
// In strict mode, the value must be one of the values of the sitemaps.org protocol, written
// the way the protocol writes them: "daily", not "Daily".
// In tolerant mode, any value is accepted and nil is returned.
// loc is the page URL used as context in the returned *ValidationError.
func (s *S) validateChangeFreq(loc string, changeFreq *URLChangeFreq) error {
	if !s.cfg.strict || changeFreq == nil {
		return nil
	}
	if !slices.Contains(changeFreqs[:], *changeFreq) {
		return &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: invalid <changefreq> value %q", string(*changeFreq))}
	}
	return nil
}

// validatePriority validates the <priority> value of a URL entry.
// In strict mode, the value must be between 0.0 and 1.0 inclusive per the sitemaps.org specification.
// In tolerant mode, any value is accepted and nil is returned.
// loc is the page URL used as context in the returned *ValidationError.
func (s *S) validatePriority(loc string, priority *float32) error {
	if !s.cfg.strict || priority == nil {
		return nil
	}
	if *priority < 0.0 || *priority > 1.0 {
		return &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: priority %g is out of range [0.0, 1.0]", *priority)}
	}
	return nil
}

// validateAndFilterImages validates the image entries on a parsed URL and returns
// the filtered slice of valid images along with any validation errors.
//
// In tolerant mode, images with an empty Loc are silently dropped. In strict mode,
// an empty Loc is an error. In both modes, a Loc exceeding maxLocLength characters
// is rejected. In strict mode, Loc must additionally be an absolute HTTP or HTTPS URL,
// see extensionURLError. In tolerant mode the URLs of the images have been
// percent-encoded by the time they get here, see URL.encodeExtensionURLs, as have those
// of the videos and of the alternate links.
//
// For an image that is kept, strict mode also validates License, a URL the image need not
// give, see secondaryURLErrors. A License that is no URL is reported and the image is kept.
//
// Note: image Loc values are not required to share the host of the parent page URL —
// CDN-hosted images are explicitly permitted by the Google Image Sitemap specification.
// loc is the parent page URL: the error for an image without a Loc names it, there being
// no URL of the image to name.
func (s *S) validateAndFilterImages(loc string, images []Image) ([]Image, []error) {
	if len(images) == 0 {
		return images, nil
	}
	valid := images[:0:0]
	var errs []error
	for _, img := range images {
		if img.Loc == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: image <loc> is empty")})
			}
			continue
		}
		if err := extensionURLError(img.Loc, s.cfg.strict); err != nil {
			errs = append(errs, err)
			continue
		}
		if s.cfg.strict {
			errs = append(errs, secondaryURLErrors(img.License)...)
		}
		valid = append(valid, img)
	}
	return valid, errs
}

// validateNews validates the news entry on a parsed URL in strict mode and returns
// the entry along with any validation errors.
//
// In tolerant mode the entry is returned unchanged with no errors.
// In strict mode all four fields required by the Google News Sitemap specification
// must be present: Publication.Name, Publication.Language, PublicationDate, and Title.
// Missing required fields are each reported as a separate *ValidationError; the News entry itself
// is kept so that callers still have access to any data that was successfully parsed.
// A nil input is a no-op and returns nil, nil.
// loc is the parent page URL used as context in the returned *ValidationError values.
// dateText is the content of the publication date element as it was read, nil if there is
// no such element. It tells why PublicationDate is nil when it is: the element is absent, it
// is empty, or what it holds could not be parsed. The last has been reported already, so the
// date is not reported as missing on top of it.
func (s *S) validateNews(loc string, news *News, dateText *string) (*News, []error) {
	if news == nil {
		return nil, nil
	}
	if !s.cfg.strict {
		return news, nil
	}
	var errs []error
	if news.Title == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <title> is empty")})
	}
	if news.Publication.Name == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication><name> is empty")})
	}
	if news.Publication.Language == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication><language> is empty")})
	}
	if news.PublicationDate == nil {
		switch {
		case dateText == nil:
			errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication_date> is missing")})
		case emptyElement(dateText):
			errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: news <publication_date> is empty")})
		}
	}
	return news, errs
}

// validateAndFilterVideos validates the video entries on a parsed URL and returns
// the filtered slice of valid videos along with any validation errors.
//
// ThumbnailLoc is treated as the primary key: videos with an empty ThumbnailLoc
// are silently dropped in tolerant mode or produce an error in strict mode.
// In both modes, a ThumbnailLoc exceeding maxLocLength is rejected. In strict mode,
// ThumbnailLoc must additionally be an absolute HTTP or HTTPS URL, see extensionURLError.
//
// For videos that pass the ThumbnailLoc check, strict mode also validates the
// remaining required fields (Title, Description, at least one of ContentLoc or
// PlayerLoc), the optional numeric fields (Duration range 1–28800, Rating range
// 0.0–5.0, ViewCount not negative, Tags count ≤ 32) and the other URLs of the video
// (ContentLoc, PlayerLoc, Uploader.Info), see validateVideoFieldsStrict. These failures
// record errors but keep the video entry.
// loc is the parent page URL, which the errors that are not about a URL name.
func (s *S) validateAndFilterVideos(loc string, videos []Video) ([]Video, []error) {
	if len(videos) == 0 {
		return videos, nil
	}
	valid := videos[:0:0]
	var errs []error
	for _, v := range videos {
		if v.ThumbnailLoc == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: video <thumbnail_loc> is empty")})
			}
			continue
		}
		if err := extensionURLError(v.ThumbnailLoc, s.cfg.strict); err != nil {
			errs = append(errs, err)
			continue
		}
		if s.cfg.strict {
			errs = append(errs, validateVideoFieldsStrict(loc, v)...)
		}
		valid = append(valid, v)
	}
	return valid, errs
}

// validateVideoFieldsStrict checks non-fatal strict-mode constraints on a video entry.
// A field that is missing or out of range is no URL that is rejected, so its error names loc,
// the page the video is on, as the errors of a news entry do. The other URLs of the video
// have to be URLs where they are given, see secondaryURLErrors; the error of one names it.
func validateVideoFieldsStrict(loc string, v Video) []error {
	var errs []error
	if v.Title == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: video <title> is empty")})
	}
	if v.Description == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: video <description> is empty")})
	}
	if v.ContentLoc == "" && v.PlayerLoc == "" {
		errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: video must have at least one of <content_loc> or <player_loc>")})
	}
	if v.Duration != nil && (*v.Duration < 1 || *v.Duration > maxVideoDuration) {
		errs = append(errs, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: video <duration> %d is out of range [1, %d]", *v.Duration, maxVideoDuration)})
	}
	if v.Rating != nil && (*v.Rating < 0.0 || *v.Rating > maxVideoRating) {
		errs = append(errs, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: video <rating> %g is out of range [0.0, %g]", *v.Rating, maxVideoRating)})
	}
	if v.ViewCount != nil && *v.ViewCount < 0 {
		errs = append(errs, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: video <view_count> %d is negative", *v.ViewCount)})
	}
	if len(v.Tags) > maxVideoTags {
		errs = append(errs, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: video has %d tags, maximum is %d", len(v.Tags), maxVideoTags)})
	}
	var uploaderInfo string
	if v.Uploader != nil {
		uploaderInfo = v.Uploader.Info
	}
	return append(errs, secondaryURLErrors(v.ContentLoc, v.PlayerLoc, uploaderInfo)...)
}

// validateAndFilterHreflangs validates the alternate link (hreflang) entries on a parsed URL
// and returns the filtered slice of valid links along with any validation errors.
//
// In tolerant mode, links with an empty Href are silently dropped. In strict mode,
// an empty Href is an error. In both modes, an Href exceeding maxLocLength characters
// is rejected. In strict mode, Href must additionally be an absolute HTTP or HTTPS URL,
// see validateAbsoluteURL, Rel must be "alternate" and Hreflang must not be empty.
// loc is the parent page URL: the error for a link without an Href names it, there being
// no URL of the link to name.
func (s *S) validateAndFilterHreflangs(loc string, links []AlternateLink) ([]AlternateLink, []error) {
	if len(links) == 0 {
		return links, nil
	}
	valid := links[:0:0]
	var errs []error
	for _, link := range links {
		if link.Href == "" {
			if s.cfg.strict {
				errs = append(errs, &ValidationError{URL: loc, Err: errors.New("strict mode: alternate link <href> is empty")})
			}
			continue
		}
		if len(link.Href) > maxLocLength {
			errs = append(errs, &ValidationError{URL: link.Href, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(link.Href))})
			continue
		}
		if s.cfg.strict {
			if link.Rel != "alternate" {
				errs = append(errs, &ValidationError{URL: link.Href, Err: fmt.Errorf("strict mode: alternate link <rel> must be \"alternate\", got %q", link.Rel)})
				continue
			}
			if link.Hreflang == "" {
				errs = append(errs, &ValidationError{URL: link.Href, Err: errors.New("strict mode: alternate link <hreflang> is empty")})
				continue
			}
			if err := validateAbsoluteURL(link.Href); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		valid = append(valid, link)
	}
	return valid, errs
}

// resolveAndValidateLoc resolves and validates a <loc> URL found in a sitemap.
// In both modes, an empty loc is rejected: it is a location that is missing, not a relative
// URL, and resolved as one it would name the sitemap itself. The error names that sitemap,
// there being no location to name.
// In both modes, URLs must not exceed 2048 characters (sitemaps.org specification) and have to
// name a host: a page or a sitemap is on one, and a URL without one cannot be requested.
// In tolerant mode (strict=false), relative URLs are resolved against baseURL, and a space or
// a character that is not in ASCII is percent-encoded before the length check, see
// percentEncode. A reference that begins with "//" names its host itself, see namesNoHost. An
// internationalised host name is returned in ASCII, see asciiHost. What is returned is a URL
// neturl.Parse accepts: a location that does not resolve to one is rejected.
// In strict mode (strict=true), URLs must additionally be absolute HTTP(S) URLs without a
// space, a control character or another character in them that a URL may not hold, and
// without a percent sign that begins no percent-encoding, see strictURLError, on the same host
// and protocol as baseURL, see sameHost. The URL is returned as the document gives it.
// baseURL is the URL the sitemap was served from: the location of the sitemap, whichever URL
// the request for it began at.
// Returns the resolved URL string and an error if validation fails.
func (s *S) resolveAndValidateLoc(loc string, baseURL string) (string, error) {
	return s.resolveAndValidate(loc, baseURL, true)
}

// resolveAndValidate resolves and validates a URL found in the document served from baseURL.
// It does what resolveAndValidateLoc is documented to do. onBaseHost tells whether the URL has
// to be on the host and protocol of baseURL in strict mode: the URLs listed in a sitemap have
// to, the sitemaps named in a robots.txt do not.
func (s *S) resolveAndValidate(loc string, baseURL string, onBaseHost bool) (string, error) {
	if loc == "" {
		return loc, &ValidationError{URL: baseURL, Err: errors.New("<loc> of an entry is empty or missing")}
	}

	base, err := neturl.Parse(baseURL)
	if err != nil {
		return loc, &ValidationError{URL: baseURL, Err: err}
	}

	parsed, err := neturl.Parse(loc)
	if err != nil {
		return loc, &ValidationError{URL: loc, Err: err}
	}

	if s.cfg.strict {
		if err := strictURLError(loc, parsed); err != nil {
			return loc, &ValidationError{URL: loc, Err: err}
		}
		if onBaseHost && parsed.Scheme != base.Scheme {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: scheme %q does not match sitemap scheme %q", parsed.Scheme, base.Scheme)}
		}
		if onBaseHost && !sameHost(parsed, base) {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("strict mode: host %q does not match sitemap host %q", parsed.Host, base.Host)}
		}
		if len(loc) > maxLocLength {
			return loc, &ValidationError{URL: loc, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(loc))}
		}
		return loc, nil
	}

	// Tolerant mode: resolve relative URLs against the base
	resolved, err := tolerantURL(loc, parsed, base)
	if err != nil {
		return loc, err
	}
	return resolved, nil
}

// tolerantURL returns the URL tolerant mode makes of loc, a URL found in the document served
// from base: loc resolved against base, with an internationalised host name in ASCII, see
// asciiHost, and with what a URL must not hold as it is percent-encoded, see percentEncode. It
// returns a *ValidationError for a loc that does not resolve to an HTTP or HTTPS URL which
// names a host, is of at most maxLocLength characters and is one neturl.Parse accepts. parsed
// is loc as neturl.Parse reads it.
func tolerantURL(loc string, parsed, base *neturl.URL) (string, error) {
	resolved := base.ResolveReference(parsed)
	resolved.Host = asciiHost(resolved)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", &ValidationError{URL: resolved.String(), Err: fmt.Errorf("unsupported scheme %q", resolved.Scheme)}
	}
	if resolved.Hostname() == "" || namesNoHost(loc, parsed) {
		return "", &ValidationError{URL: loc, Err: errors.New("missing host")}
	}
	// A space is no part of a URL, and neither is a character that is not in ASCII: a request
	// for a URL with one of them in it is malformed. Those of the path and the fragment are
	// percent-encoded already, as is a control character, which only the fragment may hold;
	// this takes care of the rest, the ones of the query.
	resolvedStr := percentEncode(resolved.String())
	if len(resolvedStr) > maxLocLength {
		return "", &ValidationError{URL: resolvedStr, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(resolvedStr))}
	}
	// neturl.Parse checks the host of a URL only when the URL names its scheme, so a reference
	// such as "//::" is read without complaint and resolves to "https://::", which is no URL.
	// Hence no URL is returned that has not been parsed the way it is returned: loc has been
	// above, whatever differs from it is here.
	if resolvedStr != loc {
		if _, err := neturl.Parse(resolvedStr); err != nil {
			return "", &ValidationError{URL: loc, Err: err}
		}
	}
	return resolvedStr, nil
}

// tolerantExtensionURL returns rawURL, a URL of an extension, the way tolerant mode returns
// it. A URL with nothing in it that percentEncode encodes is returned as it is, which nearly
// every URL is. An absolute HTTP or HTTPS URL that holds a space, a control character or a
// character that is not in ASCII is returned the way the location of a page is, see
// tolerantURL: with these percent-encoded and with an internationalised host name in ASCII.
// Whatever is not such a URL is returned as it is as well: a relative URL, a URL of another
// scheme, one that names no host, one that cannot be parsed, and one that would be longer
// than maxLocLength characters percent-encoded. Tolerant mode drops no image, video or
// alternate link over the form of its URL, so there is nothing else to do about these.
func tolerantExtensionURL(rawURL string) string {
	if encodableBytes(rawURL) == 0 {
		return rawURL
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// The URL is resolved against nothing. An absolute URL resolves to itself whatever it is
	// resolved against; a relative one is left without a scheme that way, and is no URL
	// tolerantURL returns.
	if encoded, err := tolerantURL(rawURL, parsed, &neturl.URL{}); err == nil {
		return encoded
	}
	return rawURL
}

// encodeURLs brings each of the given URLs of an extension into the form tolerant mode
// returns it in, see tolerantExtensionURL.
func encodeURLs(urls ...*string) {
	for _, u := range urls {
		*u = tolerantExtensionURL(*u)
	}
}

// encodeExtensionURLs brings the URLs of the extensions of u into the form tolerant mode
// returns them in: those of its images, of its videos and of its alternate links.
func (u *URL) encodeExtensionURLs() {
	for i := range u.Images {
		encodeURLs(&u.Images[i].Loc, &u.Images[i].License)
	}
	for i := range u.Videos {
		video := &u.Videos[i]
		encodeURLs(&video.ThumbnailLoc, &video.ContentLoc, &video.PlayerLoc)
		if video.Uploader != nil {
			encodeURLs(&video.Uploader.Info)
		}
	}
	for i := range u.Hreflangs {
		encodeURLs(&u.Hreflangs[i].Href)
	}
}

// percentEncode returns s with what a URL must not hold as it is percent-encoded: a space as
// "%20", a control character such as a tab as "%09", and a character that is not in ASCII as
// the bytes it is in UTF-8, "é" as "%C3%A9". A string without any of them is returned as it
// is.
func percentEncode(s string) string {
	const upperhex = "0123456789ABCDEF"

	n := encodableBytes(s)
	if n == 0 {
		return s
	}
	var encoded strings.Builder
	encoded.Grow(len(s) + 2*n)
	for i := 0; i < len(s); i++ {
		if c := s[i]; needsEncoding(c) {
			encoded.WriteByte('%')
			encoded.WriteByte(upperhex[c>>4])
			encoded.WriteByte(upperhex[c&15])
		} else {
			encoded.WriteByte(c)
		}
	}
	return encoded.String()
}

// encodableBytes returns the number of bytes of s that percentEncode encodes.
func encodableBytes(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if needsEncoding(s[i]) {
			n++
		}
	}
	return n
}

// needsEncoding tells whether c is a byte percentEncode encodes: a space, a control character,
// or a byte that is not ASCII.
func needsEncoding(c byte) bool {
	return c == ' ' || isControl(rune(c)) || c >= utf8.RuneSelf
}

// namesNoHost tells whether loc is a reference that begins with "//" and names no host after
// it, such as "//", "//?page=2", "///page" or "//:8080/page". A reference that begins with
// "//" gives the host itself and takes only the scheme from the URL it is resolved against, so
// one that gives none has none. neturl resolves the first three to the host of the base URL
// nevertheless, "//" to the base URL itself. parsed is loc as neturl.Parse reads it.
func namesNoHost(loc string, parsed *neturl.URL) bool {
	return strings.HasPrefix(loc, "//") && parsed.Hostname() == ""
}

// excludedFromURLs holds the printable ASCII characters that RFC 3986 gives no place in a URL:
// a URL may not hold one anywhere, and has to give it percent-encoded, "|" as "%7C". The
// square brackets are not among them, as they have a place, around an IPv6 address; they are
// excluded everywhere else, see misplacedBracket.
const excludedFromURLs = "\"<>\\^`{|}"

// strictURLError tells what keeps rawURL from being a URL strict mode accepts: an absolute
// HTTP or HTTPS URL that names a host and has neither a space, nor a control character, nor a
// character of excludedFromURLs in it, nor a square bracket where a URL may not hold one, see
// misplacedBracket. A URL has to give these percent-encoded, a space as "%20", so a percent
// sign in it has to begin such an encoding, see escapeError. A port is no host:
// "https://:8080/" names none. A character that is not in ASCII is accepted, as it is in an
// IRI (RFC 3987). parsed is rawURL as neturl.Parse reads it. It returns nil for a URL that is
// one.
func strictURLError(rawURL string, parsed *neturl.URL) error {
	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return fmt.Errorf("strict mode: unsupported scheme %q", parsed.Scheme)
	case parsed.Hostname() == "":
		return errors.New("strict mode: missing host")
	case strings.Contains(rawURL, " "):
		return errors.New("strict mode: URL contains a space")
	case strings.ContainsFunc(rawURL, isControl):
		// neturl.Parse turns down a control character itself, except in the fragment.
		return errors.New("strict mode: URL contains a control character")
	}
	// neturl.Parse turns down these in the user information and some of them in the name of
	// the host, and none of them in the path, in the query and in the fragment.
	i := strings.IndexAny(rawURL, excludedFromURLs)
	if bracket := misplacedBracket(rawURL, parsed); bracket >= 0 && (i < 0 || bracket < i) {
		i = bracket
	}
	if i >= 0 {
		return fmt.Errorf("strict mode: URL contains the character '%c', which it has to give as \"%%%02X\"", rawURL[i], rawURL[i])
	}
	// neturl.Parse turns down a percent sign that begins no percent-encoding everywhere but in
	// the query, which it does not read.
	if err := escapeError(parsed.RawQuery); err != nil {
		return fmt.Errorf("strict mode: %w in the query", err)
	}
	return nil
}

// misplacedBracket returns the index of the first square bracket of rawURL that stands where a
// URL may not hold one, and -1 if rawURL has none. RFC 3986 gives the square brackets one
// place, around an IPv6 address that is the host: "https://[2001:db8::1]/page". Everywhere else
// a URL has to give them percent-encoded, as "%5B" and "%5D", which goes for a query such as
// "?id[]=1" as well. parsed is rawURL as neturl.Parse reads it, a URL that names a host.
func misplacedBracket(rawURL string, parsed *neturl.URL) int {
	after := 0
	if strings.HasPrefix(parsed.Host, "[") {
		// neturl.Parse reads a host that begins with a bracket as an IPv6 address in a pair of
		// brackets, and turns down a bracket before the host. That pair is the one a URL may
		// hold, so every bracket after the first "]" of rawURL is one too many.
		after = strings.IndexByte(rawURL, ']') + 1
	}
	if i := strings.IndexAny(rawURL[after:], "[]"); i >= 0 {
		return after + i
	}
	return -1
}

// escapeError returns the error neturl.Parse has for a percent sign that begins no
// percent-encoding, for the first such sign of s: one that two hexadecimal digits do not
// follow, as in "100%" and in "%zz". It returns nil if s has none.
func escapeError(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && (i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2])) {
			return neturl.EscapeError(s[i:min(i+3, len(s))])
		}
	}
	return nil
}

// isHex tells whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// isControl tells whether r is an ASCII control character, which a tab and a line break are.
func isControl(r rune) bool {
	return r < ' ' || r == 0x7f
}

// validateAbsoluteURL validates a URL of an extension in strict mode: the location of an
// image, the thumbnail of a video, the target of an alternate link, and the other URLs of an
// image and of a video, see secondaryURLErrors. It has to be a URL strictURLError has nothing
// against; unlike the location of the page, it may be on any host.
// It returns a *ValidationError for rawURL, or nil.
func validateAbsoluteURL(rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	if err == nil {
		err = strictURLError(rawURL, parsed)
	}
	if err != nil {
		return &ValidationError{URL: rawURL, Err: err}
	}
	return nil
}

// extensionURLError tells what keeps rawURL from being a URL of an image or of a video that is
// accepted. In both modes it must not exceed maxLocLength characters; when strict is set it
// has to be an absolute HTTP or HTTPS URL as well, see validateAbsoluteURL. It returns a
// *ValidationError for rawURL, or nil.
func extensionURLError(rawURL string, strict bool) error {
	if len(rawURL) > maxLocLength {
		return &ValidationError{URL: rawURL, Err: fmt.Errorf("URL exceeds maximum length of %d characters (%d)", maxLocLength, len(rawURL))}
	}
	if strict {
		return validateAbsoluteURL(rawURL)
	}
	return nil
}

// secondaryURLErrors validates, in strict mode, the URLs an image or a video gives besides the
// one it is known by: the licence of an image, the content, the player and the page about the
// uploader of a video. None of them has to be there, and one that is not is not validated.
// One that is has to be what the URL the entry is known by has to be in strict mode: an
// absolute HTTP or HTTPS URL of at most maxLocLength characters, see extensionURLError.
// It returns a *ValidationError for each URL that is not one. Such a URL costs the entry
// nothing: the entry is returned with it, as the document gives it.
func secondaryURLErrors(urls ...string) []error {
	var errs []error
	for _, u := range urls {
		if u == "" {
			continue
		}
		if err := extensionURLError(u, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// defaultPorts holds the port a protocol is served on when a URL names none.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// sameHost tells whether a and b, two URLs of the same protocol, are on the same host: whether
// they name the same host and the same port. Host names are compared without regard to letter
// case, and a URL without a port is on the default port of its protocol. So
// "https://Example.com:443/" is on the host of "https://example.com/", while
// "https://example.com:8443/" and "https://www.example.com/" are not.
// An internationalised domain name is one host however it is written: its names are compared
// in ASCII, see asciiHostname, so "https://bücher.example/" is on the host of
// "https://xn--bcher-kva.example/".
func sameHost(a, b *neturl.URL) bool {
	return strings.EqualFold(asciiHostname(a.Hostname()), asciiHostname(b.Hostname())) && portOf(a) == portOf(b)
}

// portOf returns the port u is on: the one it names, or the default port of its protocol.
func portOf(u *neturl.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return defaultPorts[u.Scheme]
}

// asciiHost returns the host of u, port included, with the name of the host in ASCII, see
// asciiHostname. neturl writes a name that is not in ASCII percent-encoded
// ("b%C3%BCcher.example"), which is neither the name the document gives nor the one a request
// is sent to.
func asciiHost(u *neturl.URL) string {
	// Nearly every host is in ASCII, and there is nothing to do for it. An IP address in
	// brackets is no domain name, whatever stands between the brackets.
	if isASCII(u.Host) || strings.HasPrefix(u.Host, "[") {
		return u.Host
	}
	// What follows the name is the port, if the URL names one.
	name := u.Hostname()
	return asciiHostname(name) + strings.TrimPrefix(u.Host, name)
}

// asciiHostname returns the name of a host in ASCII. A name that is in ASCII already is
// returned as it is. An internationalised domain name is returned in its Punycode form, the
// name it is looked up by (RFC 5891): "bücher.example" is "xn--bcher-kva.example". It is
// converted the way net/http converts the host of a URL it requests, so the name is the one a
// request for the URL goes to. That maps the name before it encodes it, capitals to small
// letters for one: "BÜCHER.example" is "xn--bcher-kva.example" too.
// A name that is neither, for holding what a domain name must not, is returned as it is. So is
// one with the replacement character in it, which is what bytes that are not UTF-8 are read
// as and what they leave behind where a document was decoded: it is no part of a name,
// though the tables of some Unicode versions let it pass.
func asciiHostname(name string) string {
	if isASCII(name) || strings.ContainsRune(name, utf8.RuneError) {
		return name
	}
	if ascii, err := idna.Lookup.ToASCII(name); err == nil && ascii != "" {
		return ascii
	}
	return name
}

// isASCII tells whether s holds nothing but ASCII characters.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// gzipPrefix is what gzip content begins with, and every member of it: the two bytes that
// identify gzip, and the one that names deflate, the only compression method there is.
const gzipPrefix = "\x1f\x8b\x08"

// gzipMembers reads the members of gzip content one after the other, as one stream.
//
// A gzip file is a series of members, each of them compressed on its own (RFC 1952, section
// 2.2): what "cat a.gz b.gz" writes holds two of them, and so does a file that was appended
// to. The content of the file is that of its members together.
//
// gzip.Reader reads a series of members by itself, but fails on whatever follows the last
// one, and servers send a newline or padding after it often enough. The members are therefore
// read one at a time here: what follows a member is taken for another member if it begins the
// way gzip content does, and is left unread otherwise, the way gzip(1) ignores trailing
// garbage.
type gzipMembers struct {
	content string
	// src reads content. It is an io.ByteReader, so member reads from it no further than its
	// member goes, and what src has left is what follows the member.
	src    *strings.Reader
	member *gzip.Reader
}

// newGzipMembers returns a reader of the members of content. It fails if content does not
// begin with the header of a member.
func newGzipMembers(content string) (*gzipMembers, error) {
	src := strings.NewReader(content)
	member, err := gzip.NewReader(src)
	if err != nil {
		return nil, err
	}
	member.Multistream(false)
	return &gzipMembers{content: content, src: src, member: member}, nil
}

// Read reads from the current member, and from the next one once that is read to its end. It
// returns io.EOF when the last member is read. A member that is cut short or damaged fails
// the read, whichever member it is.
func (m *gzipMembers) Read(p []byte) (int, error) {
	for {
		n, err := m.member.Read(p)
		if err != io.EOF {
			return n, err
		}
		if !strings.HasPrefix(m.content[len(m.content)-m.src.Len():], gzipPrefix) {
			return n, io.EOF
		}
		if err := m.member.Reset(m.src); err != nil {
			return n, err
		}
		m.member.Multistream(false)
		if n > 0 {
			return n, nil
		}
	}
}

// Close closes the reader of the members. It does not have to be read to its end for that.
func (m *gzipMembers) Close() error {
	return m.member.Close()
}

// maxDeflateRatio is how many times its own size compressed content can be decompressed to at
// most: deflate, the compression method of gzip, writes up to 258 bytes with two bits.
const maxDeflateRatio = 1032

// gzipSize returns the size gzip content says its uncompressed content has, or -1 if it says
// none that can be true. A member ends with the size of what it holds, modulo 2^32 (RFC 1952,
// section 2.3.1, ISIZE), so the last four bytes of the content are the size of what its last
// member holds.
//
// That is the size of the content if the content is a single member of less than 4 GB with
// nothing after it, which is what a compressed sitemap is as a rule. It is a size to read the
// content with and nothing more, see readAtMost: content of several members is longer, so is
// content of 4 GB and more, and bytes that follow the last member are no size at all.
// A size that content of this length cannot be decompressed to is not returned, so that the
// memory allocated on the word of the content is never more than content of its length could
// take up anyway.
func gzipSize(content string) int64 {
	if len(content) < 4 {
		return -1
	}
	trailer := content[len(content)-4:]
	size := int64(trailer[0]) | int64(trailer[1])<<8 | int64(trailer[2])<<16 | int64(trailer[3])<<24
	if size > int64(len(content))*maxDeflateRatio {
		return -1
	}
	return size
}

// unzip decompresses the given gzip content: all of its members, see gzipMembers.
// It returns the uncompressed content, or the error that kept the content from being
// decompressed. No content is returned together with an error.
// At most maxSize bytes are decompressed, for all members together. If the payload is larger,
// decompression stops and an error that wraps ErrMaxResponseSize is returned, so a small
// compressed input cannot exhaust memory (decompression bomb).
// The size the content says it has is what it is read with, see gzipSize and readAtMost.
func unzip(content string, maxSize int64) (string, error) {
	members, err := newGzipMembers(content)
	if err != nil {
		return "", fmt.Errorf("gzip decompression failed: %w", err)
	}
	defer func() {
		_ = members.Close()
	}()

	uncompressed, exceeded, err := readAtMost(members, maxSize, gzipSize(content))
	if err != nil {
		return "", fmt.Errorf("gzip decompression failed: %w", err)
	}

	if exceeded {
		return "", limitErrorf(ErrMaxResponseSize, "decompressed size exceeds limit of %d bytes", maxSize)
	}

	return uncompressed, nil
}

// lastModFormats are the date and time layouts accepted in <lastmod> and in the date elements
// of the extensions.
var lastModFormats = []string{
	"2006",
	"2006-01",
	"2006-01-02",
	"2006-01-02T15:04-07:00",
	"2006-01-02T15:04Z",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999Z",
	time.RFC3339,
	time.RFC3339Nano,
}

// rfc822Layouts are the layouts of a date and time of RFC 822 whose time zone is given as an
// offset: the day of the week and the seconds are optional, the day of the month has one
// digit or two, and the year two digits or four, which RSS 2.0 adds to the RFC.
var rfc822Layouts = []string{
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 06 15:04:05 -0700",
	"Mon, 2 Jan 06 15:04 -0700",
	"2 Jan 06 15:04:05 -0700",
	"2 Jan 06 15:04 -0700",
}

// rfc822Zones are the offsets of the time zones a date and time of RFC 822 may name: the ones
// the RFC defines, and "UTC", which it does not, but which stands for nothing else.
//
// Of the military zones, the single letters of the RFC, only "Z" is among them. The RFC
// defined the others with the wrong sign, so nothing tells which way a document means them.
var rfc822Zones = map[string]string{
	"UT":  "+0000",
	"GMT": "+0000",
	"UTC": "+0000",
	"Z":   "+0000",
	"EST": "-0500",
	"EDT": "-0400",
	"CST": "-0600",
	"CDT": "-0500",
	"MST": "-0700",
	"MDT": "-0600",
	"PST": "-0800",
	"PDT": "-0700",
}

// parseRFC822Time parses a date and time in the format of RFC 822, which is what the <pubDate>
// of an RSS item holds: "Mon, 15 Jan 2024 10:30:00 GMT". The whitespace around the text is
// ignored, and so is how much of it stands between its parts. A year of two digits is one of
// the years 1969 to 2068.
//
// A time zone that is given by its name is replaced by its offset before the text is parsed.
// The time package would take the name for a zone of the local time zone of the process, and
// for UTC if there is none of that name: "CST" would be eight hours ahead of UTC on a machine
// in China, and UTC itself on one in Europe. A name that is not in rfc822Zones is not a date
// that can be told, and fails like any other text that is none.
func parseRFC822Time(text string) (LastModTime, error) {
	fields := strings.Fields(text)
	// The name of a zone is written in the letters of ASCII, in whichever case. Other letters
	// are not put in upper case: "ſ", the long s, would come out as the S of "EST".
	if last := len(fields) - 1; last >= 0 && isASCII(fields[last]) {
		if offset, named := rfc822Zones[strings.ToUpper(fields[last])]; named {
			fields[last] = offset
		}
	}
	normalized := strings.Join(fields, " ")
	for _, layout := range rfc822Layouts {
		if parsedTime, err := time.Parse(layout, normalized); err == nil {
			return LastModTime{parsedTime}, nil
		}
	}

	return LastModTime{}, fmt.Errorf("unsupported date format of RFC 822: %q", strings.TrimSpace(text))
}

// UnmarshalXML implements xml.Unmarshaler, for code that decodes sitemap XML with encoding/xml
// itself. It accepts a date or a date and time in one of the W3C formats the sitemaps.org
// protocol allows, and fails for anything else.
//
// An element that is empty, or holds only whitespace, is not an error and leaves l as it is.
// Note that encoding/xml allocates a pointer field for every element it meets, so a
// *LastModTime field decoded this way is not nil for an empty element: it holds the zero
// time, which IsZero tells. Parse and ParseContext do not read dates this way; in the URLs
// they collect, the field of an empty element is nil.
func (l *LastModTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	err := d.DecodeElement(&v, &start)
	if err != nil {
		return err
	}

	// An empty <lastmod> element (or one containing only whitespace) is common
	// in real-world sitemaps. It is "not set" rather than an error.
	if strings.TrimSpace(v) == "" {
		return nil
	}

	parsed, err := parseLastModTime(v)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// parseLastModTime parses a date or a date and time in one of lastModFormats, ignoring
// surrounding whitespace. Empty text is no date and fails like any other text that is none:
// what an empty element stands for is up to the caller.
func parseLastModTime(text string) (LastModTime, error) {
	text = strings.TrimSpace(text)
	for _, format := range lastModFormats {
		if parsedTime, err := time.Parse(format, text); err == nil {
			return LastModTime{parsedTime}, nil
		}
	}

	return LastModTime{}, fmt.Errorf("unsupported lastmod format: %q", text)
}

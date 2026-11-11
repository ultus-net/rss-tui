// Package opml reads and writes the subscription list format that feed readers
// use to move subscriptions between applications.
package opml

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Feed is one subscription from an OPML document.
type Feed struct {
	Title    string
	XMLURL   string
	HTMLURL  string
	Category string
}

type document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    head     `xml:"head"`
	Body    body     `xml:"body"`
}

type head struct {
	Title string `xml:"title"`
}

type body struct {
	Outlines []outline `xml:"outline"`
}

type outline struct {
	Text      string    `xml:"text,attr,omitempty"`
	Title     string    `xml:"title,attr,omitempty"`
	Type      string    `xml:"type,attr,omitempty"`
	XMLURL    string    `xml:"xmlUrl,attr,omitempty"`
	XMLURLLC  string    `xml:"xmlurl,attr,omitempty"`
	HTMLURL   string    `xml:"htmlUrl,attr,omitempty"`
	HTMLURLLC string    `xml:"htmlurl,attr,omitempty"`
	Outlines  []outline `xml:"outline,omitempty"`
}

// Parse reads an OPML document and returns its feeds in document order. A
// grouping outline with no xmlUrl contributes its text as the category of its
// children. Outlines without a feed URL are skipped.
func Parse(r io.Reader) ([]Feed, error) {
	var doc document
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("opml: parse: %w", err)
	}
	var out []Feed
	seen := make(map[string]bool)
	var walk func(outlines []outline, category string)
	walk = func(outlines []outline, category string) {
		for _, o := range outlines {
			xmlURL := strings.TrimSpace(firstNonEmpty(o.XMLURL, o.XMLURLLC))
			if xmlURL == "" {
				label := strings.TrimSpace(firstNonEmpty(o.Text, o.Title))
				if label == "" {
					label = category
				}
				walk(o.Outlines, label)
				continue
			}
			if seen[xmlURL] {
				continue
			}
			seen[xmlURL] = true
			out = append(out, Feed{
				Title:    strings.TrimSpace(firstNonEmpty(o.Title, o.Text)),
				XMLURL:   xmlURL,
				HTMLURL:  strings.TrimSpace(firstNonEmpty(o.HTMLURL, o.HTMLURLLC)),
				Category: strings.TrimSpace(category),
			})
		}
	}
	walk(doc.Body.Outlines, "")
	return out, nil
}

// Export writes feeds as OPML 2.0, grouped one level deep by category. Feeds
// that share no category are written flat.
func Export(w io.Writer, feeds []Feed) error {
	grouped := make([]outline, 0, len(feeds))
	byCategory := make(map[string]int)
	for _, f := range feeds {
		o := outline{
			Type:    "rss",
			XMLURL:  f.XMLURL,
			HTMLURL: f.HTMLURL,
			Text:    f.Title,
			Title:   f.Title,
		}
		if f.Category == "" {
			grouped = append(grouped, o)
			continue
		}
		idx, ok := byCategory[f.Category]
		if !ok {
			byCategory[f.Category] = len(grouped)
			grouped = append(grouped, outline{Text: f.Category, Title: f.Category})
			idx = len(grouped) - 1
		}
		grouped[idx].Outlines = append(grouped[idx].Outlines, o)
	}
	doc := document{
		Version: "2.0",
		Head:    head{Title: "rss-tui subscriptions"},
		Body:    body{Outlines: grouped},
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("opml: encode: %w", err)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("opml: write header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("opml: write body: %w", err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("opml: write trailer: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

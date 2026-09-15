package feed

import (
	"sort"
	"strings"
	"time"

	"github.com/gorilla/feeds"

	"github.com/xsaveopt/twitter-rss/internal/config"
	"github.com/xsaveopt/twitter-rss/internal/nitter"
)

type Builder struct {
	rewriteLinks bool
}

func NewBuilder(cfg config.Config) *Builder {
	return &Builder{rewriteLinks: cfg.RewriteLinks}
}

func (b *Builder) Single(f *nitter.Feed) (string, error) {
	link := b.rewrite(f.Origins, f.Link)
	if link == "" {
		link = "https://x.com/" + f.Handle
	}

	out := &feeds.Feed{
		Title:       "@" + f.Handle + " on Twitter",
		Link:        &feeds.Link{Href: link},
		Description: titleOrDefault(f),
		Author:      &feeds.Author{Name: "@" + f.Handle},
		Created:     f.Fetched,
	}
	out.Items = b.items(f)
	return out.ToRss()
}

func (b *Builder) Combined(authorName string, list []*nitter.Feed) (string, error) {
	out := &feeds.Feed{
		Title:       "Tracked Twitter accounts",
		Link:        &feeds.Link{Href: "https://x.com"},
		Description: "Combined tweets from tracked accounts",
		Author:      &feeds.Author{Name: authorName},
		Created:     time.Now(),
	}
	for _, f := range list {
		out.Items = append(out.Items, b.items(f)...)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		return out.Items[i].Created.After(out.Items[j].Created)
	})
	return out.ToRss()
}

func (b *Builder) items(f *nitter.Feed) []*feeds.Item {
	handle := f.Handle
	items := make([]*feeds.Item, 0, len(f.Items))
	for _, it := range f.Items {
		created := time.Now()
		if it.PublishedParsed != nil {
			created = *it.PublishedParsed
		}
		link := b.rewrite(f.Origins, it.Link)
		id := it.GUID
		if id == "" {
			id = link
		}
		items = append(items, &feeds.Item{
			Title:       prefixHandle(handle, it.Title),
			Link:        &feeds.Link{Href: link},
			Description: it.Description,
			Author:      &feeds.Author{Name: "@" + handle},
			Id:          id,
			Created:     created,
		})
	}
	return items
}

func (b *Builder) rewrite(origins []string, link string) string {
	if !b.rewriteLinks || link == "" {
		return link
	}
	for _, origin := range origins {
		if rest, ok := strings.CutPrefix(link, origin); ok {
			link = "https://x.com" + rest
			break
		}
	}
	return strings.TrimSuffix(link, "#m")
}

func prefixHandle(handle, title string) string {
	p := "@" + handle + ": "
	if strings.HasPrefix(title, p) {
		return title
	}
	return p + title
}

func titleOrDefault(f *nitter.Feed) string {
	if f.Title != "" {
		return f.Title
	}
	return "Tweets from @" + f.Handle
}

package main

import (
	"sort"
	"strings"
	"time"

	"github.com/gorilla/feeds"
	"github.com/mmcdole/gofeed"
)

type builder struct {
	nitterBase   string
	rewriteLinks bool
}

func newBuilder(cfg Config) *builder {
	return &builder{nitterBase: cfg.NitterBase, rewriteLinks: cfg.RewriteLinks}
}

func (b *builder) Single(f *Feed) (string, error) {
	link := b.rewrite(f.Link)
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
	out.Items = b.items(f.Handle, f.Items)
	return out.ToRss()
}

func (b *builder) Combined(authorName string, feeds_ []*Feed) (string, error) {
	out := &feeds.Feed{
		Title:       "Tracked Twitter accounts",
		Link:        &feeds.Link{Href: "https://x.com"},
		Description: "Combined tweets from tracked accounts",
		Author:      &feeds.Author{Name: authorName},
		Created:     time.Now(),
	}
	for _, f := range feeds_ {
		out.Items = append(out.Items, b.items(f.Handle, f.Items)...)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		return out.Items[i].Created.After(out.Items[j].Created)
	})
	return out.ToRss()
}

func (b *builder) items(handle string, in []*gofeed.Item) []*feeds.Item {
	items := make([]*feeds.Item, 0, len(in))
	for _, it := range in {
		created := time.Now()
		if it.PublishedParsed != nil {
			created = *it.PublishedParsed
		}
		link := b.rewrite(it.Link)
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

func (b *builder) rewrite(link string) string {
	if !b.rewriteLinks || link == "" {
		return link
	}
	if strings.HasPrefix(link, b.nitterBase) {
		link = "https://x.com" + strings.TrimPrefix(link, b.nitterBase)
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

func titleOrDefault(f *Feed) string {
	if f.Title != "" {
		return f.Title
	}
	return "Tweets from @" + f.Handle
}

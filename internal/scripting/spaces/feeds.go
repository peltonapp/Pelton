package spaces

import (
	"context"
	"fmt"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The feed permissions.
const (
	PermFeedsRead   = "feeds.read"
	PermFeedsModify = "feeds.modify"
)

// TriggerFeedItem fires for each new item a feed refresh brings in.
const TriggerFeedItem = "feeds.new_item"

// FeedItem is an entry of a subscribed feed.
type FeedItem struct {
	ID        int64
	FeedID    int64
	Feed      string
	Title     string
	URL       string
	Author    string
	Published time.Time
	Summary   string
	Read      bool
	Starred   bool
}

// FeedFilter narrows a list of feed items.
type FeedFilter struct {
	// Feed matches the feed's title, "" for every feed.
	Feed       string
	UnreadOnly bool
	Query      string
	Limit      int
}

// FeedsHost is the feeds a script reads and marks.
type FeedsHost interface {
	FeedItems(ctx context.Context, f FeedFilter) ([]FeedItem, error)
	FeedItem(ctx context.Context, id int64) (FeedItem, error)
	SetFeedItemsRead(ctx context.Context, ids []int64, read bool) error
	SetFeedItemStarred(ctx context.Context, id int64, starred bool) error
}

// FeedItemObject turns a feed item into the value scripts get.
func FeedItemObject(f FeedItem) scripting.Object {
	return scripting.Object{Type: "feed_item", Fields: map[string]any{
		"id": f.ID, "feed_id": f.FeedID, "feed": f.Feed, "title": f.Title, "url": f.URL,
		"author": f.Author, "published": unix(f.Published), "summary": f.Summary,
		"read": f.Read, "starred": f.Starred,
	}}
}

func sampleFeedItem(now time.Time) map[string]any {
	return FeedItemObject(FeedItem{Feed: "Example News", Title: "Release notes for 2.0", URL: "https://example.org/news/2-0",
		Author: "Example", Published: now.Add(-time.Hour), Summary: "What changed in 2.0."}).Fields
}

// itemIDs reads argument i as one item or a list of items or ids.
func itemIDs(c *scripting.Call, i int) ([]int64, error) {
	if items, ok := c.Arg(i).([]any); ok {
		out := make([]int64, 0, len(items))
		for _, it := range items {
			sub := &scripting.Call{Ctx: c.Ctx, Function: c.Function, Args: []any{it}}
			id, err := idOf(sub, 0, "a feed item")
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, nil
	}
	id, err := idOf(c, i, "a feed item")
	if err != nil {
		return nil, err
	}
	return []int64{id}, nil
}

func feedsEntries(h FeedsHost) scripting.Entries {
	host := func() (FeedsHost, error) {
		if h == nil {
			return nil, unavailable("feeds")
		}
		return h, nil
	}
	itemField := field("item", "feed_item")
	itemParam := param("item", scripting.TypeRef)
	itemParam.ValueType = "feed_item"
	on := param("on", scripting.TypeBool)

	fns := []scripting.Function{
		fnDef("feeds", "items", PermFeedsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			t := c.Table(0)
			f := FeedFilter{Feed: textOf(t, "feed"), Query: textOf(t, "query"), Limit: 50}
			f.UnreadOnly, _ = t["unread"].(bool)
			if n := numberOf(t, "limit"); n > 0 && n <= 500 {
				f.Limit = int(n)
			}
			out, err := hh.FeedItems(c.Ctx, f)
			if err != nil {
				return nil, err
			}
			return []any{list(out, FeedItemObject)}, nil
		}, []scripting.Field{field("items", "list")}, param("filter", scripting.TypeRef)),
		fnDef("feeds", "get", PermFeedsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a feed item")
			if err != nil {
				return nil, err
			}
			it, err := hh.FeedItem(c.Ctx, id)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{FeedItemObject(it)}, nil
		}, []scripting.Field{itemField}, param("id", scripting.TypeNumber)),
		fnDef("feeds", "mark_read", PermFeedsModify, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			ids, err := itemIDs(c, 0)
			if err != nil {
				return nil, err
			}
			if len(ids) > 500 {
				return nil, fmt.Errorf("%s: at most 500 items at a time", c.Function)
			}
			return nil, hh.SetFeedItemsRead(c.Ctx, ids, c.Bool(1, true))
		}, nil, itemParam, on),
		fnDef("feeds", "star", PermFeedsModify, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a feed item")
			if err != nil {
				return nil, err
			}
			return nil, hh.SetFeedItemStarred(c.Ctx, id, c.Bool(1, true))
		}, nil, itemParam, on),
	}

	itemRef := scripting.RefParam("item", "feed_item", "event.item")
	onDefault := withDef(param("on", scripting.TypeBool), true)
	act := func(id, lua string, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "feeds"), Params: params, Permission: PermFeedsModify, Lua: lua}
	}
	feedName := param("feed", scripting.TypeString)
	load := func(c *scripting.PrepareContext, id int64) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		it, err := hh.FeedItem(c.Ctx, id)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load feed item %d: %w", id, err)
		}
		return FeedItemObject(it), nil
	}
	return scripting.Entries{
		Placements: []scripting.Placement{menu("feeds.item", "feeds", "feed_item", PermFeedsRead, intLoad(load))},
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermFeedsRead, "feeds"), Risk: "low"},
			{Meta: meta("perm", PermFeedsModify, "feeds"), Risk: "low"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "feed_item", "feeds"), Sample: sampleFeedItem, Fields: fields(
			"id:number", "feed_id:number", "feed:string", "title:string", "url:string", "author:string",
			"published:number", "summary:text", "read:bool", "starred:bool")}},
		Triggers: []scripting.Trigger{{
			Meta: meta("trigger", TriggerFeedItem, "feeds"), Params: []scripting.Param{feedName},
			Event: []scripting.Field{itemField}, Permission: PermFeedsRead,
			Prepare: prepareObject("item", "item_id", load),
			Match: func(params, event map[string]any) bool {
				it := objectFields(event, "item")
				name, _ := it["feed"].(string)
				return it != nil && matchText(params, "feed", name)
			},
		}},
		Modules: []scripting.Module{{Meta: meta("module", "feeds", "feeds"), Functions: fns}},
		Conditions: []scripting.Condition{
			scripting.TextCondition(meta("cond", "feeds.title", "feeds"), "{{item}}.title", itemRef),
			scripting.TextCondition(meta("cond", "feeds.feed", "feeds"), "{{item}}.feed", itemRef),
			scripting.TextCondition(meta("cond", "feeds.summary", "feeds"), "{{item}}.summary", itemRef),
		},
		Actions: []scripting.Action{
			act("feeds.mark_read", "feeds.mark_read({{item}}, {{on}})", itemRef, onDefault),
			act("feeds.star", "feeds.star({{item}}, {{on}})", itemRef, onDefault),
		},
	}
}

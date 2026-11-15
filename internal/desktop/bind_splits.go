package desktop

import (
	"context"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/storage"
)

// defaultSplitTabs is the tab order before the user changes it.
const defaultSplitTabs = "important,newsletters,notifications"

// splitOther is the catch-all tab, always last.
const splitOther = "other"

// splitViewPrefix marks a saved view used as a tab: view:<id>.
const splitViewPrefix = "view:"

// SplitTabDTO is one tab of the split inbox with its counts. Key is
// important, newsletters, notifications, view:<id> or other.
type SplitTabDTO struct {
	Key    string `json:"key"`
	Total  int    `json:"total"`
	Unread int    `json:"unread"`
}

// splitTabs resolves the stored tab order into rules and their keys. A saved
// view that no longer exists is skipped, and so is a key listed twice.
func (a *App) splitTabs(ctx context.Context) ([]storage.SplitRule, []string) {
	var (
		rules []storage.SplitRule
		keys  []string
		seen  = map[string]bool{}
	)
	for _, key := range strings.Split(a.stringSetting(settingSplitTabs, defaultSplitTabs), ",") {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		switch storage.SplitKind(key) {
		case storage.SplitImportant:
			rules = append(rules, storage.SplitRule{Kind: storage.SplitImportant, Senders: a.vipSenders()})
		case storage.SplitNewsletters, storage.SplitNotifications:
			rules = append(rules, storage.SplitRule{Kind: storage.SplitKind(key)})
		default:
			id, err := strconv.ParseInt(strings.TrimPrefix(key, splitViewPrefix), 10, 64)
			if !strings.HasPrefix(key, splitViewPrefix) || err != nil {
				continue
			}
			v, err := a.store.GetView(ctx, id)
			if err != nil {
				continue
			}
			rules = append(rules, storage.SplitRule{Kind: storage.SplitView, View: *v})
		}
		keys = append(keys, key)
	}
	return rules, keys
}

// splitFilter narrows an inbox query to the tab named key. It is nil when the
// split inbox is off or the key names no tab, which lists the whole inbox.
func (a *App) splitFilter(ctx context.Context, key string) *storage.SplitFilter {
	if key == "" || !a.boolSetting(settingSplitInbox, false) {
		return nil
	}
	rules, keys := a.splitTabs(ctx)
	if key == splitOther {
		return &storage.SplitFilter{Rules: rules, Index: len(rules)}
	}
	for i, k := range keys {
		if k == key {
			return &storage.SplitFilter{Rules: rules, Index: i}
		}
	}
	return nil
}

// isInboxRequest reports whether a list request reads an inbox: the unified
// inbox or one account's inbox folder.
func (a *App) isInboxRequest(ctx context.Context, req ListMessagesRequest) bool {
	switch req.Kind {
	case "view":
		return req.View == viewInbox
	case "folder":
		f, err := a.store.GetFolder(ctx, req.FolderID)
		return err == nil && folderRole(*f) == roleInbox
	}
	return false
}

// SplitCounts returns each split inbox tab with its total and unread counts,
// in tab order with the catch-all last, from one grouped query. It returns no
// tabs for a request that is not an inbox.
func (a *App) SplitCounts(req ListMessagesRequest) ([]SplitTabDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if !a.isInboxRequest(a.ctx, req) {
		return []SplitTabDTO{}, nil
	}
	req.Split = ""
	q, err := a.requestQuery(a.ctx, req)
	if err != nil {
		return nil, err
	}
	rules, keys := a.splitTabs(a.ctx)
	counts, err := a.store.SplitCounts(a.ctx, q, rules)
	if err != nil {
		return nil, err
	}
	out := make([]SplitTabDTO, 0, len(counts))
	for i, c := range counts {
		key := splitOther
		if i < len(keys) {
			key = keys[i]
		}
		out = append(out, SplitTabDTO{Key: key, Total: c.Total, Unread: c.Unread})
	}
	return out, nil
}

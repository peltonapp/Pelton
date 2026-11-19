package desktop

import (
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Vault mail is never in the search index. While a vault is unlocked, search
// reads its newest messages instead, a page at a time, opening them in
// memory and matching the query against them. Nothing of it is written
// anywhere, and at most mailVaultSearchScan messages per search are read, so
// older vault mail is not found.
const (
	mailVaultSearchScan = 5000
	mailVaultSearchPage = 200
)

// unlockedVaultFolders returns the folders of every unlocked vault.
func (a *App) unlockedVaultFolders() []int64 {
	a.mailVault.mu.Lock()
	ids := make([]int64, 0, len(a.mailVault.open))
	for id := range a.mailVault.open {
		ids = append(ids, id)
	}
	a.mailVault.mu.Unlock()
	var folders []int64
	for _, id := range ids {
		f, err := a.store.VaultFolderIDs(a.ctx, id)
		if err != nil {
			continue
		}
		folders = append(folders, f...)
	}
	return folders
}

// searchMailVaults returns up to limit messages of unlocked vaults that the
// request matches, newest first.
func (a *App) searchMailVaults(req SearchRequestDTO, limit int) []storage.Message {
	if req.notesWanted() || limit <= 0 {
		return nil
	}
	var folders []int64
	for _, f := range a.unlockedVaultFolders() {
		if req.FolderID == 0 || f == req.FolderID {
			folders = append(folders, f)
		}
	}
	if len(folders) == 0 {
		return nil
	}
	m := vaultMatcher{
		terms:   strings.Fields(strings.ToLower(req.Query)),
		from:    strings.ToLower(strings.TrimSpace(req.From)),
		to:      strings.ToLower(strings.TrimSpace(req.To)),
		subject: strings.ToLower(strings.TrimSpace(req.Subject)),
	}
	if req.AfterUnix > 0 {
		m.after = time.Unix(req.AfterUnix, 0)
	}
	if req.BeforeUnix > 0 {
		m.before = time.Unix(req.BeforeUnix, 0)
	}
	var out []storage.Message
	ctx := a.mailCtx()
	for offset := 0; offset < mailVaultSearchScan; offset += mailVaultSearchPage {
		page, err := a.store.QueryMessages(ctx, storage.MessageQuery{FolderIDs: folders, Limit: mailVaultSearchPage, Offset: offset})
		if err != nil || len(page) == 0 {
			break
		}
		for i := range page {
			if m.matches(&page[i]) && req.keeps(&page[i]) {
				out = append(out, page[i])
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

type vaultMatcher struct {
	terms             []string
	from, to, subject string
	after, before     time.Time
}

func (v vaultMatcher) matches(m *storage.Message) bool {
	if (!v.after.IsZero() && m.Date.Before(v.after)) || (!v.before.IsZero() && m.Date.After(v.before)) {
		return false
	}
	from := strings.ToLower(m.FromName + " " + m.FromAddress)
	to := strings.ToLower(m.ToAddresses + " " + m.CcAddresses)
	subject := strings.ToLower(m.Subject)
	if !strings.Contains(from, v.from) || !strings.Contains(to, v.to) || !strings.Contains(subject, v.subject) {
		return false
	}
	if len(v.terms) == 0 {
		return true
	}
	body := m.BodyPlain
	if strings.TrimSpace(body) == "" {
		body = mailview.PlainText(m.BodyHTML)
	}
	hay := subject + "\n" + from + "\n" + to + "\n" + strings.ToLower(body)
	for _, t := range v.terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

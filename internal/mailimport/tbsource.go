package mailimport

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// thunderbirdSource is one Thunderbird profile: its accounts (settings only),
// the mail kept on this computer (Local Folders, and the mail of POP3
// accounts, which exists nowhere else), the cached copies of IMAP folders,
// address books, local calendars and message filters.
type thunderbirdSource struct {
	diskReader
	dir string
}

func (s *thunderbirdSource) Kind() string { return KindThunderbird }

// LocalFoldersName is the name Thunderbird gives its on-this-computer store.
const LocalFoldersName = "Local Folders"

func (s *thunderbirdSource) Scan(ctx context.Context) (Inventory, error) {
	inv := Inventory{Kind: KindThunderbird, Root: s.dir}
	p, err := readPrefs(filepath.Join(s.dir, "prefs.js"))
	if err != nil {
		return inv, err
	}
	inv.Accounts = accountsFromPrefs(p)
	for _, acc := range inv.Accounts {
		if acc.Kind == "pop3" {
			inv.Notices = append(inv.Notices, Notice{Code: NoticePOP3Account, Name: acc.Email})
		}
		if acc.SignInWithProvider {
			inv.Notices = append(inv.Notices, Notice{Code: NoticeOAuthAccount, Name: acc.Email})
		}
	}
	for _, srv := range thunderbirdServers(s.dir, p) {
		if ctx.Err() != nil {
			break
		}
		w := &walker{inv: &inv, ctx: ctx, serverCopy: srv.kind == "imap"}
		w.dir(srv.dir, []string{srv.label}, 0)
		if filters, err := readFilterRules(filepath.Join(srv.dir, "msgFilterRules.dat"), srv.label); err == nil {
			inv.Filters = append(inv.Filters, filters...)
		}
	}
	s.addressBooks(&inv, p)
	s.calendars(&inv, p)
	return inv, ctx.Err()
}

// tbServer is one account's mail directory in the profile.
type tbServer struct {
	key, kind, label, dir string
}

// thunderbirdServers lists the mail directory of every account, Local Folders
// first. A directory is a pref, absolute or relative to the profile, with the
// conventional location as the fallback.
func thunderbirdServers(profile string, p prefs) []tbServer {
	labels := map[string]string{}
	for _, id := range splitList(p["mail.accountmanager.accounts"]) {
		server := p["mail.account."+id+".server"]
		if ids := splitList(p["mail.account."+id+".identities"]); len(ids) > 0 {
			if email := p["mail.identity."+ids[0]+".useremail"]; email != "" {
				labels[server] = email
			}
		}
	}
	var out []tbServer
	for key, value := range p {
		if !strings.HasPrefix(key, "mail.server.") || !strings.HasSuffix(key, ".type") {
			continue
		}
		server := strings.TrimSuffix(strings.TrimPrefix(key, "mail.server."), ".type")
		var label, fallback string
		switch value {
		case "none":
			label, fallback = LocalFoldersName, filepath.Join(profile, "Mail", "Local Folders")
		case "pop3", "imap":
			host := p["mail.server."+server+".hostname"]
			label = labels[server]
			if label == "" {
				label = p["mail.server."+server+".name"]
			}
			if label == "" {
				label = host
			}
			parent := "Mail"
			if value == "imap" {
				parent = "ImapMail"
			}
			fallback = filepath.Join(profile, parent, host)
		default:
			continue
		}
		dir := p["mail.server."+server+".directory"]
		if rel := p["mail.server."+server+".directory-rel"]; rel != "" {
			dir = filepath.Join(profile, filepath.FromSlash(strings.TrimPrefix(rel, "[ProfD]")))
		}
		if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
			dir = fallback
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, tbServer{key: server, kind: value, label: label, dir: dir})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].kind == "none") != (out[j].kind == "none") {
			return out[i].kind == "none"
		}
		return out[i].label < out[j].label
	})
	return out
}

// addressBooks lists the profile's address books. Each is named by a
// ldap_2.servers pref; the personal and collected books exist even when no
// pref names them.
func (s *thunderbirdSource) addressBooks(inv *Inventory, p prefs) {
	seen := map[string]bool{}
	add := func(file, name string) {
		if seen[file] {
			return
		}
		seen[file] = true
		path := filepath.Join(s.dir, file)
		if _, err := os.Stat(path); err != nil {
			return
		}
		if strings.HasSuffix(file, ".mab") {
			inv.Notices = append(inv.Notices, Notice{Code: NoticeOldAddressBook, Name: name})
			return
		}
		inv.AddressBooks = append(inv.AddressBooks, ContactSource{Key: FormatTBAddressBook + ":" + path, Name: name,
			Format: FormatTBAddressBook, Location: path, Count: countThunderbirdCards(path)})
	}
	var keys []string
	for key := range p {
		if strings.HasPrefix(key, "ldap_2.servers.") && strings.HasSuffix(key, ".filename") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		base := strings.TrimSuffix(key, ".filename")
		name := p[base+".description"]
		file := p[key]
		if name == "" {
			name = strings.TrimSuffix(file, filepath.Ext(file))
		}
		add(file, name)
	}
	add("abook.sqlite", "Personal Address Book")
	add("history.sqlite", "Collected Addresses")
}

// calendars lists the profile's local calendars, which all share
// calendar-data/local.sqlite. Calendars that sync with a server are named in
// a notice: adding that server's calendar account brings them back.
func (s *thunderbirdSource) calendars(inv *Inventory, p prefs) {
	store := filepath.Join(s.dir, "calendar-data", "local.sqlite")
	_, storeErr := os.Stat(store)
	var ids []string
	for key, value := range p {
		if strings.HasPrefix(key, "calendar.registry.") && strings.HasSuffix(key, ".type") {
			ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(key, "calendar.registry."), ".type")+"\x00"+value)
		}
	}
	sort.Strings(ids)
	for _, entry := range ids {
		id, kind, _ := strings.Cut(entry, "\x00")
		name := p["calendar.registry."+id+".name"]
		if name == "" {
			name = id
		}
		if kind != "storage" {
			inv.Notices = append(inv.Notices, Notice{Code: NoticeRemoteCalendar, Name: name})
			continue
		}
		if storeErr != nil {
			continue
		}
		inv.Calendars = append(inv.Calendars, CalendarSource{Key: FormatTBCalendar + ":" + id, Name: name,
			Format: FormatTBCalendar, Location: store, ID: id, Count: countThunderbirdItems(store, id)})
	}
}

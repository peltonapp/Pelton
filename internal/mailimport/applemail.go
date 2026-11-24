package mailimport

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Apple Mail keeps its store under ~/Library/Mail/V<n>: one directory per
// account, one for the mailboxes "On My Mac" (Mailboxes), and MailData with
// its indexes. Each mailbox is a directory ending in .mbox holding .emlx
// files somewhere below it, and nested mailboxes are .mbox directories inside
// their parent. macOS guards that folder, so the user picks it, which is what
// grants Pelton access to read it.

type appleMailSource struct {
	diskReader
	root string
}

func (s *appleMailSource) Kind() string { return KindAppleMail }

// appleLocalMailboxes is the name of the directory holding "On My Mac".
const appleLocalMailboxes = "Mailboxes"

// AppleOnMyMac is the folder name the local mailboxes import under.
const AppleOnMyMac = "On My Mac"

func (s *appleMailSource) Scan(ctx context.Context) (Inventory, error) {
	inv := Inventory{Kind: KindAppleMail, Root: s.root}
	base := appleVersionDir(s.root)
	names, accounts := appleAccounts(base)
	inv.Accounts = accounts
	if len(accounts) == 0 {
		inv.Notices = append(inv.Notices, Notice{Code: NoticeNoAppleAccounts})
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return inv, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "MailData" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		label := AppleOnMyMac
		w := &walker{inv: &inv, ctx: ctx}
		if e.Name() != appleLocalMailboxes {
			w.serverCopy = true
			label = names[e.Name()]
			if label == "" {
				label = displayName(e.Name())
			}
		}
		mailboxes, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, m := range mailboxes {
			if m.IsDir() && strings.HasSuffix(m.Name(), ".mbox") {
				w.dir(filepath.Join(dir, m.Name()), []string{label, displayName(strings.TrimSuffix(m.Name(), ".mbox"))}, 1)
			}
		}
	}
	for _, f := range inv.Folders {
		if f.Format == FormatEmlx && slices.ContainsFunc(emlxFiles(f.Location), isPartialEmlx) {
			inv.Notices = append(inv.Notices, Notice{Code: NoticePartialMessages, Name: strings.Join(f.Path, "/")})
		}
	}
	return inv, ctx.Err()
}

// appleVersionDir finds the newest V<n> store under the picked folder, or
// the folder itself when the user picked the store directly.
func appleVersionDir(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	best, bestN := "", -1
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) < 2 || e.Name()[0] != 'V' {
			continue
		}
		n, err := strconv.Atoi(e.Name()[1:])
		if err == nil && n > bestN {
			best, bestN = e.Name(), n
		}
	}
	if best == "" {
		return root
	}
	return filepath.Join(root, best)
}

// appleAccounts reads account settings from MailData/Accounts.plist, the
// file older Mail versions keep them in. Newer macOS keeps accounts in the
// system's own account store, outside the Mail folder, which is not read;
// those come back empty and the user adds them as usual. Passwords are never
// in this file and nothing here asks for them. It also returns a name for
// each account directory, for the folder tree.
func appleAccounts(base string) (map[string]string, []Account) {
	names := map[string]string{}
	data, err := os.ReadFile(filepath.Join(base, "MailData", "Accounts.plist"))
	if err != nil {
		return names, nil
	}
	root, err := parsePlist(data)
	if err != nil {
		return names, nil
	}
	top, _ := root.(map[string]any)
	smtp := map[string]map[string]any{}
	for _, d := range plistDicts(top["DeliveryAccounts"]) {
		key := plistString(d, "Hostname") + ":" + plistString(d, "Username")
		smtp[key] = d
		smtp[plistString(d, "Hostname")] = d
	}
	var out []Account
	for _, d := range plistDicts(top["MailAccounts"]) {
		kind := plistString(d, "AccountType")
		if path := plistString(d, "AccountPath"); path != "" {
			names[filepath.Base(path)] = plistString(d, "AccountName")
		}
		if kind != "IMAPAccount" && kind != "POPAccount" && kind != "iToolsAccount" {
			continue
		}
		emails := plistStrings(d["EmailAddresses"])
		acc := Account{
			DisplayName: plistString(d, "FullUserName"),
			Username:    plistString(d, "Username"),
			IMAPHost:    plistString(d, "Hostname"),
			IMAPPort:    int(plistInt(d, "PortNumber")),
			Kind:        "imap",
		}
		if kind == "POPAccount" {
			acc.Kind = "pop3"
		}
		if len(emails) > 0 {
			acc.Email = emails[0]
		}
		if acc.Email == "" || acc.IMAPHost == "" {
			continue
		}
		if acc.Username == acc.Email {
			acc.Username = ""
		}
		if id := plistString(d, "SMTPIdentifier"); id != "" {
			if s, ok := smtp[id]; ok {
				acc.SMTPHost = plistString(s, "Hostname")
				acc.SMTPPort = int(plistInt(s, "PortNumber"))
			} else if host, _, ok := strings.Cut(id, ":"); ok {
				acc.SMTPHost = host
			}
		}
		out = append(out, acc)
	}
	return names, out
}

func plistDicts(v any) []map[string]any {
	list, _ := v.([]any)
	var out []map[string]any
	for _, item := range list {
		if d, ok := item.(map[string]any); ok {
			out = append(out, d)
		}
	}
	return out
}

func plistStrings(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// emlxFiles lists the .emlx messages of one mailbox directory, wherever they
// sit below it, leaving out nested mailboxes, sorted.
func emlxFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasSuffix(d.Name(), ".mbox") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".emlx") {
			out = append(out, path)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func isPartialEmlx(path string) bool {
	return strings.HasSuffix(path, ".partial.emlx")
}

// The bits of an .emlx "flags" value Pelton keeps.
const (
	emlxRead      = 1 << 0
	emlxDeleted   = 1 << 1
	emlxAnswered  = 1 << 2
	emlxFlagged   = 1 << 4
	emlxForwarded = 1 << 8
)

// readEmlx reads one Apple Mail message: a line with the message's length in
// bytes, the message, then an XML property list with its state.
func readEmlx(path string) (Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return Item{}, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	first, err := br.ReadString('\n')
	if err != nil {
		return Item{}, err
	}
	length, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil || length < 0 {
		return Item{}, errBadPlist
	}
	if length > maxMessageBytes {
		return Item{Skip: ReasonTooLarge}, nil
	}
	raw := make([]byte, length)
	n, err := io.ReadFull(br, raw)
	if err != nil {
		if n == 0 {
			return Item{}, err
		}
		raw = raw[:n]
	}
	item := Item{Raw: raw, Flags: storage.FlagSeen}
	if isPartialEmlx(path) {
		item.Note = ReasonPartial
	}
	trailer, _ := io.ReadAll(io.LimitReader(br, 1<<20))
	if v, err := parsePlist(bytes.TrimSpace(trailer)); err == nil {
		if d, ok := v.(map[string]any); ok {
			bits := plistInt(d, "flags")
			item.Flags = 0
			if bits&emlxRead != 0 {
				item.Flags |= storage.FlagSeen
			}
			if bits&emlxAnswered != 0 {
				item.Flags |= storage.FlagAnswered
			}
			if bits&emlxFlagged != 0 {
				item.Flags |= storage.FlagFlagged
			}
			if bits&emlxForwarded != 0 {
				item.Flags |= storage.FlagForwarded
			}
			if bits&emlxDeleted != 0 {
				item.Skip = ReasonDeletedInSource
			}
			if secs := plistInt(d, "date-received"); secs > 0 {
				item.Received = time.Unix(secs, 0)
			}
		}
	}
	return item, nil
}

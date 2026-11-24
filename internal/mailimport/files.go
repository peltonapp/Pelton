package mailimport

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// filesSource is mail the user picked as files or folders: mbox archives,
// single .eml messages, Maildir folders, Apple Mail's mailbox exports, plus
// vCard and iCalendar files found among them. Each picked folder keeps its
// tree.
type filesSource struct {
	diskReader
	paths []string
}

func (s *filesSource) Kind() string { return KindFiles }

func (s *filesSource) Scan(ctx context.Context) (Inventory, error) {
	inv := Inventory{Kind: KindFiles, Root: strings.Join(s.paths, "\n")}
	w := &walker{inv: &inv, ctx: ctx}
	for _, p := range s.paths {
		info, err := os.Stat(p)
		if err != nil {
			inv.Notices = append(inv.Notices, Notice{Code: NoticeUnreadableFolder, Name: displayName(filepath.Base(p))})
			continue
		}
		if info.IsDir() {
			w.dir(p, []string{displayName(strings.TrimSuffix(filepath.Base(p), ".mbox"))}, 0)
		} else {
			w.file(p, nil)
		}
	}
	return inv, ctx.Err()
}

// walker collects what a tree of files holds into an inventory.
type walker struct {
	inv *Inventory
	ctx context.Context
	// serverCopy marks every folder found as a cached server copy.
	serverCopy bool
}

const maxWalkDepth = 32

// ignoredFiles are sidecars mail programs keep next to mail: indexes and
// summaries, never mail.
func ignoredFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".msf", ".dat", ".json", ".sqlite", ".plist", ".log", ".html", ".js", ".lock":
		return true
	}
	switch name {
	case "table_of_contents", "Info.plist", "dovecot-uidlist", "dovecot-keywords", "dovecot.index",
		"dovecot.index.cache", "dovecot.index.log", "maildirfolder", "subscriptions", "courierimapsubscribed":
		return true
	}
	return strings.HasPrefix(name, "dovecot.")
}

func (w *walker) addFolder(f MailFolder) {
	f.ServerCopy = w.serverCopy
	w.inv.Folders = append(w.inv.Folders, f)
}

func (w *walker) dir(dir string, path []string, depth int) {
	if depth > maxWalkDepth || w.ctx.Err() != nil {
		return
	}
	if isMaildir(dir) {
		w.maildir(dir, path)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.inv.Notices = append(w.inv.Notices, Notice{Code: NoticeUnreadableFolder, Name: strings.Join(path, "/")})
		return
	}
	if strings.HasSuffix(dir, ".mbox") {
		if info, err := os.Stat(filepath.Join(dir, "mbox")); err == nil && !info.IsDir() {
			w.mboxFile(filepath.Join(dir, "mbox"), path)
		} else if files := emlxFiles(dir); len(files) > 0 {
			w.addFolder(MailFolder{Key: FormatEmlx + ":" + dir, Path: path, Format: FormatEmlx, Location: dir,
				Count: len(files), SizeBytes: sizeOf(files)})
		}
	}
	if files := emlFiles(dir); len(files) > 0 {
		w.addFolder(MailFolder{Key: FormatEmlDir + ":" + dir, Path: path, Format: FormatEmlDir, Location: dir,
			Count: len(files), SizeBytes: sizeOf(files)})
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		if e.IsDir() {
			if strings.HasPrefix(name, ".") || name == "Messages" || name == "Attachments" || name == "Data" {
				continue
			}
			if strings.HasSuffix(dir, ".mbox") && !strings.HasSuffix(name, ".mbox") {
				continue
			}
			child := strings.TrimSuffix(strings.TrimSuffix(name, ".sbd"), ".mbox")
			w.dir(full, append(slices.Clone(path), displayName(child)), depth+1)
			continue
		}
		if strings.HasSuffix(dir, ".mbox") && name == "mbox" {
			continue
		}
		w.file(full, path)
	}
}

// file sorts one file into a folder, an address book, a calendar or a notice.
func (w *walker) file(full string, parent []string) {
	name := filepath.Base(full)
	ext := strings.ToLower(filepath.Ext(name))
	label := displayName(strings.TrimSuffix(name, filepath.Ext(name)))
	if ext == "" {
		label = displayName(name)
	}
	switch ext {
	case ".eml", ".emlx":
		if parent == nil {
			w.addFolder(MailFolder{Key: FormatEml + ":" + full, Format: FormatEml,
				Location: full, Count: 1, SizeBytes: sizeOf([]string{full})})
		}
		return
	case ".vcf", ".vcard":
		w.inv.AddressBooks = append(w.inv.AddressBooks, ContactSource{Key: FormatVCard + ":" + full, Name: label,
			Format: FormatVCard, Location: full, Count: countVCards(full)})
		return
	case ".ics", ".ical", ".ifb":
		w.inv.Calendars = append(w.inv.Calendars, CalendarSource{Key: FormatICS + ":" + full, Name: label,
			Format: FormatICS, Location: full, Count: countEvents(full)})
		return
	case ".pst":
		w.inv.Notices = append(w.inv.Notices, Notice{Code: NoticePST, Name: displayName(name)})
		return
	case ".ost":
		w.inv.Notices = append(w.inv.Notices, Notice{Code: NoticeOST, Name: displayName(name)})
		return
	}
	if ignoredFile(name) {
		return
	}
	if sniffFile(full) == FormatMbox {
		w.mboxFile(full, append(slices.Clone(parent), label))
		return
	}
	if parent == nil && sniffFile(full) == FormatEml {
		w.addFolder(MailFolder{Key: FormatEml + ":" + full, Format: FormatEml,
			Location: full, Count: 1, SizeBytes: sizeOf([]string{full})})
		return
	}
	if info, err := os.Stat(full); err == nil && info.Size() > 0 {
		w.inv.Notices = append(w.inv.Notices, Notice{Code: NoticeUnknownFile, Name: displayName(name)})
	}
}

func (w *walker) mboxFile(full string, path []string) {
	f, err := os.Open(full)
	if err != nil {
		w.inv.Notices = append(w.inv.Notices, Notice{Code: NoticeUnreadableFolder, Name: strings.Join(path, "/")})
		return
	}
	count, _ := countMbox(f)
	f.Close()
	w.addFolder(MailFolder{Key: FormatMbox + ":" + full, Path: path, Format: FormatMbox, Location: full,
		Count: count, SizeBytes: sizeOf([]string{full})})
}

// maildir adds a Maildir and the folders under it, in either layout: the
// Maildir++ one, where ".Work.Projects" beside cur/ is a folder two levels
// down, or one directory per folder.
func (w *walker) maildir(dir string, path []string) {
	files := maildirFiles(dir)
	w.addFolder(MailFolder{Key: FormatMaildir + ":" + dir, Path: path, Format: FormatMaildir, Location: dir,
		Count: len(files), SizeBytes: sizeOf(files)})
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "cur" || name == "new" || name == "tmp" {
			continue
		}
		full := filepath.Join(dir, name)
		if strings.HasPrefix(name, ".") && len(name) > 1 && isMaildir(full) {
			sub := slices.Clone(path)
			for _, part := range strings.Split(name[1:], ".") {
				sub = append(sub, mailboxName(part))
			}
			files := maildirFiles(full)
			w.addFolder(MailFolder{Key: FormatMaildir + ":" + full, Path: sub, Format: FormatMaildir, Location: full,
				Count: len(files), SizeBytes: sizeOf(files)})
			continue
		}
		if !strings.HasPrefix(name, ".") {
			w.dir(full, append(slices.Clone(path), mailboxName(name)), 1)
		}
	}
}

// importedFolderName is where single messages land, since an .eml carries no
// folder of its own.
const importedFolderName = "Imported"

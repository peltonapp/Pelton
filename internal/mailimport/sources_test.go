package mailimport

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func folderByPath(inv Inventory, path string) (MailFolder, bool) {
	for _, f := range inv.Folders {
		if strings.Join(f.Path, "/") == path {
			return f, true
		}
	}
	return MailFolder{}, false
}

func paths(inv Inventory) []string {
	var out []string
	for _, f := range inv.Folders {
		out = append(out, strings.Join(f.Path, "/"))
	}
	slices.Sort(out)
	return out
}

func hasNotice(inv Inventory, code string) bool {
	return slices.ContainsFunc(inv.Notices, func(n Notice) bool { return n.Code == code })
}

func TestThunderbirdScan(t *testing.T) {
	dir := makeThunderbirdProfile(t)
	src, err := Open(KindThunderbird, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Local Folders/Archive/2019", "Local Folders/Inbox",
		"juno@example.net/Inbox", "wren@example.org/INBOX",
	}
	if got := paths(inv); !slices.Equal(got, want) {
		t.Fatalf("folders = %v, want %v", got, want)
	}
	inbox, _ := folderByPath(inv, "Local Folders/Inbox")
	if inbox.Count != 3 || inbox.ServerCopy {
		t.Fatalf("local inbox = %+v", inbox)
	}
	cached, _ := folderByPath(inv, "wren@example.org/INBOX")
	if !cached.ServerCopy {
		t.Fatal("the cached IMAP folder was not marked as a server copy")
	}
	if len(inv.Accounts) != 2 || !inv.Accounts[0].SignInWithProvider {
		t.Fatalf("accounts = %+v", inv.Accounts)
	}
	for _, code := range []string{NoticePOP3Account, NoticeOAuthAccount, NoticeOldAddressBook, NoticeRemoteCalendar} {
		if !hasNotice(inv, code) {
			t.Errorf("no %s notice in %+v", code, inv.Notices)
		}
	}
	if len(inv.AddressBooks) != 1 || inv.AddressBooks[0].Count != 2 || inv.AddressBooks[0].Name != "Personal Address Book" {
		t.Fatalf("address books = %+v", inv.AddressBooks)
	}
	if len(inv.Calendars) != 1 || inv.Calendars[0].Name != "Home" || inv.Calendars[0].Count != 3 {
		t.Fatalf("calendars = %+v", inv.Calendars)
	}
	if len(inv.Filters) != 3 || inv.Filters[0].Name != "Receipts" || inv.Filters[0].Account != "wren@example.org" {
		t.Fatalf("filters = %+v", inv.Filters)
	}

	cards, err := src.Contacts(inv.AddressBooks[0])
	if err != nil || len(cards) != 2 {
		t.Fatalf("cards = %v %v", cards, err)
	}
	if !strings.Contains(cards[0], "FN:Ada Example") || !strings.Contains(cards[0], "TEL;TYPE=cell:+1 555 0100") {
		t.Fatalf("card built from properties = %q", cards[0])
	}
	if !strings.Contains(cards[1], "FN:Bo Example") {
		t.Fatalf("stored vCard = %q", cards[1])
	}

	ics, err := src.Calendar(inv.Calendars[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(ics)
	for _, want := range []string{
		"SUMMARY:Choir practice", "DTSTART;TZID=Europe/Berlin:", "RRULE:FREQ=WEEKLY;COUNT=4",
		`LOCATION:Hall\, room 2`, "DTSTART;VALUE=DATE:", "BEGIN:VTODO", "SUMMARY:Buy sheet music",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("calendar lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Elsewhere") {
		t.Error("an event of another calendar leaked into this one")
	}
}

func TestAppleMailScan(t *testing.T) {
	root := makeAppleMail(t)
	src, _ := Open(KindAppleMail, []string{root})
	inv, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Juno at Example/INBOX", "On My Mac/Family", "On My Mac/Family/Cousins"}
	if got := paths(inv); !slices.Equal(got, want) {
		t.Fatalf("folders = %v, want %v", got, want)
	}
	family, _ := folderByPath(inv, "On My Mac/Family")
	if family.Count != 3 || family.Format != FormatEmlx || family.ServerCopy {
		t.Fatalf("family = %+v", family)
	}
	server, _ := folderByPath(inv, "Juno at Example/INBOX")
	if !server.ServerCopy {
		t.Fatal("an account's mailbox was not marked as a server copy")
	}
	if !hasNotice(inv, NoticePartialMessages) {
		t.Fatal("no notice about partly stored messages")
	}
	if len(inv.Accounts) != 1 {
		t.Fatalf("accounts = %+v", inv.Accounts)
	}
	acc := inv.Accounts[0]
	if acc.Email != "juno@example.net" || acc.IMAPHost != "imap.example.net" || acc.IMAPPort != 993 ||
		acc.SMTPHost != "smtp.example.net" || acc.SMTPPort != 587 || acc.Username != "juno" {
		t.Fatalf("account = %+v", acc)
	}
	r, err := src.OpenFolder(family, 0)
	if err != nil {
		t.Fatal(err)
	}
	items := drain(t, r)
	if len(items) != 3 {
		t.Fatalf("%d items", len(items))
	}
}

func TestAppleMailWithoutAccountFile(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "V2", "Mailboxes", "Old.mbox", "Messages", "1.emlx"), "5\nab\r\n\r\n")
	src, _ := Open(KindAppleMail, []string{root})
	inv, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasNotice(inv, NoticeNoAppleAccounts) || len(inv.Folders) != 1 {
		t.Fatalf("inventory = %+v", inv)
	}
}

func TestFilesScan(t *testing.T) {
	root := t.TempDir()
	md := filepath.Join(root, "Maildir")
	mkdirs(t, filepath.Join(md, "cur"), filepath.Join(md, "new"), filepath.Join(md, "tmp"))
	put(t, filepath.Join(md, "cur", "1.host:2,S"), fixtureMessage("md-1@x", "one", "a"))
	sub := filepath.Join(md, ".Work.Entw&APw-rfe")
	mkdirs(t, filepath.Join(sub, "cur"), filepath.Join(sub, "new"), filepath.Join(sub, "tmp"))
	put(t, filepath.Join(sub, "new", "2.host"), fixtureMessage("md-2@x", "two", "b"))
	put(t, filepath.Join(md, "dovecot-uidlist"), "3 V1 N2\n")

	deep := filepath.Join(root, "Archive")
	for i := 0; i < 12; i++ {
		deep = filepath.Join(deep, "level"+itoa(i))
	}
	put(t, filepath.Join(deep, "old.mbox"), mboxEntry(fixtureMessage("deep@x", "deep", "down")))
	// a Latin-1 file name only exists where the file system stores bytes;
	// macOS refuses one, so there it is a plain UTF-8 name.
	latin := filepath.Join(root, "Archive", "caf\xe9")
	if os.WriteFile(latin, []byte(mboxEntry(fixtureMessage("latin@x", "latin", "name"))), 0o600) != nil {
		put(t, filepath.Join(root, "Archive", "café"), mboxEntry(fixtureMessage("latin@x", "latin", "name")))
	} else {
		put(t, latin+".msf", "mork")
	}
	put(t, filepath.Join(root, "Exported.mbox", "mbox"), mboxEntry(fixtureMessage("exp@x", "exported", "apple export")))
	put(t, filepath.Join(root, "Exported.mbox", "table_of_contents"), "binary")
	put(t, filepath.Join(root, "loose", "a.eml"), fixtureMessage("l-1@x", "a", "a"))
	put(t, filepath.Join(root, "loose", "b.eml"), fixtureMessage("l-2@x", "b", "b"))
	put(t, filepath.Join(root, "people.vcf"), "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ida Example\r\nEND:VCARD\r\nBEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ole Example\r\nEND:VCARD\r\n")
	put(t, filepath.Join(root, "dates.ics"), "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	put(t, filepath.Join(root, "outlook.pst"), "!BDN")
	put(t, filepath.Join(root, "notes.txt"), "nothing to do with mail")

	src, _ := Open(KindFiles, []string{root})
	inv, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(root)
	deepPath := base + "/Archive"
	for i := 0; i < 12; i++ {
		deepPath += "/level" + itoa(i)
	}
	want := []string{
		base + "/Archive/café", deepPath + "/old", base + "/Exported", base + "/Maildir",
		base + "/Maildir/Work/Entwürfe", base + "/loose",
	}
	slices.Sort(want)
	if got := paths(inv); !slices.Equal(got, want) {
		t.Fatalf("folders =\n%v\nwant\n%v", got, want)
	}
	if len(inv.AddressBooks) != 1 || inv.AddressBooks[0].Count != 2 {
		t.Fatalf("address books = %+v", inv.AddressBooks)
	}
	if len(inv.Calendars) != 1 || inv.Calendars[0].Count != 1 {
		t.Fatalf("calendars = %+v", inv.Calendars)
	}
	if !hasNotice(inv, NoticePST) || !hasNotice(inv, NoticeUnknownFile) {
		t.Fatalf("notices = %+v", inv.Notices)
	}
	cards, err := src.Contacts(inv.AddressBooks[0])
	if err != nil || len(cards) != 2 || !strings.Contains(cards[1], "Ole Example") {
		t.Fatalf("cards = %q %v", cards, err)
	}
}

func TestPSTSourceIsNotAvailable(t *testing.T) {
	for _, name := range []string{"mail.pst", "cache.ost"} {
		src, _ := Open(KindPST, []string{filepath.Join(t.TempDir(), name)})
		inv, err := src.Scan(context.Background())
		if err != nil || len(inv.Notices) != 1 || len(inv.Folders) != 0 {
			t.Fatalf("%s: %+v %v", name, inv, err)
		}
		if _, err := src.OpenFolder(MailFolder{}, 0); err != ErrPSTUnavailable {
			t.Fatalf("open folder = %v", err)
		}
	}
}

func TestBinaryPlist(t *testing.T) {
	data := encodeBinaryPlist(map[string]any{"a": 1, "list": []any{"x", "longer than fifteen characters"}})
	v, err := parsePlist(data)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"] != int64(1) {
		t.Fatalf("a = %#v", m["a"])
	}
	list := m["list"].([]any)
	if list[1] != "longer than fifteen characters" {
		t.Fatalf("list = %#v", list)
	}
	for _, bad := range [][]byte{[]byte("bplist00"), append([]byte("bplist00"), make([]byte, 40)...), data[:len(data)-5]} {
		if _, err := parsePlist(bad); err == nil {
			t.Errorf("a broken plist of %d bytes parsed", len(bad))
		}
	}
}

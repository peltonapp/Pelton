package mailimport

import (
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Fixtures for every source, built in temp directories from made-up people
// and addresses. Nothing here reads a real profile.

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func put(t *testing.T, path, content string) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// bplistEncoder writes a binary property list from dicts, arrays, strings
// and integers, the subset Apple Mail's account file uses.
type bplistEncoder struct {
	objects [][]byte
}

func (e *bplistEncoder) add(v any) int {
	idx := len(e.objects)
	e.objects = append(e.objects, nil)
	var b []byte
	marker := func(kind byte, n int) {
		if n < 15 {
			b = append(b, kind<<4|byte(n))
			return
		}
		b = append(b, kind<<4|0x0f, 0x12)
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	switch x := v.(type) {
	case string:
		marker(0x5, len(x))
		b = append(b, x...)
	case int:
		b = append(b, 0x13)
		b = binary.BigEndian.AppendUint64(b, uint64(x))
	case []any:
		refs := make([]int, len(x))
		for i, item := range x {
			refs[i] = e.add(item)
		}
		marker(0xA, len(x))
		for _, r := range refs {
			b = binary.BigEndian.AppendUint16(b, uint16(r))
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var krefs, vrefs []int
		for _, k := range keys {
			krefs = append(krefs, e.add(k))
			vrefs = append(vrefs, e.add(x[k]))
		}
		marker(0xD, len(keys))
		for _, r := range append(krefs, vrefs...) {
			b = binary.BigEndian.AppendUint16(b, uint16(r))
		}
	}
	e.objects[idx] = b
	return idx
}

func encodeBinaryPlist(v any) []byte {
	e := &bplistEncoder{}
	top := e.add(v)
	out := []byte("bplist00")
	offsets := make([]int, len(e.objects))
	for i, o := range e.objects {
		offsets[i] = len(out)
		out = append(out, o...)
	}
	table := len(out)
	for _, off := range offsets {
		out = binary.BigEndian.AppendUint32(out, uint32(off))
	}
	trailer := make([]byte, 32)
	trailer[6], trailer[7] = 4, 2
	binary.BigEndian.PutUint64(trailer[8:], uint64(len(e.objects)))
	binary.BigEndian.PutUint64(trailer[16:], uint64(top))
	binary.BigEndian.PutUint64(trailer[24:], uint64(table))
	return append(out, trailer...)
}

// makeThunderbirdProfile builds a profile with one IMAP account (OAuth), one
// POP3 account with mail of its own, Local Folders with a nested folder and
// its .msf sidecars, filters, an address book and a local calendar.
func makeThunderbirdProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prefs := []string{
		`// Mozilla User Preferences`,
		`user_pref("mail.accountmanager.accounts", "account1,account2,account3");`,
		`user_pref("mail.account.account1.server", "server1");`,
		`user_pref("mail.account.account1.identities", "id1");`,
		`user_pref("mail.server.server1.type", "imap");`,
		`user_pref("mail.server.server1.hostname", "imap.example.org");`,
		`user_pref("mail.server.server1.port", 993);`,
		`user_pref("mail.server.server1.authMethod", 10);`,
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.example.org");`,
		`user_pref("mail.identity.id1.useremail", "wren@example.org");`,
		`user_pref("mail.identity.id1.fullName", "Wren Example");`,
		`user_pref("mail.identity.id1.smtpServer", "smtp1");`,
		`user_pref("mail.smtpserver.smtp1.hostname", "smtp.example.org");`,
		`user_pref("mail.smtpserver.smtp1.port", 465);`,
		`user_pref("mail.account.account2.server", "server2");`,
		`user_pref("mail.account.account2.identities", "id2");`,
		`user_pref("mail.server.server2.type", "pop3");`,
		`user_pref("mail.server.server2.hostname", "pop.example.net");`,
		`user_pref("mail.server.server2.directory-rel", "[ProfD]Mail/pop.example.net");`,
		`user_pref("mail.identity.id2.useremail", "juno@example.net");`,
		`user_pref("mail.account.account3.server", "server3");`,
		`user_pref("mail.server.server3.type", "none");`,
		`user_pref("mail.server.server3.directory-rel", "[ProfD]Mail/Local Folders");`,
		`user_pref("ldap_2.servers.pab.filename", "abook.sqlite");`,
		`user_pref("ldap_2.servers.pab.description", "Personal Address Book");`,
		`user_pref("ldap_2.servers.old.filename", "impab.mab");`,
		`user_pref("ldap_2.servers.old.description", "Old Book");`,
		`user_pref("calendar.registry.cal-1.type", "storage");`,
		`user_pref("calendar.registry.cal-1.name", "Home");`,
		`user_pref("calendar.registry.cal-2.type", "caldav");`,
		`user_pref("calendar.registry.cal-2.name", "Work server");`,
	}
	put(t, filepath.Join(dir, "prefs.js"), strings.Join(prefs, "\n")+"\n")

	local := filepath.Join(dir, "Mail", "Local Folders")
	put(t, filepath.Join(local, "Inbox"),
		mboxEntry("X-Mozilla-Status: 0001\r\n"+fixtureMessage("tb-1@example.org", "read one", "hello"))+
			mboxEntry("X-Mozilla-Status: 0004\r\n"+fixtureMessage("tb-2@example.org", "flagged unread", "flag"))+
			mboxEntry("X-Mozilla-Status: 0009\r\n"+fixtureMessage("tb-3@example.org", "expunged", "gone")))
	put(t, filepath.Join(local, "Inbox.msf"), "// <!-- <mdb:mork:z v=\"1.4\"/> -->\n")
	put(t, filepath.Join(local, "Archive.sbd", "2019"), mboxEntry(fixtureMessage("tb-4@example.org", "old", "archived")))
	put(t, filepath.Join(local, "Archive.sbd", "2019.msf"), "mork")
	put(t, filepath.Join(local, "Trash"), "")
	put(t, filepath.Join(local, "msgFilterRules.dat"), "version=\"9\"\nlogging=\"no\"\n")

	pop := filepath.Join(dir, "Mail", "pop.example.net")
	put(t, filepath.Join(pop, "Inbox"), mboxEntry(fixtureMessage("pop-1@example.net", "pop mail", "only here")))
	put(t, filepath.Join(pop, "popstate.dat"), "# POP3 State File\n")

	imap := filepath.Join(dir, "ImapMail", "imap.example.org")
	put(t, filepath.Join(imap, "INBOX"), mboxEntry(fixtureMessage("imap-1@example.org", "cached", "server copy")))
	put(t, filepath.Join(imap, "msgFilterRules.dat"), strings.Join([]string{
		`version="9"`,
		`logging="no"`,
		`name="Receipts"`,
		`enabled="yes"`,
		`type="17"`,
		`action="Move to folder"`,
		`actionValue="imap://wren%40example.org@imap.example.org/INBOX/Receipts"`,
		`action="Mark read"`,
		`condition="AND (from,contains,shop@example.com) AND (subject,doesn't contain,\"reminder, weekly\")"`,
		`name="Tag it"`,
		`enabled="yes"`,
		`type="1"`,
		`action="AddTag"`,
		`actionValue="$label1"`,
		`condition="AND (date,is before,01-Jan-2020)"`,
		`name="Forward and flag"`,
		`enabled="no"`,
		`type="1"`,
		`action="Forward"`,
		`actionValue="else@example.com"`,
		`action="Mark flagged"`,
		`condition="OR (to or cc,is,team@example.org) OR (\"X-Priority\",begins with,1)"`,
	}, "\n")+"\n")

	makeAddressBook(t, filepath.Join(dir, "abook.sqlite"))
	put(t, filepath.Join(dir, "impab.mab"), "// <!-- <mdb:mork:z v=\"1.4\"/> -->")
	makeCalendarStore(t, filepath.Join(dir, "calendar-data", "local.sqlite"))
	return dir
}

func makeAddressBook(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE properties (card TEXT, name TEXT, value TEXT)`,
		`INSERT INTO properties VALUES ('c1', 'DisplayName', 'Ada Example')`,
		`INSERT INTO properties VALUES ('c1', 'PrimaryEmail', 'ada@example.com')`,
		`INSERT INTO properties VALUES ('c1', 'CellularNumber', '+1 555 0100')`,
		`INSERT INTO properties VALUES ('c2', '_vCard', 'BEGIN:VCARD' || char(13,10) || 'VERSION:4.0' || char(13,10) || 'UID:c2' || char(13,10) || 'FN:Bo Example' || char(13,10) || 'EMAIL:bo@example.com' || char(13,10) || 'END:VCARD')`,
		`INSERT INTO properties VALUES ('c2', 'PrimaryEmail', 'bo@example.com')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
}

func makeCalendarStore(t *testing.T, path string) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := int64(1700000000) * 1_000_000
	stmts := []string{
		`CREATE TABLE cal_events (cal_id TEXT, id TEXT, time_created INTEGER, last_modified INTEGER, title TEXT,
			priority INTEGER, privacy TEXT, ical_status TEXT, flags INTEGER, event_start INTEGER, event_start_tz TEXT,
			event_end INTEGER, event_end_tz TEXT, event_stamp INTEGER, recurrence_id INTEGER, recurrence_id_tz TEXT,
			alarm_last_ack INTEGER, offline_journal INTEGER)`,
		`CREATE TABLE cal_todos (cal_id TEXT, id TEXT, time_created INTEGER, last_modified INTEGER, title TEXT,
			priority INTEGER, privacy TEXT, ical_status TEXT, flags INTEGER, todo_entry INTEGER, todo_entry_tz TEXT,
			todo_due INTEGER, todo_due_tz TEXT, todo_completed INTEGER, todo_completed_tz TEXT, todo_complete INTEGER,
			recurrence_id INTEGER, recurrence_id_tz TEXT, alarm_last_ack INTEGER, offline_journal INTEGER)`,
		`CREATE TABLE cal_properties (item_id TEXT, key TEXT, value BLOB, recurrence_id INTEGER, recurrence_id_tz TEXT, cal_id TEXT)`,
		`CREATE TABLE cal_recurrence (item_id TEXT, cal_id TEXT, icalString TEXT)`,
		`CREATE TABLE cal_attendees (item_id TEXT, recurrence_id INTEGER, recurrence_id_tz TEXT, icalString TEXT, cal_id TEXT)`,
		`INSERT INTO cal_events (cal_id, id, title, flags, event_start, event_start_tz, event_end, event_end_tz)
			VALUES ('cal-1', 'ev-1', 'Choir practice', 0, ` + strconv.FormatInt(start, 10) + `, 'Europe/Berlin', ` +
			strconv.FormatInt(start+3600_000_000, 10) + `, 'Europe/Berlin')`,
		`INSERT INTO cal_events (cal_id, id, title, flags, event_start, event_start_tz, event_end, event_end_tz)
			VALUES ('cal-1', 'ev-2', 'Holiday', 8, ` + strconv.FormatInt(start, 10) + `, 'floating', ` +
			strconv.FormatInt(start+86400_000_000, 10) + `, 'floating')`,
		`INSERT INTO cal_events (cal_id, id, title, flags, event_start, event_start_tz) VALUES ('cal-2', 'ev-3', 'Elsewhere', 0, 1, 'UTC')`,
		`INSERT INTO cal_properties VALUES ('ev-1', 'LOCATION', 'Hall, room 2', NULL, NULL, 'cal-1')`,
		`INSERT INTO cal_recurrence VALUES ('ev-1', 'cal-1', 'RRULE:FREQ=WEEKLY;COUNT=4')`,
		`INSERT INTO cal_todos (cal_id, id, title, flags, todo_due, todo_due_tz) VALUES ('cal-1', 'td-1', 'Buy sheet music', 0, ` +
			strconv.FormatInt(start, 10) + `, 'UTC')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// makeAppleMail builds ~/Library/Mail with a V10 store: mailboxes on this
// Mac with a nested child, one account's cached mailbox and an account file.
func makeAppleMail(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	v := filepath.Join(root, "V10")
	emlx := func(msg string, flags int) string {
		return strconv.Itoa(len(msg)) + "\n" + msg +
			`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>flags</key><integer>` +
			strconv.Itoa(flags) + `</integer></dict></plist>`
	}
	box := filepath.Join(v, "Mailboxes", "Family.mbox", "1A2B", "Data", "Messages")
	put(t, filepath.Join(box, "1.emlx"), emlx(fixtureMessage("am-1@example.org", "picnic", "sunday"), 1))
	put(t, filepath.Join(box, "2.emlx"), emlx(fixtureMessage("am-2@example.org", "photos", "attached"), 1|16))
	put(t, filepath.Join(box, "3.partial.emlx"), emlx(fixtureMessage("am-3@example.org", "big file", "parts elsewhere"), 0))
	put(t, filepath.Join(v, "Mailboxes", "Family.mbox", "Info.plist"), "<plist/>")
	child := filepath.Join(v, "Mailboxes", "Family.mbox", "Cousins.mbox", "9F", "Data", "Messages")
	put(t, filepath.Join(child, "7.emlx"), emlx(fixtureMessage("am-7@example.org", "reunion", "next year"), 1|4))
	acct := filepath.Join(v, "3C4D-ACCOUNT", "INBOX.mbox", "Messages")
	put(t, filepath.Join(acct, "5.emlx"), emlx(fixtureMessage("am-5@example.org", "server", "copy"), 1))
	put(t, filepath.Join(v, "MailData", "Envelope Index"), "sqlite bytes")
	plist := map[string]any{
		"MailAccounts": []any{
			map[string]any{"AccountType": "LocalAccount", "AccountName": "On My Mac"},
			map[string]any{
				"AccountType": "IMAPAccount", "AccountName": "Juno at Example", "AccountPath": "~/Library/Mail/V10/3C4D-ACCOUNT",
				"EmailAddresses": []any{"juno@example.net"}, "FullUserName": "Juno Example",
				"Hostname": "imap.example.net", "PortNumber": "993", "Username": "juno",
				"SMTPIdentifier": "smtp.example.net:juno",
			},
		},
		"DeliveryAccounts": []any{
			map[string]any{"Hostname": "smtp.example.net", "Username": "juno", "PortNumber": 587},
		},
	}
	put(t, filepath.Join(v, "MailData", "Accounts.plist"), string(encodeBinaryPlist(plist)))
	mkdirs(t, filepath.Join(root, "V9"))
	return root
}

package mailimport

import (
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxTextFile caps a vCard or iCalendar file read whole.
const maxTextFile = 64 << 20

// readContacts returns an address book's cards as vCard text.
func readContacts(book ContactSource) ([]string, error) {
	switch book.Format {
	case FormatVCard:
		f, err := os.Open(book.Location)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return splitVCards(io.LimitReader(f, maxTextFile))
	case FormatTBAddressBook:
		return thunderbirdCards(book.Location)
	}
	return nil, fmt.Errorf("mailimport: unknown address book format %q", book.Format)
}

// splitVCards cuts a file of vCards into one text per card. Text outside a
// BEGIN:VCARD ... END:VCARD pair is dropped.
func splitVCards(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var (
		out  []string
		card strings.Builder
		in   bool
	)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case upper == "BEGIN:VCARD":
			in = true
			card.Reset()
			card.WriteString("BEGIN:VCARD\r\n")
		case upper == "END:VCARD" && in:
			card.WriteString("END:VCARD\r\n")
			out = append(out, card.String())
			in = false
		case in:
			card.WriteString(line + "\r\n")
		}
	}
	return out, sc.Err()
}

func countVCards(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	return countLinePrefix(f, "BEGIN:VCARD")
}

func countLinePrefix(r io.Reader, prefixes ...string) int {
	sc := bufio.NewScanner(io.LimitReader(r, maxTextFile))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	n := 0
	for sc.Scan() {
		line := bytes.ToUpper(bytes.TrimSpace(sc.Bytes()))
		for _, p := range prefixes {
			if bytes.Equal(line, []byte(p)) {
				n++
			}
		}
	}
	return n
}

// openReadOnly opens another program's SQLite file without writing to it:
// immutable tells SQLite the file cannot change, so it takes no lock and
// creates no journal or shared-memory file beside it. Changes still sitting
// in that program's write-ahead log are not seen, which only matters while
// it is running.
func openReadOnly(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(path)}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// thunderbirdCards reads a Thunderbird address book (abook.sqlite). Newer
// versions keep each contact's whole vCard in a _vCard property; older ones
// only a set of named properties, which are turned into a card here.
func thunderbirdCards(path string) ([]string, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT card, name, value FROM properties ORDER BY card`)
	if err != nil {
		return nil, fmt.Errorf("mailimport: read address book: %w", err)
	}
	defer rows.Close()
	cards := map[string]map[string]string{}
	var order []string
	for rows.Next() {
		var id, name, value string
		if err := rows.Scan(&id, &name, &value); err != nil {
			return nil, err
		}
		props, ok := cards[id]
		if !ok {
			props = map[string]string{}
			cards[id] = props
			order = append(order, id)
		}
		props[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(order))
	for _, id := range order {
		props := cards[id]
		if v := props["_vCard"]; strings.Contains(strings.ToUpper(v), "BEGIN:VCARD") {
			parsed, err := splitVCards(strings.NewReader(v))
			if err == nil && len(parsed) > 0 {
				out = append(out, parsed[0])
				continue
			}
		}
		if card := cardFromProperties(id, props); card != "" {
			out = append(out, card)
		}
	}
	return out, nil
}

func countThunderbirdCards(path string) int {
	db, err := openReadOnly(path)
	if err != nil {
		return 0
	}
	defer db.Close()
	var n int
	_ = db.QueryRow(`SELECT count(DISTINCT card) FROM properties`).Scan(&n)
	return n
}

// cardFromProperties builds a vCard 4 from Thunderbird's named properties.
func cardFromProperties(id string, p map[string]string) string {
	name := p["DisplayName"]
	if name == "" {
		name = strings.TrimSpace(p["FirstName"] + " " + p["LastName"])
	}
	if name == "" && p["PrimaryEmail"] == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:4.0\r\n")
	b.WriteString("UID:" + vcardEscape(id) + "\r\n")
	b.WriteString("FN:" + vcardEscape(name) + "\r\n")
	if p["FirstName"] != "" || p["LastName"] != "" {
		b.WriteString("N:" + vcardEscape(p["LastName"]) + ";" + vcardEscape(p["FirstName"]) + ";;;\r\n")
	}
	if p["PrimaryEmail"] != "" {
		b.WriteString("EMAIL;TYPE=home:" + vcardEscape(p["PrimaryEmail"]) + "\r\n")
	}
	if p["SecondEmail"] != "" {
		b.WriteString("EMAIL;TYPE=work:" + vcardEscape(p["SecondEmail"]) + "\r\n")
	}
	phones := map[string]string{"HomePhone": "home", "WorkPhone": "work", "CellularNumber": "cell", "FaxNumber": "fax", "PagerNumber": "pager"}
	keys := make([]string, 0, len(phones))
	for k := range phones {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := p[k]; v != "" {
			b.WriteString("TEL;TYPE=" + phones[k] + ":" + vcardEscape(v) + "\r\n")
		}
	}
	if p["Company"] != "" {
		b.WriteString("ORG:" + vcardEscape(p["Company"]) + "\r\n")
	}
	if p["JobTitle"] != "" {
		b.WriteString("TITLE:" + vcardEscape(p["JobTitle"]) + "\r\n")
	}
	if p["Notes"] != "" {
		b.WriteString("NOTE:" + vcardEscape(p["Notes"]) + "\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

func vcardEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(s)
}

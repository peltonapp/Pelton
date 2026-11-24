package mailimport

import (
	"context"
	"errors"
	"fmt"
)

// The programs and file kinds a guided import reads from.
const (
	KindThunderbird = "thunderbird"
	KindAppleMail   = "applemail"
	KindFiles       = "files"
	KindPST         = "pst"
)

// MailFolder is one folder of mail a source holds.
type MailFolder struct {
	// Key identifies the folder within its source, stable across scans, so a
	// plan and a resumed run can name it.
	Key string
	// Path is the folder's place in the source's tree, top level first.
	Path []string
	// Format and Location say how and where it is stored.
	Format   string
	Location string
	// Count is the number of messages, SizeBytes their size on disk.
	Count     int
	SizeBytes int64
	// ServerCopy marks a folder that is a cached copy of mail an account
	// still holds on its server, which adding the account brings back anyway.
	ServerCopy bool
}

// ContactSource is one address book a source holds.
type ContactSource struct {
	Key      string
	Name     string
	Format   string
	Location string
	Count    int
}

// CalendarSource is one calendar a source holds.
type CalendarSource struct {
	Key      string
	Name     string
	Format   string
	Location string
	// ID is the calendar's id inside a store shared by several calendars.
	ID    string
	Count int
}

// The address book and calendar formats.
const (
	FormatVCard         = "vcf"
	FormatTBAddressBook = "tbabook"
	FormatICS           = "ics"
	FormatTBCalendar    = "tbcal"
)

// Notice is something found in the source that will not be imported, with a
// code the ui words and the name it concerns.
type Notice struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// The notice codes.
const (
	NoticePOP3Account      = "pop3Account"
	NoticeOAuthAccount     = "oauthAccount"
	NoticeOldAddressBook   = "oldAddressBook"
	NoticeRemoteCalendar   = "remoteCalendar"
	NoticePST              = "pstUnavailable"
	NoticeOST              = "ostUnsupported"
	NoticeUnknownFile      = "unknownFile"
	NoticeNoAppleAccounts  = "noAppleAccounts"
	NoticePartialMessages  = "partialMessages"
	NoticeUnreadableFolder = "unreadableFolder"
)

// Inventory is what a source holds, as the summary the user picks from.
type Inventory struct {
	Kind         string
	Root         string
	Accounts     []Account
	Folders      []MailFolder
	AddressBooks []ContactSource
	Calendars    []CalendarSource
	Filters      []Filter
	Notices      []Notice
}

// MailSource is a program's store or a set of files to import from. Every
// method only reads: nothing in a source is created, changed or deleted.
type MailSource interface {
	Kind() string
	// Scan finds what the source holds.
	Scan(ctx context.Context) (Inventory, error)
	// OpenFolder reads one folder's messages, starting at a position an
	// earlier run recorded (0 for the start).
	OpenFolder(f MailFolder, from int64) (MessageReader, error)
	// Contacts returns an address book's cards as vCard text.
	Contacts(book ContactSource) ([]string, error)
	// Calendar returns a calendar as iCalendar text.
	Calendar(cal CalendarSource) ([]byte, error)
}

// ErrUnknownKind is returned by Open for a kind it does not know.
var ErrUnknownKind = errors.New("mailimport: unknown import source")

// Open returns the source of the given kind rooted at path. For KindFiles,
// paths lists every file or folder the user picked.
func Open(kind string, paths []string) (MailSource, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("mailimport: no path to import from")
	}
	switch kind {
	case KindThunderbird:
		return &thunderbirdSource{dir: paths[0]}, nil
	case KindAppleMail:
		return &appleMailSource{root: paths[0]}, nil
	case KindFiles:
		return &filesSource{paths: paths}, nil
	case KindPST:
		return &pstSource{path: paths[0]}, nil
	}
	return nil, ErrUnknownKind
}

// diskReader implements the reading half of MailSource for stores made of plain
// files, which is every source but PST.
type diskReader struct{}

func (diskReader) OpenFolder(f MailFolder, from int64) (MessageReader, error) {
	return openDiskFolder(f, from)
}

func (diskReader) Contacts(book ContactSource) ([]string, error) {
	return readContacts(book)
}

func (diskReader) Calendar(cal CalendarSource) ([]byte, error) {
	return readCalendar(cal)
}

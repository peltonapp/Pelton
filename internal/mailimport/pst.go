package mailimport

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// ErrPSTUnavailable is returned for an Outlook data file: reading one is not
// available in this version.
var ErrPSTUnavailable = errors.New("mailimport: reading Outlook data files is not available yet")

// pstSource stands in for an Outlook .pst or .ost file. Its Scan reports the
// file as one Pelton cannot read yet, so the guided flow can say so instead
// of failing; a reader plugs in here without anything else changing.
type pstSource struct {
	path string
}

func (s *pstSource) Kind() string { return KindPST }

func (s *pstSource) Scan(ctx context.Context) (Inventory, error) {
	inv := Inventory{Kind: KindPST, Root: s.path}
	code := NoticePST
	if strings.EqualFold(filepath.Ext(s.path), ".ost") {
		code = NoticeOST
	}
	inv.Notices = append(inv.Notices, Notice{Code: code, Name: filepath.Base(s.path)})
	return inv, ctx.Err()
}

func (s *pstSource) OpenFolder(MailFolder, int64) (MessageReader, error) {
	return nil, ErrPSTUnavailable
}

func (s *pstSource) Contacts(ContactSource) ([]string, error) { return nil, ErrPSTUnavailable }

func (s *pstSource) Calendar(CalendarSource) ([]byte, error) { return nil, ErrPSTUnavailable }

package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/emersion/go-imap/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/peltonapp/Pelton/internal/mailexport"
	"github.com/peltonapp/Pelton/internal/storage"
)

// EmlExportDTO reports a multi-message export. Dir is empty when the user
// cancelled the folder picker. FirstError is the reason the first failed
// message could not be written, so the ui can say more than a count.
type EmlExportDTO struct {
	Dir        string `json:"dir"`
	Exported   int    `json:"exported"`
	Failed     int    `json:"failed"`
	FirstError string `json:"firstError"`
}

// emlSource is the outcome of fetching one message's raw source.
type emlSource struct {
	raw []byte
	err error
}

// emlFetcher returns the raw source of every message in ms, which all live in
// the same folder, in the same order.
type emlFetcher func(ms []*storage.Message) []emlSource

// emlOptions is the naming a message gets inside dir: its account's file name
// template, and no subfolders because the user picked exactly where files go.
func (a *App) emlOptions(dir string, m *storage.Message) mailexport.Options {
	options := mailexport.Options{Dir: dir, Subfolders: mailexport.SubfoldersNone}
	if account, err := a.store.GetAccount(a.ctx, m.AccountID); err == nil {
		options.Template = account.ExportNameTemplate
	}
	return options
}

// ExportMessageEML asks where to save one message and writes its raw source
// there as an .eml file. It returns the path written, or empty when the user
// cancelled.
func (a *App) ExportMessageEML(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return "", err
	}
	name := mailexport.FileName(exportMeta(m), a.emlOptions("", m).Template) + ".eml"
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export message as .eml",
		DefaultFilename: name,
		Filters:         []runtime.FileFilter{{DisplayName: "Email (*.eml)", Pattern: "*.eml"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}

	src := a.fetchEMLSources([]*storage.Message{m})[0]
	if src.err != nil {
		return "", src.err
	}
	if len(src.raw) == 0 {
		return "", errors.New("pelton: the message has no content to export")
	}
	if err := os.WriteFile(filepath.Clean(path), src.raw, 0o600); err != nil {
		return "", fmt.Errorf("pelton: write %s: %w", path, err)
	}
	return path, nil
}

// ExportMessagesEML asks for a folder and writes one .eml file per message into
// it, named by each account's file name template with -2, -3 suffixes on a
// clash. A message that cannot be fetched or written is counted and skipped so
// the rest still export.
func (a *App) ExportMessagesEML(ids []int64) (EmlExportDTO, error) {
	if err := a.ready(); err != nil {
		return EmlExportDTO{}, err
	}
	if len(ids) == 0 {
		return EmlExportDTO{}, errors.New("pelton: no messages to export")
	}
	msgs := make([]*storage.Message, 0, len(ids))
	for _, id := range ids {
		m, err := a.store.GetMessage(a.ctx, id)
		if err != nil {
			return EmlExportDTO{}, err
		}
		msgs = append(msgs, m)
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose where to save the exported messages",
	})
	if err != nil {
		return EmlExportDTO{}, err
	}
	if dir == "" {
		return EmlExportDTO{}, nil
	}
	return a.exportEMLTo(dir, msgs, a.fetchEMLSources), nil
}

// exportEMLTo fetches the sources folder by folder, so a selection of many
// messages in one mailbox shares one connection, and writes each into dir.
func (a *App) exportEMLTo(dir string, msgs []*storage.Message, fetch emlFetcher) EmlExportDTO {
	result := EmlExportDTO{Dir: dir}
	fail := func(m *storage.Message, err error) {
		result.Failed++
		if result.FirstError == "" {
			result.FirstError = err.Error()
		}
		a.log.Error("export eml", "id", m.ID, "err", err)
	}

	var order []int64
	groups := map[int64][]*storage.Message{}
	for _, m := range msgs {
		if _, seen := groups[m.FolderID]; !seen {
			order = append(order, m.FolderID)
		}
		groups[m.FolderID] = append(groups[m.FolderID], m)
	}
	for _, folderID := range order {
		group := groups[folderID]
		sources := fetch(group)
		for i, m := range group {
			if sources[i].err != nil {
				fail(m, sources[i].err)
				continue
			}
			if _, err := a.emlOptions(dir, m).Write(exportMeta(m), sources[i].raw); err != nil {
				fail(m, err)
				continue
			}
			result.Exported++
		}
	}
	return result
}

// fetchEMLSources reads the raw source of messages that share one folder over a
// single imap connection. Local Folders mail has no server copy to read.
func (a *App) fetchEMLSources(ms []*storage.Message) []emlSource {
	out := make([]emlSource, len(ms))
	failAll := func(err error) []emlSource {
		for i := range out {
			out[i].err = err
		}
		return out
	}
	if len(ms) == 0 {
		return out
	}

	account, err := a.store.GetAccount(a.ctx, ms[0].AccountID)
	if err != nil {
		return failAll(err)
	}
	if account.Local {
		return failAll(errors.New("pelton: Local Folders messages keep no original source to export"))
	}
	folder, err := a.store.GetFolder(a.ctx, ms[0].FolderID)
	if err != nil {
		return failAll(err)
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return failAll(err)
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return failAll(offlineOrErr(err))
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return failAll(offlineOrErr(err))
	}
	defer client.Logout()
	if _, err := client.Select(folder.IMAPPath); err != nil {
		return failAll(offlineOrErr(err))
	}
	for i, m := range ms {
		raw, err := client.FetchRawMessage(imap.UID(m.UID))
		if err != nil {
			out[i].err = offlineOrErr(err)
			continue
		}
		if len(raw) == 0 {
			out[i].err = errors.New("pelton: the message has no content to export")
			continue
		}
		out[i].raw = raw
	}
	return out
}

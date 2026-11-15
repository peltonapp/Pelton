package desktop

import (
	"path/filepath"
	"time"

	"github.com/peltonapp/Pelton/internal/mailexport"
)

// validSubfolderMode keeps an unknown mode from reaching the exporter, where it
// would silently mean "no subfolders".
func validSubfolderMode(mode string) string {
	switch mode {
	case mailexport.SubfoldersYear, mailexport.SubfoldersMonth:
		return mode
	default:
		return mailexport.SubfoldersNone
	}
}

// ChooseArchiveExportFolder opens a directory picker for the export-on-archive
// location and returns the chosen path, or empty when the user cancelled.
func (a *App) ChooseArchiveExportFolder() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.openDirectoryDialog(openDialogOptions{
		Title: "Choose where archived mail is saved",
	})
}

// PreviewArchiveExportName renders a file name template against a sample
// message, so the settings screen can show what the pattern produces without
// waiting for a real archive. The sample is fixed rather than taken from the
// mailbox: the preview must not depend on which message happens to be newest.
func (a *App) PreviewArchiveExportName(template, subfolders string) string {
	sample := mailexport.Meta{
		Date:      time.Date(2026, 3, 7, 14, 5, 9, 0, time.Local),
		Subject:   "Invoice 42",
		From:      "Acme Billing",
		MessageID: "<7c1f9@acme.example>",
	}
	options := mailexport.Options{Subfolders: validSubfolderMode(subfolders)}
	return filepath.Join(options.TargetDir(sample), mailexport.FileName(sample, template)+".eml")
}

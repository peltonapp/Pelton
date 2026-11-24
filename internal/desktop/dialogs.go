package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// fileFilter is one entry of a file dialog's type list: a label and a
// semicolon separated list of patterns, e.g. "*.pem;*.crt".
type fileFilter = application.FileFilter

// openDialogOptions describes a file or folder picker.
type openDialogOptions struct {
	Title   string
	Filters []fileFilter
	// Directory is where the dialog opens, empty for the system's choice.
	Directory string
	// ShowHidden lists hidden files and folders, which a mail program's
	// store often is, and opens folders macOS shows as packages.
	ShowHidden bool
}

// saveDialogOptions describes a save dialog.
type saveDialogOptions struct {
	Title           string
	DefaultFilename string
	Filters         []fileFilter
}

// The dialog helpers below return an empty path (or no paths) and no error
// when the user cancels, and the same before the app is running, so callers
// treat "" as "nothing chosen".

// openFileDialog asks for one existing file.
func (a *App) openFileDialog(o openDialogOptions) (string, error) {
	if a.wapp == nil {
		return "", nil
	}
	return a.wapp.Dialog.OpenFileWithOptions(a.openOptions(o, true, false)).PromptForSingleSelection()
}

// openMultipleFilesDialog asks for any number of existing files.
func (a *App) openMultipleFilesDialog(o openDialogOptions) ([]string, error) {
	if a.wapp == nil {
		return nil, nil
	}
	return a.wapp.Dialog.OpenFileWithOptions(a.openOptions(o, true, false)).PromptForMultipleSelection()
}

// openDirectoryDialog asks for a folder.
func (a *App) openDirectoryDialog(o openDialogOptions) (string, error) {
	if a.wapp == nil {
		return "", nil
	}
	return a.wapp.Dialog.OpenFileWithOptions(a.openOptions(o, false, true)).PromptForSingleSelection()
}

// saveFileDialog asks where to write a file.
func (a *App) saveFileDialog(o saveDialogOptions) (string, error) {
	if a.wapp == nil {
		return "", nil
	}
	opts := &application.SaveFileDialogOptions{
		Title:                o.Title,
		Filename:             o.DefaultFilename,
		Filters:              o.Filters,
		CanCreateDirectories: true,
	}
	if a.win != nil {
		opts.Window = a.win
	}
	return a.wapp.Dialog.SaveFileWithOptions(opts).PromptForSingleSelection()
}

func (a *App) openOptions(o openDialogOptions, files, dirs bool) *application.OpenFileDialogOptions {
	opts := &application.OpenFileDialogOptions{
		Title:                o.Title,
		Filters:              o.Filters,
		CanChooseFiles:       files,
		CanChooseDirectories: dirs,
		CanCreateDirectories: dirs,
		Directory:            o.Directory,
		ShowHiddenFiles:      o.ShowHidden,
		// an Apple Mail mailbox is a folder ending in .mbox, which macOS
		// would otherwise offer as a single file.
		TreatsFilePackagesAsDirectories: o.ShowHidden,
	}
	if a.win != nil {
		opts.Window = a.win
	}
	return opts
}

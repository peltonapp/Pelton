package desktop

// Default-mail-client integration (#119): let Pelton register as the system's
// mailto handler. The per-platform detect/set live in defaultclient_<os>.go;
// this file is the platform-independent binding surface. The design is
// deliberately quiet: onboarding offers it once and the About section shows a
// passive line, nothing nags.

// DefaultMailStatusDTO reports whether Pelton is the default mailto handler.
// Known is false when the platform cannot answer reliably; the frontend then
// shows nothing rather than guessing wrong.
type DefaultMailStatusDTO struct {
	Known     bool `json:"known"`
	IsDefault bool `json:"isDefault"`
}

// DefaultMailClientStatus reports whether Pelton is currently the default
// handler for mailto: links.
func (a *App) DefaultMailClientStatus() DefaultMailStatusDTO {
	isDefault, known := isDefaultMailHandler()
	return DefaultMailStatusDTO{Known: known, IsDefault: isDefault}
}

// SetDefaultMailClient asks the OS to make Pelton the default mailto handler.
// On macOS this shows the system's own confirmation sheet; on Linux it writes
// the xdg association; on Windows it opens the Settings "Default apps" page
// (Windows does not allow setting it programmatically).
func (a *App) SetDefaultMailClient() error {
	return setDefaultMailHandler()
}

// DefaultCalendarFileStatus reports whether Pelton opens .ics files. The
// installers list Pelton as an app that can open them without taking the
// default; this answers whether the user has made it the one.
func (a *App) DefaultCalendarFileStatus() DefaultMailStatusDTO {
	isDefault, known := isDefaultCalendarHandler()
	return DefaultMailStatusDTO{Known: known, IsDefault: isDefault}
}

// SetDefaultCalendarFileHandler asks the OS to open .ics files with Pelton,
// the same way SetDefaultMailClient does for mailto links.
func (a *App) SetDefaultCalendarFileHandler() error {
	return setDefaultCalendarHandler()
}

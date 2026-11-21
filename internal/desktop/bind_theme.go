package desktop

// SetWindowTheme matches the native window chrome (the Windows title/caption
// bar) to the app's resolved theme, so a dark ui does not sit under a light
// caption bar. It is a no-op on macOS and Linux, where the native chrome is
// not themed from here; the frontend calls it whenever the resolved theme
// changes.
func (a *App) SetWindowTheme(dark bool) {
	if dark {
		a.setNativeTheme(nativeThemeDark)
	} else {
		a.setNativeTheme(nativeThemeLight)
	}
}

// SystemColorScheme reports the operating system's dark/light preference as
// "dark" or "light", or "" when it cannot be determined. It exists because
// WebKitGTK on Linux does not surface the desktop color-scheme to CSS's
// prefers-color-scheme media query, so the frontend cannot detect dark mode on
// its own there; it consults this and falls back to the media query (which is
// correct on macOS and Windows) whenever this returns "".
func (a *App) SystemColorScheme() string {
	return detectSystemColorScheme()
}

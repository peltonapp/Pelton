//go:build linux

package desktop

import "os"

// WebKitGTK paints overlay scrollbars in a layer of their own above the page,
// so a scrollbar that was showing when a menu or the settings window opened
// stayed drawn on top of it, whatever the z-index (#497). Classic scrollbars
// are part of the page and stack like everything else. GTK reads this as it
// starts, and the web process inherits it, so it has to be set before either;
// a value the user set themselves is left alone.
func init() {
	if _, set := os.LookupEnv("GTK_OVERLAY_SCROLLING"); !set {
		_ = os.Setenv("GTK_OVERLAY_SCROLLING", "0")
	}
}

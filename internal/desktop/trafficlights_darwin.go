//go:build darwin

package desktop

/*
#cgo LDFLAGS: -framework Cocoa
void peltonTrafficLights(void *nsWindow, double *center, double *end);
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// measureTrafficLights reads where a window's traffic lights sit. It has to
// run on the main thread, which InvokeSync sees to.
func measureTrafficLights(w application.Window) TrafficLightsDTO {
	var out TrafficLightsDTO
	if w == nil {
		return out
	}
	application.InvokeSync(func() {
		ptr := w.NativeWindow()
		if ptr == nil {
			return
		}
		var center, end C.double
		C.peltonTrafficLights(ptr, &center, &end)
		out.Center = float64(center)
		out.End = float64(end)
	})
	return out
}

// insetComposeTitleBar gives a compose window of the new design a toolbar
// style title bar. That makes the bar as tall as the composer's own toolbar
// and puts the traffic lights on its centre line, instead of leaving them in
// the top corner above the buttons they sit beside.
func insetComposeTitleBar(o *application.WebviewWindowOptions) {
	o.Mac.TitleBar = application.MacTitleBarHiddenInsetUnified
}

//go:build !darwin

package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// measureTrafficLights is macOS only: other systems have no traffic lights to
// line anything up with.
func measureTrafficLights(application.Window) TrafficLightsDTO {
	return TrafficLightsDTO{}
}

func insetComposeTitleBar(*application.WebviewWindowOptions) {}

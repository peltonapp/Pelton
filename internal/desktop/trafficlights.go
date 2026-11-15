package desktop

// TrafficLightsDTO is where the macOS window buttons of a window sit, in
// points, so the page can line its own title bar content up with them. Both
// values are 0 when unknown or on another system.
type TrafficLightsDTO struct {
	// Center is how far below the window's top edge their centre line is.
	Center float64 `json:"center"`
	// End is how far from the window's leading edge the last button ends.
	End float64 `json:"end"`
}

// TrafficLights reports where the window buttons sit in the main window, or
// in the compose window opened with the given token.
func (a *App) TrafficLights(composeToken string) TrafficLightsDTO {
	if composeToken == "" {
		if a.win == nil {
			return TrafficLightsDTO{}
		}
		return measureTrafficLights(a.win)
	}
	if a.wapp == nil {
		return TrafficLightsDTO{}
	}
	w, ok := a.wapp.Window.GetByName("compose-" + composeToken)
	if !ok {
		return TrafficLightsDTO{}
	}
	return measureTrafficLights(w)
}

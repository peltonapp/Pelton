package desktop

// SenderReadingDTO is how the reader shows one sender's mail: Colors is
// "adapt", "original" or empty to follow the global setting, FontSize the
// reader text size in px or 0 to follow the global size.
type SenderReadingDTO struct {
	Address  string `json:"address"`
	Colors   string `json:"colors"`
	FontSize int    `json:"fontSize"`
}

// ListSenderReading returns every sender with a remembered reading choice.
func (a *App) ListSenderReading() ([]SenderReadingDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	rows, err := a.store.ListSenderReading(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SenderReadingDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, SenderReadingDTO{Address: r.Address, Colors: r.Colors, FontSize: r.FontSize})
	}
	return out, nil
}

// SetSenderColors remembers whether html mail from address adapts to a dark
// theme ("adapt") or keeps its own colours ("original"); "" forgets it.
func (a *App) SetSenderColors(address, mode string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSenderColors(a.ctx, address, mode)
}

// SetSenderFontSize remembers the reader text size for address in px; 0
// forgets it.
func (a *App) SetSenderFontSize(address string, size int) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSenderFontSize(a.ctx, address, size)
}

// ResetSenderReading forgets every remembered sender reading choice and
// returns how many senders there were.
func (a *App) ResetSenderReading() (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.store.ResetSenderReading(a.ctx)
}

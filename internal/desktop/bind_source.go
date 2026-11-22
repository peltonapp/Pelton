package desktop

// GetMessageSource returns a message's raw RFC 822 source (all headers plus
// body, exactly as fetched). The messages table only caches the parsed
// plain/html bodies, so the raw bytes are fetched on demand over imap; nothing
// is stored.
func (a *App) GetMessageSource(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return "", err
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return "", err
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return "", err
	}
	return a.protocolFor(*account).source(*m, *folder, *account)
}

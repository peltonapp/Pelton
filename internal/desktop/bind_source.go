package desktop

import (
	"github.com/emersion/go-imap/v2"
)

// GetMessageSource returns a message's raw RFC 822 source (all headers plus
// body, exactly as fetched). The messages table only caches the parsed
// plain/html bodies, so the raw bytes are fetched on demand over imap; nothing
// is stored.
func (a *App) GetMessageSource(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	m, err := a.store.GetMessage(a.mailCtx(), id)
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
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return "", err
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return "", offlineOrErr(err)
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return "", offlineOrErr(err)
	}
	defer client.Logout()
	if _, err := client.Select(folder.IMAPPath); err != nil {
		return "", offlineOrErr(err)
	}

	raw, err := client.FetchRawMessage(imap.UID(m.UID))
	if err != nil {
		return "", offlineOrErr(err)
	}
	return string(raw), nil
}

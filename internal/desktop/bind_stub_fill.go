package desktop

import (
	"context"
	"errors"
	"fmt"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// The sync engine stores header stubs (body_complete = 0) and fills their
// bodies afterwards. The reading pane and "download for offline use" here
// still expect a stored message to have its body, so they fill a stub first,
// synchronously, through ensureBody. Stubs-first reading (on-demand body
// fetch, mail:updated) replaces this later in the stack.
//
// The fill is the engine's own body fill (Engine.FetchBodies), the one the
// sync and the later on-demand fetch use: the fetch carries the folder's
// stored UIDVALIDITY, so a changed one fills nothing; it only updates the
// existing stub, so a stub moved or deleted meanwhile stays gone; and it
// stores the whole fetched message (headers, authentication results,
// unsubscribe, charset, signatures, the source of protected mail).

// errBodyUnavailable wraps a stub whose body could not be fetched, so the
// reading pane shows a clear, retryable error instead of an empty message.
// The frontend recognises its text (lib/bodyfetcherror.ts): it is not "the
// message is gone".
var errBodyUnavailable = errors.New("pelton: this message could not be downloaded from the server")

// ensureBody returns m with its body: a stub is fetched on a short session of
// its own and stored in place, then read back. A local account's message is
// returned as it is. A stub that left its folder meanwhile returns
// storage.ErrMessageNotFound.
func (a *App) ensureBody(m *storage.Message) (*storage.Message, error) {
	if m == nil || m.BodyComplete {
		return m, nil
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return nil, err
	}
	if account.Local {
		return m, nil
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return nil, err
	}
	fillErr := a.fillStubDirect(*account, *folder, m.RemoteID)
	filled, err := a.store.GetMessage(a.ctx, m.ID)
	if err != nil {
		return nil, err
	}
	if !filled.BodyComplete {
		if fillErr == nil {
			fillErr = errors.New("the server no longer has it")
		}
		return nil, fmt.Errorf("%w: %w", errBodyUnavailable, fillErr)
	}
	goSafe("indexing new mail", func() { _ = a.indexNewMessages() })
	return filled, nil
}

// fillStubDirect fetches one stub's body over a direct session. It does not
// take syncMu: the user waits for it, and a long sync holds that lock. Like
// IDLE's session it is one more login beside the sync's, for one FETCH.
func (a *App) fillStubDirect(account storage.Account, folder storage.Folder, remoteID string) error {
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return err
	}
	client, err := a.connectIMAP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return err
	}
	defer client.Logout()
	return a.fillStubOnSession(a.ctx, client, folder, remoteID)
}

// fillStubOnSession fills the stub remoteID of folder with the engine's body
// fill on client. folder must be the stored row: its UIDVALIDITY is the
// generation the fetch checks. No progress line, no mail:new.
func (a *App) fillStubOnSession(ctx context.Context, client mailClient, folder storage.Folder, remoteID string) error {
	engine := a.newSyncEngine(pimap.NewAdapter(client), folder.AccountID)
	engine.OnProgress = nil
	engine.OnStored = nil
	_, err := engine.FetchBodies(ctx, folder, []string{remoteID})
	if bad, ok := errors.AsType[*psync.MessageError](err); ok {
		a.log.Warn("message could not be read", "folder", folder.Name, "ids", bad.RemoteIDs, "err", bad.Err)
	}
	return err
}

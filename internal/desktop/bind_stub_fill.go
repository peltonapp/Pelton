package desktop

import (
	"context"
	"errors"
	"fmt"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
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

// ensureBody returns m with its body: a stub is fetched now, on the
// account's pool, and stored in place, then read back. A local account's message is
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
	fillErr := a.fillStubDirect(*account, *folder, *m)
	if errors.Is(fillErr, errAccountSyncHeld) {
		// being removed: the stub stays as it is until the account goes.
		return m, nil
	}
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

// fillStubDirect fetches one stub's body on the account's pool and waits for
// it: a fast-lane task (KindOnDemandBody), or, for a message larger than the
// account's chunk size, the bounded large-body path (fillLargeStub). Each
// server operation counts toward the throttle once, from the unit that ran
// it (noteUnitOutcome): callers that joined one task (an equal key) share
// its count, and a stale task that fetched nothing counts nothing.
// KindOnDemandBody is a kind whose unit reports its own outcome, and this
// bridge is that unit until stubs-first reading replaces it.
func (a *App) fillStubDirect(account storage.Account, folder storage.Folder, m storage.Message) error {
	var err, unreadable error
	if rt, rerr := a.ensureAccountSync(a.ctx, account.ID); rerr == nil && m.SizeBytes > rt.sched.ChunkBudget().Get() {
		err = a.fillLargeStub(account, folder, m.ID, m.RemoteID, &unreadable)
	} else {
		err = a.runNowKey(a.ctx, account, syncsched.KindOnDemandBody, folder.ID, fmt.Sprintf("open:%d", m.ID),
			func(ctx context.Context, c mailClient) error {
				// A queued open is stale once the body arrived another way,
				// or the message left the folder.
				need, err := a.store.MessageNeedsBody(ctx, m.ID)
				if errors.Is(err, storage.ErrMessageNotFound) || (err == nil && !need) {
					return nil
				}
				err = a.fillStubOnSession(ctx, c, folder, m.RemoteID)
				a.noteUnitOutcome(account.ID, err)
				if _, ok := errors.AsType[*psync.MessageError](err); ok {
					// unreadable mail: the session is fine, so the task
					// does not fail for it; the stub stays a stub.
					unreadable = err
					return nil
				}
				return err
			})
	}
	if err == nil {
		err = unreadable
	}
	return err
}

// fillLargeStub fetches an opened stub larger than the chunk size in the
// normal lane, one chunk per unit (KindOpenedBody: the top class, waiting
// for no listing or body work of its folder), so it never holds a fast-lane
// session for a long download. The caller waits for the last unit.
// A message that cannot be read is put in *unreadable, not failed.
func (a *App) fillLargeStub(account storage.Account, folder storage.Folder, msgID int64, remoteID string, unreadable *error) error {
	part := psync.NewPartialBody(remoteID)
	return a.runPooled(a.ctx, account, poolTask{
		Kind: syncsched.KindOpenedBody, FolderID: folder.ID,
		Key: fmt.Sprintf("open:%d", msgID),
		Run: func(ctx context.Context, c mailClient) error {
			if need, err := a.store.MessageNeedsBody(ctx, msgID); err != nil || !need {
				return nil
			}
			rt := a.accountSync(account.ID)
			if rt == nil {
				return errAccountSyncHeld
			}
			engine := a.newSyncEngine(pimap.NewAdapter(c), account.ID)
			engine.OnProgress = nil
			engine.OnStored = nil
			done, res, err := engine.FetchBodyChunk(ctx, folder, part, rt.sched.ChunkBudget().Get())
			// One chunk is one FETCH on this task's session; joined callers
			// share this unit, so it counts once.
			a.noteUnitOutcome(account.ID, err)
			if _, ok := errors.AsType[*psync.MessageError](err); ok {
				*unreadable = err
				return nil
			}
			if err != nil {
				return err
			}
			if !done {
				return syncsched.ErrAgain
			}
			if res.New > 0 {
				a.afterStoredBodies()
			}
			return nil
		},
	})
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

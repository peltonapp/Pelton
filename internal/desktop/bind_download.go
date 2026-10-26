package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// maxPreviewBytes caps how large an attachment we will stream to the ui for the
// in-app preview. Above this we tell the ui to offer "open externally" instead of
// pushing tens of megabytes of base64 across the bridge.
const maxPreviewBytes = 25 << 20 // 25 MiB

// downloadActive guards against two bulk downloads running at once.
var downloadActive atomic.Bool

// AttachmentContentDTO carries one attachment's bytes to the previewer. Data is
// base64 so it crosses the bindings as a plain string. TooLarge is set (with no
// Data) when the file exceeds the preview cap.
type AttachmentContentDTO struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Data        string `json:"data"`
	TooLarge    bool   `json:"tooLarge"`
}

// ReadAttachment returns an attachment's bytes for the in-app previewer. messageID
// scopes the lookup so an id cannot reach another message's files.
func (a *App) ReadAttachment(messageID, attachmentID int64) (AttachmentContentDTO, error) {
	if err := a.ready(); err != nil {
		return AttachmentContentDTO{}, err
	}
	target, err := a.findAttachment(messageID, attachmentID)
	if err != nil {
		return AttachmentContentDTO{}, err
	}
	dto := AttachmentContentDTO{
		Filename:    target.Filename,
		ContentType: target.ContentType,
		SizeBytes:   target.SizeBytes,
	}
	if target.SizeBytes > maxPreviewBytes {
		dto.TooLarge = true
		return dto, nil
	}
	rc, err := a.store.OpenAttachment(target.DiskPath)
	if err != nil {
		return AttachmentContentDTO{}, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return AttachmentContentDTO{}, err
	}
	dto.Data = base64.StdEncoding.EncodeToString(data)
	return dto, nil
}

// findAttachment resolves one attachment row within a message.
func (a *App) findAttachment(messageID, attachmentID int64) (*storage.Attachment, error) {
	atts, err := a.store.ListAttachments(a.ctx, messageID)
	if err != nil {
		return nil, err
	}
	for i := range atts {
		if atts[i].ID == attachmentID {
			return &atts[i], nil
		}
	}
	return nil, fmt.Errorf("pelton: attachment %d not found", attachmentID)
}

// SaveAllAttachments prompts for a directory and writes every non-inline
// attachment of a message there, emitting progress. It returns the chosen
// directory (empty if cancelled).
func (a *App) SaveAllAttachments(messageID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	atts, err := a.store.ListAttachments(a.ctx, messageID)
	if err != nil {
		return "", err
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Save all attachments to folder"})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}

	total := len(atts)
	defer a.emit(EventAttachmentProgress, AttachmentProgressEvent{Running: false, FilesDone: total, FilesTotal: total})
	for i, att := range atts {
		dest := uniqueDestPath(dir, att.Filename)
		if err := a.copyAttachmentProgress(att, dest, i, total); err != nil {
			a.emit(EventAttachmentProgress, AttachmentProgressEvent{Running: false, Error: err.Error(), FilesDone: i, FilesTotal: total})
			return "", err
		}
	}
	return dir, nil
}

// copyAttachmentProgress streams one attachment to dest, emitting byte progress
// so the ui can show a bar even though the source is a local cached file.
func (a *App) copyAttachmentProgress(att storage.Attachment, dest string, fileIndex, filesTotal int) error {
	src, err := a.store.OpenAttachment(att.DiskPath)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(filepath.Clean(dest))
	if err != nil {
		return err
	}
	defer out.Close()

	pw := &progressWriter{
		total:    att.SizeBytes,
		filename: att.Filename,
		fileIdx:  fileIndex,
		files:    filesTotal,
		emit:     a.emit,
	}
	if _, err := io.Copy(io.MultiWriter(out, pw), src); err != nil {
		return err
	}
	return nil
}

// progressWriter counts bytes copied and emits attachment progress events. It
// throttles to at most one event per ~64 KiB so a stream of tiny writes does not
// flood the event bus.
type progressWriter struct {
	total    int64
	written  int64
	lastEmit int64
	filename string
	fileIdx  int
	files    int
	emit     func(string, any)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.written += int64(n)
	if w.written-w.lastEmit >= 64<<10 || w.written == w.total {
		w.lastEmit = w.written
		w.emit(EventAttachmentProgress, AttachmentProgressEvent{
			Running:    true,
			Filename:   w.filename,
			BytesDone:  w.written,
			BytesTotal: w.total,
			FilesDone:  w.fileIdx,
			FilesTotal: w.files,
		})
	}
	return n, nil
}

// DownloadRange downloads every message from startDateRFC3339 to today that is
// not already cached, across all accounts and folders, and pins them offline for
// fast local search. includeAttachments controls whether attachment bytes are
// persisted for messages not yet in the cache; a cached stub always gets its
// attachments. Progress (percent + eta) is emitted for the status bar.
func (a *App) DownloadRange(startDateRFC3339 string, includeAttachments bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	since, err := time.Parse(time.RFC3339, startDateRFC3339)
	if err != nil {
		// tolerate a plain date (YYYY-MM-DD) from a date picker with no time.
		since, err = time.Parse("2006-01-02", startDateRFC3339)
		if err != nil {
			return fmt.Errorf("pelton: invalid start date %q: %w", startDateRFC3339, err)
		}
	}
	if a.lowPowerMode() {
		return errors.New("pelton: low power mode is on; turn it off to start a bulk download")
	}
	if !downloadActive.CompareAndSwap(false, true) {
		return errors.New("pelton: a download is already running")
	}

	// remember the attachment choice as the default for next time.
	_ = a.store.SetBool(a.ctx, settingDownloadAtts, includeAttachments)
	// remember the range itself so a restart mid-download can pick back up
	// instead of silently dropping the job (see ResumePendingDownload).
	_ = a.store.Set(a.ctx, settingDownloadPending, since.Format(time.RFC3339))

	// run the whole job on a background goroutine so the bound call returns
	// immediately and neither the ui nor the go caller waits on imap. progress
	// and completion are reported entirely through events. the job gets its own
	// cancellable context so CancelDownload can stop it without shutting the app.
	ctx := a.beginDownload()
	goSafe("downloading mail for offline use", func() { a.runRangeDownload(ctx, since, includeAttachments) })
	return nil
}

// beginDownload derives a cancellable context from the app context for a bulk
// download and stores its cancel func so CancelDownload can reach it.
func (a *App) beginDownload() context.Context {
	ctx, cancel := context.WithCancel(a.ctx)
	a.dlMu.Lock()
	a.dlCancel = cancel
	a.dlMu.Unlock()
	return ctx
}

// endDownload releases the stored cancel func once a job returns.
func (a *App) endDownload() {
	a.dlMu.Lock()
	if a.dlCancel != nil {
		a.dlCancel()
		a.dlCancel = nil
	}
	a.dlMu.Unlock()
}

// CancelDownload stops a running bulk offline download and clears its resume
// marker so it does not restart on the next launch. A no-op if none is running.
func (a *App) CancelDownload() {
	a.dlMu.Lock()
	cancel := a.dlCancel
	a.dlMu.Unlock()
	if cancel == nil {
		return
	}
	// clear the marker first so the shutdown-vs-cancel check in runRangeDownload
	// cannot race a resume back in.
	_ = a.store.Set(a.ctx, settingDownloadPending, "")
	cancel()
}

// ResumePendingDownload restarts a bulk download that was still running when
// the app last closed. planDownload/planAccount already skip anything that has
// its body, so replaying the same range only fetches whatever the previous run
// had not gotten to yet. Called once from startup; a no-op if nothing was pending.
func (a *App) ResumePendingDownload() {
	raw, err := a.store.Get(a.ctx, settingDownloadPending)
	if err != nil || raw == "" {
		return
	}
	since, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		_ = a.store.Set(a.ctx, settingDownloadPending, "")
		return
	}
	if a.lowPowerMode() {
		return
	}
	includeAttachments := a.boolSetting(settingDownloadAtts, true)
	if !downloadActive.CompareAndSwap(false, true) {
		return
	}
	ctx := a.beginDownload()
	goSafe("downloading mail for offline use", func() { a.runRangeDownload(ctx, since, includeAttachments) })
}

// runRangeDownload performs the plan-and-fetch passes off the calling goroutine.
// The resume marker is only cleared when the job actually finishes or is stopped
// by the user (CancelDownload). If it stops because the app is shutting down
// (a.ctx cancelled) the marker is left in place so ResumePendingDownload picks
// it up next launch, which is what makes an interrupted download continue.
func (a *App) runRangeDownload(ctx context.Context, since time.Time, includeAttachments bool) {
	defer downloadActive.Store(false)
	defer a.endDownload()

	// clearIfNotShutdown drops the resume marker unless the app is shutting down;
	// on shutdown we keep it so the job resumes on the next launch.
	clearIfNotShutdown := func() {
		if a.ctx.Err() == nil {
			_ = a.store.Set(a.ctx, settingDownloadPending, "")
		}
	}

	a.emit(EventDownloadProgress, DownloadProgressEvent{Running: true, Label: "Scanning"})
	tasks, pin, err := a.planDownload(ctx, since)
	if err != nil {
		clearIfNotShutdown()
		a.emit(EventDownloadProgress, DownloadProgressEvent{Running: false, Error: err.Error()})
		return
	}
	total := len(tasks)
	if total == 0 && len(pin) == 0 {
		clearIfNotShutdown()
		a.emit(EventDownloadProgress, DownloadProgressEvent{Running: false, Label: "Nothing to download"})
		return
	}

	a.emit(EventDownloadProgress, DownloadProgressEvent{Running: true, Total: total, Label: "Starting"})
	if err := a.runDownload(ctx, tasks, pin, includeAttachments, total); err != nil {
		clearIfNotShutdown()
		a.emit(EventDownloadProgress, DownloadProgressEvent{Running: false, Error: err.Error()})
		return
	}
	clearIfNotShutdown()
	a.emit(EventDownloadProgress, DownloadProgressEvent{Running: false, Done: total, Total: total, Percent: 100, Label: "Done"})
}

// dlTask is one message to fetch, paired with the folder it belongs to.
// hadRow records whether the message had a row (a stub) at plan time: a stub
// is only ever filled, so one moved or archived meanwhile is not stored
// again.
type dlTask struct {
	folder   storage.Folder
	remoteID string
	hadRow   bool
}

// planDownload lists, across every account, the messages since the cutoff that
// still need a body (tasks) and the cached ones that only need pinning (pin).
// It is the cheap counting pass that lets the fetch pass report an accurate
// percentage and eta.
func (a *App) planDownload(ctx context.Context, since time.Time) ([]dlTask, []int64, error) {
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, nil, err
	}
	var (
		tasks []dlTask
		pin   []int64
	)
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		accTasks, accPin, err := a.planAccount(ctx, account, since)
		if err != nil {
			if errors.Is(err, errNoCredentials) {
				continue
			}
			a.log.Error("plan download", "account", account.Email, "err", err)
			continue
		}
		tasks = append(tasks, accTasks...)
		pin = append(pin, accPin...)
	}
	return tasks, pin, nil
}

// planAccount lists, for one account, the messages since the cutoff that still
// need a body (tasks) and the ones already complete that only need pinning.
// IMAP asks the server, since a folder may hold mail older than what sync
// listed.
func (a *App) planAccount(ctx context.Context, account storage.Account, since time.Time) ([]dlTask, []int64, error) {
	folders, err := a.store.ListFolders(a.ctx, account.ID)
	if err != nil {
		return nil, nil, err
	}
	// an unchecked folder is not synced, so downloading it for offline use
	// would fetch mail the user asked not to keep up to date (#173).
	folders = slices.DeleteFunc(folders, func(f storage.Folder) bool { return f.SyncExcluded })
	return a.protocolFor(account).planDownload(ctx, account, folders, since)
}

// planIMAPAccount searches each folder on the server for uids since the cutoff
// and checks them against the cache: a uid missing or without its body is a
// task, a complete one is pinned. Each folder is one normal-lane task.
func (a *App) planIMAPAccount(ctx context.Context, account storage.Account, folders []storage.Folder, since time.Time) ([]dlTask, []int64, error) {
	var (
		mu    sync.Mutex
		tasks []dlTask
		pin   []int64
	)
	err := a.runFolderTasks(ctx, account, syncsched.KindOffline, "offline-plan", folders,
		func(_ context.Context, client mailClient, folder storage.Folder) error {
			if ctx.Err() != nil {
				return nil
			}
			ft, fp, err := a.planIMAPFolder(client, folder, since)
			mu.Lock()
			tasks, pin = append(tasks, ft...), append(pin, fp...)
			mu.Unlock()
			return err
		})
	if err != nil {
		return nil, nil, err
	}
	return tasks, pin, nil
}

// planIMAPFolder is one folder of planIMAPAccount. A folder the server
// refuses is logged and skipped.
func (a *App) planIMAPFolder(client mailClient, folder storage.Folder, since time.Time) ([]dlTask, []int64, error) {
	if _, err := client.Select(folder.IMAPPath); err != nil {
		a.log.Error("plan select", "folder", folder.IMAPPath, "err", err)
		return nil, nil, nil
	}
	uids, err := client.SearchSince(since)
	if err != nil {
		a.log.Error("plan search", "folder", folder.IMAPPath, "err", err)
		return nil, nil, nil
	}
	// the server's SINCE has already applied the cutoff, so every cached row
	// is a candidate match regardless of its stored date.
	states, err := a.store.MessageBodyStates(a.ctx, folder.ID, time.Time{})
	if err != nil {
		return nil, nil, err
	}
	have := make(map[uint32]storage.MessageBodyState, len(states))
	for _, s := range states {
		have[s.UID] = s
	}
	var (
		tasks []dlTask
		pin   []int64
	)
	for _, uid := range uids {
		if s, cached := have[uint32(uid)]; cached && s.BodyComplete {
			pin = append(pin, s.ID)
			continue
		}
		_, hadRow := have[uint32(uid)]
		tasks = append(tasks, dlTask{folder: folder, remoteID: strconv.FormatUint(uint64(uid), 10), hadRow: hadRow})
	}
	return tasks, pin, nil
}

// runFolderTasks queues one normal-lane task per folder, keyed by prefix and
// folder id, and waits for all of them. run may return syncsched.ErrAgain
// for its next unit. A local error is the run's to return; it ends that
// folder only. The first error comes back.
func (a *App) runFolderTasks(ctx context.Context, account storage.Account, kind syncsched.Kind, prefix string, folders []storage.Folder, run func(ctx context.Context, c mailClient, folder storage.Folder) error) error {
	errs := make(chan error, len(folders))
	queued := 0
	for _, f := range folders {
		err := a.enqueuePooled(ctx, account, poolTask{
			Kind: kind, FolderID: f.ID, Key: fmt.Sprintf("%s:%d", prefix, f.ID),
			Run: func(ctx context.Context, c mailClient) error { return run(ctx, c, f) },
		}, func(err error) { errs <- a.stoppedErr(account.ID, err) })
		if err != nil {
			return err
		}
		queued++
	}
	var first error
	for range queued {
		select {
		case err := <-errs:
			if first == nil {
				first = err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return first
}

// runDownload pins the messages that already have their body, then fetches the
// planned ones account by account, storing and pinning each, and emits progress
// with percent and a running eta.
func (a *App) runDownload(ctx context.Context, tasks []dlTask, pin []int64, includeAttachments bool, total int) error {
	for _, id := range pin {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.store.SetOffline(a.ctx, id, true); err != nil {
			a.log.Error("download pin", "id", id, "err", err)
		}
	}

	prog := &dlProgress{total: total, start: time.Now()}
	for accountID, accTasks := range groupByAccount(tasks) {
		if err := ctx.Err(); err != nil {
			return err
		}
		account, err := a.store.GetAccount(a.ctx, accountID)
		if err != nil {
			a.log.Error("download get account", "id", accountID, "err", err)
			prog.add(len(accTasks))
			continue
		}
		if err := a.downloadAccount(ctx, *account, accTasks, includeAttachments, prog); err != nil {
			if errors.Is(err, errNoCredentials) {
				prog.add(len(accTasks))
				continue
			}
			if errors.Is(err, errAccountSyncHeld) {
				a.log.Info("download stopped: mailbox is being removed or was removed", "account", account.Email)
				continue
			}
			a.log.Error("download account", "account", account.Email, "err", err)
		}
	}
	return nil
}

// dlProgress counts a bulk download's messages across the tasks fetching
// them, which may run at once on several sessions.
type dlProgress struct {
	mu    sync.Mutex
	done  int
	total int
	start time.Time
}

// add counts n more messages and returns the new total done.
func (p *dlProgress) add(n int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	return p.done
}

// downloadAccount fetches one account's tasks, folder by folder in batches,
// storing each message the way sync does and pinning it.
func (a *App) downloadAccount(ctx context.Context, account storage.Account, tasks []dlTask, includeAttachments bool, prog *dlProgress) error {
	return a.protocolFor(account).download(ctx, account, groupByFolder(tasks), includeAttachments, prog)
}

// offlineUnit is one unit of a folder's offline download on the task's
// session: one batch, sized by the account's adaptive count and byte budget.
// It returns syncsched.ErrAgain while the folder has more.
//
// Stubs planned with a row go first. Each unit asks the store which of them
// still need a body, so one filled, moved or deleted meanwhile is dropped,
// and fills them the way the body generator does: fill only, never an
// insert. Messages that had no row at plan time come after; they are stored
// with the attachment choice, or fill a stub a sync listed meanwhile.
//
// The unit claims what it fetches from the account's body claims, shared
// with the folder's body generator, so no message is fetched by both. Ids
// the generator holds stay planned: a later unit finds them filled and only
// pins them. When everything left is claimed it returns
// syncsched.ErrSaturated, and runs again once a unit of the folder ended.
func (a *App) offlineUnit(ctx context.Context, client mailClient, account storage.Account, ft *folderTasks, includeAttachments bool, prog *dlProgress) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	shared := a.accountShared(account.ID)
	if shared == nil {
		return errAccountSyncHeld
	}
	claims := &shared.bodyClaims
	folder, err := a.store.GetFolder(ctx, ft.folder.ID)
	if errors.Is(err, storage.ErrFolderNotFound) {
		a.emitDownloadProgress(prog.add(len(ft.stubs)+len(ft.fresh)), prog.total, prog.start, account.Email)
		return nil
	}
	if err != nil {
		return err
	}
	count, budget := syncsched.DefaultBodyCount, int64(syncsched.DefaultBodyBytes)
	var limits *syncsched.BodyLimits
	if rt := a.accountSync(account.ID); rt != nil {
		limits = rt.sched.BodyLimits()
		count, budget = limits.Get()
	}
	started := time.Now()
	var full, busy bool
	var done int
	if len(ft.stubs) > 0 {
		done, full, busy, err = a.offlineStubs(ctx, account, *folder, ft, claims, count, budget, includeAttachments)
	}
	// The messages with no row when the stubs are done, or all claimed.
	if err == nil && len(ft.fresh) > 0 && (len(ft.stubs) == 0 || busy) {
		var n int
		n, full, busy, err = a.offlineFresh(ctx, client, account, *folder, ft, claims, count, includeAttachments)
		done += n
	}
	switch {
	case errors.Is(err, psync.ErrGenerationChanged):
		// The planned uids may name other mail now: drop the folder's rest.
		a.log.Warn("download: folder changed on the server, skipping the rest", "folder", folder.Name)
		done += len(ft.stubs) + len(ft.fresh)
		ft.stubs, ft.fresh = nil, nil
		err = nil
	case err != nil:
		ft.failures++
		if ft.failures < syncStepAttempts && ctx.Err() == nil {
			return retryUnit(err)
		}
		return err
	}
	ft.failures = 0
	if limits != nil && done > 0 {
		limits.Observe(time.Since(started), full)
	}
	a.emitDownloadProgress(prog.add(done), prog.total, prog.start, account.Email)
	switch {
	case len(ft.stubs)+len(ft.fresh) == 0:
		return nil
	case busy:
		return syncsched.ErrSaturated
	}
	return syncsched.ErrAgain
}

// offlineStubs fills one batch of the folder's planned stubs and pins them.
// A stub inside the sync window is filled with its attachments whatever the
// choice, as the body generator would fill it: nothing fetches them later.
// Without attachments a stub below the window ends body-complete with no
// attachment files, like a message stored with no row.
// It returns how many planned stubs it settled, whether the batch was cut
// by the count or the budget, and busy when every stub left is claimed by
// another unit.
func (a *App) offlineStubs(ctx context.Context, account storage.Account, folder storage.Folder, ft *folderTasks, claims *syncsched.Claims, count int, budget int64, includeAttachments bool) (settled int, full, busy bool, err error) {
	need, err := a.store.RemoteIDsNeedingBody(ctx, folder.ID, ft.stubs)
	if err != nil {
		return 0, false, false, err
	}
	needSet := make(map[string]struct{}, len(need))
	for _, id := range need {
		needSet[id] = struct{}{}
	}
	// Filled meanwhile: pinned; moved or deleted: gone from the plan.
	ft.stubs = slices.DeleteFunc(ft.stubs, func(id string) bool {
		if _, ok := needSet[id]; ok {
			return false
		}
		settled++
		a.pinOffline(ctx, folder, id)
		return true
	})
	if len(ft.stubs) == 0 {
		return settled, false, false, nil
	}
	free := slices.DeleteFunc(slices.Clone(ft.stubs), func(id string) bool { return claims.Claimed(folder.ID, id) })
	if len(free) == 0 {
		return settled, false, true, nil
	}
	sizes, err := a.store.MessageSizesByRemoteID(ctx, folder.ID, free[:min(count, len(free))])
	if err != nil {
		return settled, false, false, err
	}
	batch, full := syncsched.PlanBatchFull(free, func(id string) int64 { return sizes[id] }, count, budget)
	tok, batch := claims.Take(folder.ID, batch, len(batch))
	defer claims.Release(folder.ID, tok)
	if len(batch) == 0 {
		return settled, false, true, nil
	}
	with, without := batch, []string(nil)
	if !includeAttachments {
		floorUID, err := a.store.FolderSyncFloorUID(ctx, folder.ID)
		if err != nil {
			return settled, full, false, err
		}
		with, without = nil, nil
		for _, id := range batch {
			if inSyncWindow(id, floorUID) {
				with = append(with, id)
			} else {
				without = append(without, id)
			}
		}
	}
	for _, part := range []struct {
		ids         []string
		attachments bool
	}{{with, true}, {without, false}} {
		if len(part.ids) == 0 {
			continue
		}
		if err := a.fillIMAPBodies(ctx, account, folder, part.ids, true, false, part.attachments); err != nil {
			return settled, full, false, err
		}
	}
	for _, id := range batch {
		a.pinOffline(ctx, folder, id)
	}
	ft.stubs = withoutIDs(ft.stubs, batch)
	return settled + len(batch), full, false, nil
}

// withoutIDs is ids less the ones in drop, in order.
func withoutIDs(ids, drop []string) []string {
	gone := make(map[string]struct{}, len(drop))
	for _, id := range drop {
		gone[id] = struct{}{}
	}
	return slices.DeleteFunc(ids, func(id string) bool {
		_, ok := gone[id]
		return ok
	})
}

// offlineFresh fetches one batch of the folder's planned messages that had
// no row, stores and pins them. It returns how many it settled, whether the
// batch was the full count, and busy when every one left is claimed by
// another unit (a sync listed it and its body generator is fetching it).
// One a sync listed and its body generator filled meanwhile is only pinned:
// the claim taken first keeps that check true until the unit ends.
func (a *App) offlineFresh(ctx context.Context, client mailClient, account storage.Account, folder storage.Folder, ft *folderTasks, claims *syncsched.Claims, count int, includeAttachments bool) (int, bool, bool, error) {
	tok, chunk := claims.Take(folder.ID, ft.fresh, count)
	defer claims.Release(folder.ID, tok)
	if len(chunk) == 0 {
		return 0, false, true, nil
	}
	fetch, err := a.offlineUnfilled(ctx, folder, chunk)
	if err != nil {
		return 0, false, false, err
	}
	if len(fetch) == 0 {
		ft.fresh = withoutIDs(ft.fresh, chunk)
		return len(chunk), false, false, nil
	}
	ctx = psync.WithGeneration(ctx, folder.RemoteID, folderGeneration(folder))
	fetched, err := pimap.NewAdapter(client).Fetch(ctx, folder.RemoteID, fetch)
	if err != nil && !psync.IsSoftError(err) {
		return 0, false, false, err
	}
	if err != nil {
		a.log.Warn("download: messages could not be read", "folder", folder.Name, "err", err)
	}
	for _, msg := range fetched {
		id, err := a.storeOffline(folder, msg, includeAttachments)
		if errors.Is(err, storage.ErrBodyTargetChanged) {
			// The folder was reset while the message was fetched: its uid
			// names other mail now. Nothing was stored; a later sync lists it.
			a.log.Warn("download: folder reset meanwhile, message skipped", "folder", folder.Name, "remote_id", msg.RemoteID)
			continue
		}
		if err != nil {
			a.log.Error("download store", "remote_id", msg.RemoteID, "err", err)
			continue
		}
		if err := a.store.SetOffline(a.ctx, id, true); err != nil {
			a.log.Error("download pin", "id", id, "err", err)
		}
	}
	ft.fresh = withoutIDs(ft.fresh, chunk)
	return len(chunk), len(chunk) == count, false, nil
}

// offlineUnfilled is ids less the ones stored with their body by now (a
// sync listed them after the plan and its body generator filled them),
// which it pins.
func (a *App) offlineUnfilled(ctx context.Context, folder storage.Folder, ids []string) ([]string, error) {
	var out []string
	for _, remoteID := range ids {
		id, err := a.store.MessageIDByRemoteID(ctx, folder.ID, remoteID)
		if errors.Is(err, storage.ErrMessageNotFound) {
			out = append(out, remoteID)
			continue
		}
		if err != nil {
			return nil, err
		}
		need, err := a.store.MessageNeedsBody(ctx, id)
		if err != nil {
			return nil, err
		}
		if need {
			out = append(out, remoteID)
			continue
		}
		if err := a.store.SetOffline(a.ctx, id, true); err != nil {
			a.log.Error("download pin", "id", id, "err", err)
		}
	}
	return out, nil
}

// folderGeneration is the folder's stored generation as the adapter compares
// it (psync.WithGeneration): its UIDVALIDITY, empty when unknown.
func folderGeneration(folder storage.Folder) string {
	if folder.UIDValidity == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(folder.UIDValidity), 10)
}

// pinOffline pins the folder's message with remoteID when its body is
// stored. A row that is gone or still a stub is left alone.
func (a *App) pinOffline(ctx context.Context, folder storage.Folder, remoteID string) {
	id, err := a.store.MessageIDByRemoteID(ctx, folder.ID, remoteID)
	if err != nil {
		return
	}
	if need, err := a.store.MessageNeedsBody(ctx, id); err != nil || need {
		return
	}
	if err := a.store.SetOffline(a.ctx, id, true); err != nil {
		a.log.Error("download pin", "id", id, "err", err)
	}
}

// storeOffline stores one downloaded message that had no row at plan time
// and returns its row id, with the attachment choice. A stub a sync listed
// meanwhile is filled in place, with its attachments when it is inside the
// sync window (see offlineStubs), else with the choice. A row that is
// complete already, or that a sync inserted between the lookup and the
// insert, is only looked up. A folder reset to another generation since the
// fetch stores nothing and returns storage.ErrBodyTargetChanged. It stores
// with the app context so a cancel cannot leave a message half written.
func (a *App) storeOffline(folder storage.Folder, msg psync.Fetched, includeAttachments bool) (int64, error) {
	if id, err := a.store.MessageIDByRemoteID(a.ctx, folder.ID, msg.RemoteID); err == nil {
		need, err := a.store.MessageNeedsBody(a.ctx, id)
		if err != nil {
			return 0, err
		}
		if !need {
			return id, nil
		}
	} else if !errors.Is(err, storage.ErrMessageNotFound) {
		return 0, err
	}
	// Inside the sync window the message keeps its attachments whatever the
	// choice, for a stub and for a uid with no row alike: a sync that lists
	// it later will not fetch them.
	if !includeAttachments {
		floorUID, err := a.store.FolderSyncFloorUID(a.ctx, folder.ID)
		if err != nil {
			return 0, err
		}
		includeAttachments = inSyncWindow(msg.RemoteID, floorUID)
	}
	id, err := psync.StoreFetched(a.ctx, a.store, a.log, folder, msg, includeAttachments)
	if err != nil && !isUniqueViolation(err) {
		return 0, err
	}
	if id == 0 {
		return a.store.MessageIDByRemoteID(a.ctx, folder.ID, msg.RemoteID)
	}
	return id, nil
}

// inSyncWindow reports whether an IMAP remote id (the uid as text) is inside
// the folder's newest-first sync window: at or above floorUID, or any id when
// there is no floor. It is the body generator's predicate
// (RemoteIDsNeedingBodyNewest): an id that is not a uid counts as inside.
func inSyncWindow(remoteID string, floorUID uint32) bool {
	if floorUID == 0 {
		return true
	}
	uid, err := strconv.ParseUint(remoteID, 10, 32)
	return err != nil || uid == 0 || uint32(uid) >= floorUID
}

// isUniqueViolation reports a store error from a row inserted by someone else
// first.
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// emitDownloadProgress computes percent and eta and emits a progress event.
func (a *App) emitDownloadProgress(done, total int, start time.Time, label string) {
	percent := 0
	if total > 0 {
		percent = done * 100 / total
	}
	eta := 0
	if done > 0 {
		elapsed := time.Since(start).Seconds()
		perItem := elapsed / float64(done)
		eta = int(perItem * float64(total-done))
	}
	a.emit(EventDownloadProgress, DownloadProgressEvent{
		Running: true, Done: done, Total: total, Percent: percent, ETASeconds: eta, Label: label,
	})
}

// folderTasks is one folder's share of an account's download tasks: the
// stubs it had (newest first) and the messages that had no row. A unit
// holds mu for its run.
type folderTasks struct {
	mu       sync.Mutex
	folder   storage.Folder
	stubs    []string
	fresh    []string
	failures int
}

// groupByFolder buckets one account's tasks by folder id, since a Fetch asks
// for messages from a single mailbox.
func groupByFolder(tasks []dlTask) map[int64]*folderTasks {
	out := make(map[int64]*folderTasks)
	for _, t := range tasks {
		ft, ok := out[t.folder.ID]
		if !ok {
			ft = &folderTasks{folder: t.folder}
			out[t.folder.ID] = ft
		}
		if t.hadRow {
			ft.stubs = append(ft.stubs, t.remoteID)
		} else {
			ft.fresh = append(ft.fresh, t.remoteID)
		}
	}
	// The plan lists UIDs oldest first; the newest are filled first.
	for _, ft := range out {
		slices.Reverse(ft.stubs)
		slices.Reverse(ft.fresh)
	}
	return out
}

// groupByAccount buckets download tasks by their folder's account id.
func groupByAccount(tasks []dlTask) map[int64][]dlTask {
	out := make(map[int64][]dlTask)
	for _, t := range tasks {
		out[t.folder.AccountID] = append(out[t.folder.AccountID], t)
	}
	return out
}

// uniqueDestPath appends " (n)" before the extension until the path is free, so a
// save-all never silently overwrites two attachments that share a name.
func uniqueDestPath(dir, filename string) string {
	base := filepath.Base(filename)
	dest := filepath.Join(dir, base)
	if !fileExistsAt(dest) {
		return dest
	}
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if !fileExistsAt(candidate) {
			return candidate
		}
	}
	return dest
}

func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

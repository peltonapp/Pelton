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
// fast local search; a message already cached with its body is only pinned.
// includeAttachments controls whether attachment bytes are persisted.
// Progress (percent + eta) is emitted for the status bar.
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
// the app last closed. planDownload/planAccount already skip anything cached,
// so replaying the same range only fetches whatever the previous run had not
// gotten to yet. Called once from startup; a no-op if nothing was pending.
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
type dlTask struct {
	folder   storage.Folder
	remoteID string
}

// downloadBatch is how many messages one Fetch asks for: one round trip per
// batch keeps progress moving without a request per message.
const downloadBatch = 25

// planDownload lists, across every account, the messages since the cutoff that
// are not cached yet (tasks) and the cached ones that only need pinning (pin).
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

// planAccount lists, for one account, the messages since the cutoff that are
// not cached yet (tasks) and the ones already complete that only need pinning.
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
// and checks them against the cache: a uid missing or cached as a stub is a
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
		s, cached := have[uint32(uid)]
		if !cached || !s.BodyComplete {
			// a stub is fetched too: the user asked to have it offline.
			tasks = append(tasks, dlTask{folder: folder, remoteID: strconv.FormatUint(uint64(uid), 10)})
			continue
		}
		pin = append(pin, s.ID)
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

// fetchDownloadBatch fetches one batch of a folder's tasks, stores each message
// and pins it, then reports progress. Failures are logged per message so one
// bad message does not stop the rest.
func (a *App) fetchDownloadBatch(ctx context.Context, ad psync.Adapter, account storage.Account, folder storage.Folder, chunk []string, includeAttachments bool, prog *dlProgress) {
	fetched, ferr := ad.Fetch(ctx, folder.RemoteID, chunk)
	if ferr != nil {
		a.log.Error("download fetch", "folder", folder.ID, "err", ferr)
	}
	for _, msg := range fetched {
		id, err := a.storeOffline(folder, msg, includeAttachments)
		if err != nil {
			a.log.Error("download store", "remote_id", msg.RemoteID, "err", err)
			continue
		}
		if id == 0 {
			continue
		}
		if err := a.store.SetOffline(a.ctx, id, true); err != nil {
			a.log.Error("download pin", "id", id, "err", err)
		}
	}
	a.emitDownloadProgress(prog.add(len(chunk)), prog.total, prog.start, account.Email)
}

// storeOffline stores one downloaded message and returns its row id to pin.
// A stub, cached at plan time or listed by a sync since, is filled in place
// with its attachments, as a sync's body fill would. A row that is complete
// by then, or that a sync inserted between the lookup and the insert, is only
// looked up. It stores with the app context so a cancel cannot leave a
// message half written.
func (a *App) storeOffline(folder storage.Folder, msg psync.Fetched, includeAttachments bool) (int64, error) {
	if id, err := a.store.MessageIDByRemoteID(a.ctx, folder.ID, msg.RemoteID); err == nil {
		need, err := a.store.MessageNeedsBody(a.ctx, id)
		if err != nil {
			return 0, err
		}
		if !need {
			return id, nil
		}
		includeAttachments = true
	} else if !errors.Is(err, storage.ErrMessageNotFound) {
		return 0, err
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

// folderTasks is one folder's share of an account's download tasks.
type folderTasks struct {
	folder    storage.Folder
	remoteIDs []string
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
		ft.remoteIDs = append(ft.remoteIDs, t.remoteID)
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

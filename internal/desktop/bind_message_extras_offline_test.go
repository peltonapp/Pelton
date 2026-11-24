package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Keeping a stub offline fetches its body first; when that fetch fails the
// message is not pinned, since there is nothing to read offline.
func TestDownloadMessageOfflineFetchesStubBody(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fetch func(db *storage.DB) func(context.Context, storage.Account, storage.Folder, []string) error
		ok    bool
	}{{
		name: "FetchStoresBody",
		fetch: func(db *storage.DB) func(context.Context, storage.Account, storage.Folder, []string) error {
			return func(ctx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
				_, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
					AccountID: acc.ID, FolderID: f.ID, RemoteID: remoteIDs[0],
					Subject: "Stub subject", BodyPlain: "full body",
				}, nil)
				return err
			}
		},
		ok: true,
	}, {
		name: "FetchFailureDoesNotPin",
		fetch: func(*storage.DB) func(context.Context, storage.Account, storage.Folder, []string) error {
			return func(context.Context, storage.Account, storage.Folder, []string) error {
				return errors.New("offline")
			}
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStubApp(t, nil)
			s.app.onDemandFetchForTest = tc.fetch(s.db)

			if err := s.app.DownloadMessageOffline(s.msgID); (err == nil) != tc.ok {
				t.Fatalf("DownloadMessageOffline = %v, want success %v", err, tc.ok)
			}
			m := mustGetMessage(t, s.db, s.msgID)
			if m.BodyComplete != tc.ok || m.Offline != tc.ok {
				t.Fatalf("BodyComplete=%v Offline=%v, want both %v", m.BodyComplete, m.Offline, tc.ok)
			}
		})
	}
}

// A fetch that stores nothing (the server lost the message) reads as such,
// not as an internal error.
func TestDownloadMessageOfflineReportsAMessageGoneFromTheServer(t *testing.T) {
	s := newStubApp(t, nil)
	s.app.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error { return nil }
	if err := s.app.DownloadMessageOffline(s.msgID); !errors.Is(err, errMessageNotOnServer) {
		t.Fatalf("DownloadMessageOffline = %v, want %v", err, errMessageNotOnServer)
	}
}

// Keeping a message offline while the reading pane fetches its body joins
// that fetch instead of starting a second one.
func TestDownloadMessageOfflineJoinsThePanesFetch(t *testing.T) {
	s := newStubApp(t, nil)
	var fetches atomic.Int32
	s.app.onDemandFetchForTest = func(context.Context, storage.Account, storage.Folder, []string) error {
		fetches.Add(1)
		return nil
	}
	paneDone := make(chan struct{})
	s.app.bodyFetches.Store(s.msgID, paneDone)
	result := make(chan error, 1)
	go func() { result <- s.app.DownloadMessageOffline(s.msgID) }()
	noRecv(t, result, 100*time.Millisecond, "keeping offline did not wait for the pane's fetch")

	m := mustGetMessage(t, s.db, s.msgID)
	if _, err := s.db.InsertMessageWithAttachments(s.app.ctx, &storage.Message{
		AccountID: m.AccountID, FolderID: m.FolderID, RemoteID: m.RemoteID, BodyPlain: "full body",
	}, nil); err != nil {
		t.Fatal(err)
	}
	s.app.bodyFetches.Delete(s.msgID)
	close(paneDone)
	if err := recv(t, result, 2*time.Second, "keeping offline never ended"); err != nil {
		t.Fatalf("DownloadMessageOffline: %v", err)
	}
	if fetches.Load() != 0 || !mustGetMessage(t, s.db, s.msgID).Offline {
		t.Fatalf("%d own fetches, offline %v; want 0 and pinned", fetches.Load(), mustGetMessage(t, s.db, s.msgID).Offline)
	}
}

// A pane that opens the message while keeping it offline fetches its body
// finds that fetch running and starts none of its own; its spinner waits for
// mail:updated or mail:bodyFailed, so the joined fetch sends one.
func TestDownloadMessageOfflineTellsAPaneThatJoined(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store bool
		want  string
	}{
		{"stored", true, EventMailUpdated},
		{"failed", false, EventMailBodyFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStubApp(t, nil)
			s.app.onDemandFetchForTest = func(ctx context.Context, acc storage.Account, f storage.Folder, remoteIDs []string) error {
				if !tc.store {
					return errors.New("offline")
				}
				_, err := s.db.InsertMessageWithAttachments(ctx, &storage.Message{
					AccountID: acc.ID, FolderID: f.ID, RemoteID: remoteIDs[0], BodyPlain: "full body",
				}, nil)
				return err
			}
			var mu sync.Mutex
			var got []string
			s.app.emitForTest = func(name string, payload any) {
				switch e := payload.(type) {
				case MailUpdatedEvent:
					if e.MessageID == s.msgID {
						mu.Lock()
						got = append(got, name)
						mu.Unlock()
					}
				case MailBodyFailedEvent:
					if e.MessageID == s.msgID {
						mu.Lock()
						got = append(got, name)
						mu.Unlock()
					}
				}
			}
			_ = s.app.DownloadMessageOffline(s.msgID)
			mu.Lock()
			defer mu.Unlock()
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("pane events %v, want [%s]", got, tc.want)
			}
		})
	}
}

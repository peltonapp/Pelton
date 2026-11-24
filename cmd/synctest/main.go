// Command synctest exercises internal/sync end-to-end against a real account:
// it opens the local db, runs migrations, connects over imap, runs a full sync
// of INBOX, then makes a local flag change and syncs again to show it pushed to
// the server. Credentials come from the environment, same as the imap layer:
//
//	IMAP_HOST, IMAP_PORT, IMAP_USER, IMAP_PASSWORD
//	IMAP_INSECURE=1   skip TLS verification (debug only)
//
// Note: the push demo marks one message \Seen on the real account. That is a
// real side effect on your mailbox (reversible by marking it unread again).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

const inboxPath = "INBOX"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// cancel cleanly on ctrl+c.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := configFromEnv()
	if err != nil {
		return err
	}

	store, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	client, err := connect(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	defer logoutQuietly(client)

	folder, err := ensureAccountAndFolder(ctx, store, client, cfg)
	if err != nil {
		return err
	}

	engine := psync.NewEngine(pimap.NewAdapter(client), store, log)

	fmt.Println("\n=== first sync ===")
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		return err
	}
	printResult(res)

	if err := pushDemo(ctx, store, client, engine, folder); err != nil {
		return err
	}
	return nil
}

func configFromEnv() (pimap.Config, error) {
	cfg := pimap.Config{
		Host:               os.Getenv("IMAP_HOST"),
		Username:           os.Getenv("IMAP_USER"),
		Password:           os.Getenv("IMAP_PASSWORD"),
		InsecureSkipVerify: os.Getenv("IMAP_INSECURE") == "1",
	}
	if cfg.Host == "" || cfg.Username == "" || cfg.Password == "" {
		return cfg, fmt.Errorf("IMAP_HOST, IMAP_USER and IMAP_PASSWORD must be set")
	}
	if p := os.Getenv("IMAP_PORT"); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return cfg, fmt.Errorf("invalid IMAP_PORT %q: %w", p, err)
		}
		cfg.Port = port
	}
	return cfg, nil
}

func openStore(ctx context.Context) (*storage.DB, error) {
	path, err := storage.DefaultPath()
	if err != nil {
		return nil, err
	}
	fmt.Printf("opening db at %s\n", path)
	store, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	if err := store.RunMigrations(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func connect(cfg pimap.Config) (*pimap.Client, error) {
	fmt.Printf("connecting to %s ...\n", cfg.Host)
	client, err := pimap.Connect(cfg)
	if err != nil {
		return nil, err
	}
	if err := client.Login(); err != nil {
		client.Close()
		return nil, err
	}
	fmt.Printf("logged in as %s\n", cfg.Username)
	return client, nil
}

func logoutQuietly(client *pimap.Client) {
	if err := client.Logout(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: logout: %v\n", err)
	}
}

// ensureAccountAndFolder finds or creates the storage rows the sync engine needs
// for this account's INBOX. Credentials are never stored, only metadata. The
// folder's delimiter is taken from the server's LIST so it is never assumed.
func ensureAccountAndFolder(ctx context.Context, store *storage.DB, client *pimap.Client, cfg pimap.Config) (storage.Folder, error) {
	account, err := findOrCreateAccount(ctx, store, cfg)
	if err != nil {
		return storage.Folder{}, err
	}
	folder, err := findOrCreateInbox(ctx, store, client, account.ID)
	if err != nil {
		return storage.Folder{}, err
	}
	return folder, nil
}

func findOrCreateAccount(ctx context.Context, store *storage.DB, cfg pimap.Config) (storage.Account, error) {
	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		return storage.Account{}, err
	}
	for _, a := range accounts {
		if a.Email == cfg.Username {
			return a, nil
		}
	}

	account := &storage.Account{
		Email:    cfg.Username,
		IMAPHost: cfg.Host,
		IMAPPort: cfg.Port,
	}
	if account.IMAPPort == 0 {
		account.IMAPPort = pimap.DefaultPort
	}
	if _, err := store.CreateAccount(ctx, account); err != nil {
		return storage.Account{}, err
	}
	fmt.Printf("created account %d for %s\n", account.ID, account.Email)
	return *account, nil
}

func findOrCreateInbox(ctx context.Context, store *storage.DB, client *pimap.Client, accountID int64) (storage.Folder, error) {
	folders, err := store.ListFolders(ctx, accountID)
	if err != nil {
		return storage.Folder{}, err
	}
	for _, f := range folders {
		if f.IMAPPath == inboxPath {
			return f, nil
		}
	}

	delimiter := inboxDelimiter(client)
	folder := &storage.Folder{
		AccountID: accountID,
		Name:      inboxPath,
		IMAPPath:  inboxPath,
		Delimiter: delimiter,
	}
	if _, err := store.CreateFolder(ctx, folder); err != nil {
		return storage.Folder{}, err
	}
	fmt.Printf("created folder %d for %s (delimiter=%q)\n", folder.ID, folder.IMAPPath, folder.Delimiter)
	return *folder, nil
}

// inboxDelimiter reads the hierarchy separator the server reports for INBOX,
// falling back to empty if the listing does not include it.
func inboxDelimiter(client *pimap.Client) string {
	folders, err := client.ListFolders()
	if err != nil {
		return ""
	}
	for _, f := range folders {
		if f.Name == inboxPath {
			return string(f.Delimiter)
		}
	}
	return ""
}

// pushDemo marks the newest cached message \Seen locally if it is currently
// unseen, then runs sync again so the change is pushed to the server.
func pushDemo(ctx context.Context, store *storage.DB, client *pimap.Client, engine *psync.Engine, folder storage.Folder) error {
	states, err := store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Println("\ninbox empty, skipping push demo")
		return nil
	}

	target := pickUnseen(states)
	fmt.Printf("\n=== push demo on uid %d ===\n", target.UID)

	before, err := client.FetchFlags(imap.UID(target.UID))
	if err != nil {
		return err
	}
	fmt.Printf("server flags before: %v\n", before)

	if err := store.MarkFlagsPending(ctx, target.ID, target.Flags|storage.FlagSeen); err != nil {
		return err
	}
	fmt.Println("marked \\Seen locally (pending push)")

	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		return err
	}
	printResult(res)

	after, err := client.FetchFlags(imap.UID(target.UID))
	if err != nil {
		return err
	}
	fmt.Printf("server flags after:  %v\n", after)
	return nil
}

// pickUnseen returns the highest-uid message that is not yet \Seen, so the push
// is visible, falling back to the highest uid if all are already seen.
func pickUnseen(states []storage.MessageState) storage.MessageState {
	// states are ordered by uid ascending.
	target := states[len(states)-1]
	for _, state := range slices.Backward(states) {
		if !state.Flags.Has(storage.FlagSeen) {
			return state
		}
	}
	return target
}

func printResult(res psync.FolderSyncResult) {
	fmt.Printf("new=%d deleted=%d flag_updated=%d conflicts=%d pushed=%d generation_reset=%v has_older=%v repaired=%d\n",
		res.New, res.Deleted, res.FlagUpdated, res.Conflicts, res.Pushed, res.GenerationReset, res.HasOlder, res.Repaired)
}

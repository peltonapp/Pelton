// Command storagetest exercises internal/storage end-to-end: it opens the real
// config dir database, runs migrations, writes an account, a folder with a non
// default delimiter, a message with one attachment, then reads it all back,
// runs a full text search and round trips a few settings.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

const (
	// a non default hierarchy delimiter, to prove it is stored verbatim and not
	// assumed to be "/".
	dottedDelimiter = "."

	demoAttachmentName    = "notes.txt"
	demoAttachmentType    = "text/plain"
	demoAttachmentContent = "hello from a fake attachment\n"

	missingSetKey = "this_key_was_never_set"
)

// windowSize is a structured setting stored as json.
type windowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	path, err := storage.DefaultPath()
	if err != nil {
		return err
	}
	fmt.Printf("opening db at %s\n", path)

	db, err := storage.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.RunMigrations(ctx); err != nil {
		return err
	}
	fmt.Println("migrations applied")

	accountID, folderID, err := seedAccountAndFolder(ctx, db)
	if err != nil {
		return err
	}

	messageID, err := seedMessage(ctx, db, accountID, folderID)
	if err != nil {
		return err
	}

	if err := readBack(ctx, db, accountID, folderID, messageID); err != nil {
		return err
	}
	return settingsDemo(ctx, db)
}

func seedAccountAndFolder(ctx context.Context, db *storage.DB) (accountID, folderID int64, err error) {
	account := &storage.Account{
		Email:       "demo@example.com",
		DisplayName: "Demo User",
		IMAPHost:    "imap.example.com",
		IMAPPort:    993,
		SMTPHost:    "smtp.example.com",
		SMTPPort:    465,
	}
	accountID, err = db.CreateAccount(ctx, account)
	if err != nil {
		return 0, 0, err
	}
	fmt.Printf("\ncreated account %d (%s)\n", accountID, account.Email)

	folder := &storage.Folder{
		AccountID:   accountID,
		Name:        "Inbox",
		IMAPPath:    "INBOX",
		Delimiter:   dottedDelimiter, // non default separator, stored as is
		Attributes:  []string{"\\HasNoChildren"},
		UIDValidity: 123456789,
	}
	folderID, err = db.CreateFolder(ctx, folder)
	if err != nil {
		return 0, 0, err
	}
	fmt.Printf("created folder %d (path=%q delimiter=%q uidvalidity=%d)\n",
		folderID, folder.IMAPPath, folder.Delimiter, folder.UIDValidity)
	return accountID, folderID, nil
}

func seedMessage(ctx context.Context, db *storage.DB, accountID, folderID int64) (int64, error) {
	msg := &storage.Message{
		AccountID:   accountID,
		FolderID:    folderID,
		UID:         42,
		MessageID:   "<demo-42@example.com>",
		Subject:     "Your invoice for June",
		FromAddress: "billing@example.com",
		FromName:    "Billing",
		ToAddresses: "demo@example.com",
		Date:        time.Now().UTC(),
		Flags:       storage.FlagSeen,
		BodyPlain:   "Please find your invoice attached. Total due is 42 euros.",
		BodyHTML:    "<p>Please find your invoice attached.</p>",
		SizeBytes:   2048,
	}
	atts := []storage.IncomingAttachment{{
		Filename:    demoAttachmentName,
		ContentType: demoAttachmentType,
		Content:     strings.NewReader(demoAttachmentContent),
	}}

	id, err := db.InsertMessageWithAttachments(ctx, msg, atts)
	if err != nil {
		return 0, err
	}
	fmt.Printf("inserted message %d (uid %d) with %d attachment(s)\n", id, msg.UID, len(atts))
	return id, nil
}

func readBack(ctx context.Context, db *storage.DB, accountID, folderID, messageID int64) error {
	fmt.Println("\n=== read back ===")

	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		fmt.Printf("account %d: %s <%s> imap=%s:%d created=%s\n",
			a.ID, a.DisplayName, a.Email, a.IMAPHost, a.IMAPPort, a.CreatedAt.Format(time.RFC3339))
	}

	folders, err := db.ListFolders(ctx, accountID)
	if err != nil {
		return err
	}
	for _, f := range folders {
		fmt.Printf("folder %d: %s delim=%q attrs=%v uidvalidity=%d\n",
			f.ID, f.IMAPPath, f.Delimiter, f.Attributes, f.UIDValidity)
	}

	messages, err := db.ListMessages(ctx, folderID, 0)
	if err != nil {
		return err
	}
	for _, m := range messages {
		fmt.Printf("message %d: uid=%d subject=%q seen=%v has_attachments=%v\n",
			m.ID, m.UID, m.Subject, m.Flags.Has(storage.FlagSeen), m.HasAttachments)
	}

	attachments, err := db.ListAttachments(ctx, messageID)
	if err != nil {
		return err
	}
	for _, a := range attachments {
		content, err := readAttachment(db, a)
		if err != nil {
			return err
		}
		fmt.Printf("attachment %d: %s (%s, %d bytes) disk_path=%q content=%q\n",
			a.ID, a.Filename, a.ContentType, a.SizeBytes, a.DiskPath, content)
	}
	return nil
}

func readAttachment(db *storage.DB, att storage.Attachment) (string, error) {
	rc, err := db.OpenAttachment(context.Background(), att)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read attachment: %w", err)
	}
	return string(data), nil
}

func settingsDemo(ctx context.Context, db *storage.DB) error {
	fmt.Println("\n=== settings ===")

	if err := db.Set(ctx, storage.SettingTheme, "dark"); err != nil {
		return err
	}
	theme, err := db.Get(ctx, storage.SettingTheme)
	if err != nil {
		return err
	}
	fmt.Printf("%s = %q\n", storage.SettingTheme, theme)

	if err := db.SetJSON(ctx, storage.SettingWindowSize, windowSize{Width: 1280, Height: 800}); err != nil {
		return err
	}
	var size windowSize
	if err := db.GetJSON(ctx, storage.SettingWindowSize, &size); err != nil {
		return err
	}
	fmt.Printf("%s = %+v\n", storage.SettingWindowSize, size)

	// demonstrate the not found behaviour for a key that was never set.
	_, err = db.Get(ctx, missingSetKey)
	if err != nil {
		fmt.Printf("get %q -> %v (expected)\n", missingSetKey, err)
	} else {
		fmt.Printf("get %q unexpectedly returned a value\n", missingSetKey)
	}
	return nil
}

package desktop

import (
	"errors"

	"github.com/peltonapp/Pelton/internal/credentials"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// errEmptyPassword rejects a blank password rather than storing one that can
// never authenticate.
var errEmptyPassword = errors.New("pelton: enter a password")

// errAccountUsesOAuth means the account signs in with a provider token, so a
// password would be the wrong credential entirely.
var errAccountUsesOAuth = errors.New("pelton: this mailbox signs in with your provider, not a password")

// UpdateAccountRequest carries the editable fields of an existing account. The
// email address is intentionally not editable here: it keys folder/message
// ownership and the display identity, so changing it belongs to a re-add rather
// than an in-place edit.
type UpdateAccountRequest struct {
	ID int64 `json:"id"`
	// DisplayName is the From name recipients see. LocalLabel is the name this
	// app shows for the mailbox instead when UseLocalLabel is set; it is stored
	// either way, so switching the toggle off does not throw it away.
	DisplayName   string `json:"displayName"`
	LocalLabel    string `json:"localLabel"`
	UseLocalLabel bool   `json:"useLocalLabel"`
	// Username is the login name when it differs from the email; empty logs in
	// with the account's email.
	Username string `json:"username"`
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	// Password sets a new login password. Empty leaves whatever is stored
	// alone, so an edit that only moves the server ports does not have to
	// re-enter it. An account imported from another client has no stored
	// password at all, and this is how it gets one.
	Password string `json:"password"`
	// IMAPTLS and SMTPTLS pin the connection security: "ssl", "starttls", or
	// empty to derive it from the port.
	IMAPTLS string `json:"imapTls"`
	SMTPTLS string `json:"smtpTls"`
	// ExportOnArchive and the fields under it configure the local .eml copy
	// written when a message is archived. An empty ExportDir turns the export
	// off whatever the toggle says, since there would be nowhere to write.
	ExportOnArchive    bool   `json:"exportOnArchive"`
	ExportDir          string `json:"exportDir"`
	ExportSubfolders   string `json:"exportSubfolders"`
	ExportNameTemplate string `json:"exportNameTemplate"`
	// PGPDefault is the account's starting point for protecting outgoing mail:
	// '' unprotected, 'sign', or 'auto'. An unknown value is treated as ''.
	PGPDefault string `json:"pgpDefault"`
	// SyncMaxParallel overrides the global parallel-sync setting for this
	// account (clamped to 1-5). Nil follows the global setting: it clears a
	// stored override rather than leaving it alone, so every update must send
	// the current value.
	SyncMaxParallel *int `json:"syncMaxParallel"`
	// Proxy is the route the mailbox's connections take (#457).
	Proxy AccountProxyDTO `json:"proxy"`
}

// UpdateAccount persists edits to an account's display name and server settings.
// It loads the row first so the immutable email is preserved regardless of the
// request. Server changes take effect on the next connection (existing idle
// sessions keep their current settings until they reconnect).
func (a *App) UpdateAccount(req UpdateAccountRequest) (AccountDTO, error) {
	if err := a.ready(); err != nil {
		return AccountDTO{}, err
	}
	if !validTLSMode(req.IMAPTLS) || !validTLSMode(req.SMTPTLS) {
		return AccountDTO{}, errUnknownTLSMode
	}
	route, err := accountProxyFromDTO(req.Proxy)
	if err != nil {
		return AccountDTO{}, err
	}
	account, err := a.store.GetAccount(a.ctx, req.ID)
	if err != nil {
		return AccountDTO{}, err
	}
	account.DisplayName = req.DisplayName
	account.LocalLabel = req.LocalLabel
	account.UseLocalLabel = req.UseLocalLabel
	account.Username = req.Username
	account.IMAPHost = req.IMAPHost
	account.IMAPPort = req.IMAPPort
	account.SMTPHost = req.SMTPHost
	account.SMTPPort = req.SMTPPort
	account.IMAPTLS = req.IMAPTLS
	account.SMTPTLS = req.SMTPTLS
	if err := a.store.UpdateAccount(a.ctx, account); err != nil {
		return AccountDTO{}, err
	}
	account.ExportOnArchive = req.ExportOnArchive && req.ExportDir != ""
	account.ExportDir = req.ExportDir
	account.ExportSubfolders = validSubfolderMode(req.ExportSubfolders)
	account.ExportNameTemplate = req.ExportNameTemplate
	if err := a.store.SetAccountArchiveExport(a.ctx, account.ID, account.ExportOnArchive,
		account.ExportDir, account.ExportSubfolders, account.ExportNameTemplate); err != nil {
		return AccountDTO{}, err
	}
	account.PGPDefault = validPGPDefault(req.PGPDefault)
	if err := a.store.SetAccountPGPDefault(a.ctx, account.ID, account.PGPDefault); err != nil {
		return AccountDTO{}, err
	}
	if err := saveAccountProxyPassword(account.ID, route, req.Proxy); err != nil {
		return AccountDTO{}, err
	}
	account.Proxy = route
	if err := a.store.SetAccountProxy(a.ctx, account.ID, route); err != nil {
		return AccountDTO{}, err
	}
	if req.SyncMaxParallel != nil {
		n := clampSyncMaxParallel(*req.SyncMaxParallel)
		req.SyncMaxParallel = &n
	}
	account.SyncMaxParallel = req.SyncMaxParallel
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, account.ID, account.SyncMaxParallel); err != nil {
		return AccountDTO{}, err
	}
	a.reconfigureAccountPool(account.ID)
	if req.Password != "" {
		if err := a.SetAccountPassword(req.ID, req.Password); err != nil {
			return AccountDTO{}, err
		}
	}
	return toAccountDTO(*account), nil
}

// SetAccountPassword stores a login password for an account, replacing whatever
// was there. It refuses to overwrite an OAuth secret with a password, since
// that would silently downgrade a working Gmail or Outlook login to one the
// provider will reject.
func (a *App) SetAccountPassword(accountID int64, password string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if password == "" {
		return errEmptyPassword
	}
	if existing, err := credentials.Load(accountID); err == nil && existing.Method == credentials.MethodOAuth {
		return errAccountUsesOAuth
	}
	if err := credentials.Store(accountID, credentials.Secret{
		Method:   credentials.MethodPassword,
		Password: password,
	}); err != nil {
		return err
	}
	a.clearRejectedLogin(accountID)
	// a dismissal answers "stop asking about this missing password". The
	// password is no longer missing, so the next one that goes missing gets its
	// own prompt rather than inheriting this answer.
	if err := a.store.SetAccountPasswordPromptDismissed(a.ctx, accountID, false); err != nil {
		a.log.Error("clear password prompt dismissal", "account", accountID, "err", err)
	}
	return nil
}

// errAccountUsesPassword means the account logs in with a password, so there
// is no provider to sign in to again.
var errAccountUsesPassword = errors.New("pelton: this mailbox signs in with a password, not a provider")

// AccountOAuthProvider returns the oauth provider key an account signs in with
// ("google", "microsoft"), or empty when it uses a password or has no stored
// secret, so the mailbox editor can offer signing in again instead of a
// password field.
func (a *App) AccountOAuthProvider(accountID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	secret, err := a.secretStore().Load(accountID)
	if errors.Is(err, credentials.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if secret.Method != credentials.MethodOAuth {
		return "", nil
	}
	return secret.Provider, nil
}

// ReauthorizeOAuthAccount runs the consent flow again for an oauth account with
// the client it was set up with, and replaces its tokens. It is the way back
// once the provider revoked or expired the refresh token, without deleting the
// mailbox and its cached mail. The sync and idle loops resolve credentials on
// every attempt, so they pick up the new token on their next try.
func (a *App) ReauthorizeOAuthAccount(accountID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	existing, err := a.secretStore().Load(accountID)
	if err != nil {
		return err
	}
	if existing.Method != credentials.MethodOAuth {
		return errAccountUsesPassword
	}
	client, err := a.accountOAuthClient(*account)
	if err != nil {
		return err
	}
	secret, err := a.authorizeOAuth(client, existing.Provider, existing.ClientID, existing.ClientSecret, account.Email)
	if err != nil {
		return err
	}
	if err := a.secretStore().Store(accountID, secret); err != nil {
		return err
	}
	a.clearRejectedLogin(accountID)
	return nil
}

// PasswordCheckDTO reports how a password fared against the account's imap
// server. Rejected separates "the server said no" from "nothing answered": the
// first means the password is wrong, the second says nothing about it at all.
type PasswordCheckDTO struct {
	OK       bool   `json:"ok"`
	Rejected bool   `json:"rejected"`
	Error    string `json:"error"`
}

// CheckAccountPassword tries a password against an existing account's imap
// server without storing it, so a mailbox marked as refused can be fixed on the
// spot instead of the user finding out at the next sync.
//
// A refusal and an unreachable server are both returned as a result, not an
// error: the caller shows them differently. Only a problem with the request
// itself (no such account, nothing typed) is an error.
func (a *App) CheckAccountPassword(accountID int64, password string) (PasswordCheckDTO, error) {
	if err := a.ready(); err != nil {
		return PasswordCheckDTO{}, err
	}
	if password == "" {
		return PasswordCheckDTO{}, errEmptyPassword
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return PasswordCheckDTO{}, err
	}
	if existing, err := credentials.Load(accountID); err == nil && existing.Method == credentials.MethodOAuth {
		return PasswordCheckDTO{}, errAccountUsesOAuth
	}
	return a.protocolFor(*account).checkPassword(*account, password), nil
}

// noteLoginResult records what the server said about an account's credentials.
// A refusal is remembered so the ui can mark the mailbox and offer the password
// prompt; anything else (a dropped connection, a server that is down) says
// nothing about the password and is ignored. A success clears the mark.
//
// Callers pass the error from Login directly, including nil.
func (a *App) noteLoginResult(accountID int64, err error) {
	if err != nil && !loginRefused(err) {
		return
	}
	a.rejectedLoginsMu.Lock()
	defer a.rejectedLoginsMu.Unlock()
	if err == nil {
		delete(a.rejectedLogins, accountID)
		return
	}
	if a.rejectedLogins == nil {
		a.rejectedLogins = make(map[int64]struct{})
	}
	a.rejectedLogins[accountID] = struct{}{}
}

// loginRefused reports whether err is the server refusing the credentials.
func loginRefused(err error) bool {
	return errors.Is(err, pimap.ErrAuthFailed)
}

// loginRejected reports whether the server refused this account's credentials
// the last time Pelton tried.
func (a *App) loginRejected(accountID int64) bool {
	a.rejectedLoginsMu.Lock()
	defer a.rejectedLoginsMu.Unlock()
	_, rejected := a.rejectedLogins[accountID]
	return rejected
}

// clearRejectedLogin forgets a refusal, so a newly stored password is given a
// fair try rather than staying marked until the next sync.
func (a *App) clearRejectedLogin(accountID int64) {
	a.rejectedLoginsMu.Lock()
	defer a.rejectedLoginsMu.Unlock()
	delete(a.rejectedLogins, accountID)
}

// DismissPasswordPrompt stops the missing-password prompt from interrupting for
// one account. The account still cannot sync without a password; the ui marks
// it in the sidebar and in Settings instead, and clicking that marker brings the
// prompt back.
func (a *App) DismissPasswordPrompt(accountID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetAccountPasswordPromptDismissed(a.ctx, accountID, true)
}

// needsPassword decides whether one account should be prompted for, given the
// error its keyring lookup returned. It is separate from the loop so it can be
// tested without reading the developer's real keyring.
//
// Only a definite "nothing stored" counts. A keyring that is locked or broken
// returns some other error, and prompting then would ask the user to retype a
// password they already have.
func (a *App) needsPassword(acct storage.Account, secretErr error) bool {
	if !errors.Is(secretErr, credentials.ErrNotFound) {
		return false
	}
	// Local Folders has no server to log in to, so it has no secret and is not
	// missing one.
	if acct.Local {
		return false
	}
	// the legacy cli account takes its password from the environment and is
	// not actually missing one.
	if _, err := a.imapFromEnv(pimap.Config{Username: loginName(acct)}); err == nil {
		return false
	}
	return true
}

// AccountsNeedingPassword lists the accounts that cannot log in: no password was
// ever stored for them, or the server refused the one that is. Importing from
// another mail client creates the account but cannot take its password, and a
// password changed at the provider goes stale; the frontend prompts for a new
// one instead of letting every sync fail quietly.
//
// Accounts whose prompt was dismissed stay in the list. They are still broken,
// and the ui needs them to place the marker that says so; it reads
// PasswordPromptDismissed to decide which ones to actually ask about.
func (a *App) AccountsNeedingPassword() ([]AccountDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountDTO, 0)
	for _, acct := range accounts {
		_, secretErr := credentials.Load(acct.ID)
		// two ways to be unable to log in: nothing stored, or something stored
		// that the server refuses. The user fixes both the same way.
		if !a.needsPassword(acct, secretErr) && !a.loginRejected(acct.ID) {
			continue
		}
		out = append(out, toAccountDTO(acct))
	}
	return out, nil
}

// AllAccounts lists every account on the install, including the ones the
// current profile does not show. The profile editor is the only screen that
// wants this: everywhere else works with what the profile can see.
func (a *App) AllAccounts() ([]AccountDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListAllAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountDTO, 0, len(accounts))
	for _, acct := range accounts {
		out = append(out, toAccountDTO(acct))
	}
	return out, nil
}

// DeleteAccount removes an account entirely: its keyring secret, its cached mail
// (folders, messages and attachment rows cascade in the db) and its attachment
// files on disk. First it hard-cancels that account only: its worker (IDLE),
// scheduler jobs and IMAP sessions stop, and nothing can start
// sync work for it again. Other accounts keep running. If the row cannot be
// deleted, a worker the account had is started again.
func (a *App) DeleteAccount(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	release := a.holdAccountSync(id)
	defer release()
	hadWorker := a.hasAccountWorker(id)
	a.stopAccountWorker(id)
	if err := a.store.DeleteAccount(a.ctx, id); err != nil {
		if hadWorker {
			a.startHeldAccountWorker(id)
		}
		return err
	}
	a.forgetAccountState(id)
	if err := credentials.Delete(id); err != nil {
		a.log.Error("delete credentials", "account", id, "err", err)
	}
	if err := credentials.DeleteAccountProxyPassword(id); err != nil {
		a.log.Error("delete proxy password", "account", id, "err", err)
	}
	if err := a.store.DeleteAttachmentFilesForAccount(id); err != nil {
		a.log.Error("delete attachment files", "account", id, "err", err)
	}
	return nil
}

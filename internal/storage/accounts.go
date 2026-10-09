package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAccountNotFound is returned when an account id has no row.
var ErrAccountNotFound = errors.New("storage: account not found")

// Account is non sensitive account metadata. Passwords and tokens are never
// stored here, they live in the os keyring keyed by this row's ID.
type Account struct {
	ID    int64
	Email string
	// DisplayName is the outward identity: the name recipients see next to the
	// address on mail sent from this account. It never doubles as the local
	// name for the mailbox, LocalLabel does that.
	DisplayName string
	// LocalLabel is what this app calls the mailbox, shown wherever the account
	// is named locally and sent nowhere. UseLocalLabel is whether it applies;
	// switching it off keeps the label stored rather than discarding it.
	LocalLabel    string
	UseLocalLabel bool
	// Username is the login name when it differs from the email address. Empty
	// means authenticate with Email.
	Username string
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
	// IMAPTLS and SMTPTLS pin the connection security: "ssl" for implicit TLS,
	// "starttls" to upgrade a cleartext connection. Empty derives it from the
	// port, which is what every account created before this was storable does.
	IMAPTLS   string
	SMTPTLS   string
	CreatedAt time.Time
	// Position is the account section's rank in the sidebar, or 0 until the user
	// reorders them. Unpositioned accounts sort after positioned ones by id, so
	// an install that never reordered keeps creation order.
	Position int
	// ExportOnArchive writes a local .eml copy of every message archived from
	// this account. ExportDir is where they go and must be set for the export to
	// run; ExportSubfolders is one of the mailexport subfolder modes and
	// ExportNameTemplate its file name pattern, both empty meaning the default.
	ExportOnArchive    bool
	ExportDir          string
	ExportSubfolders   string
	ExportNameTemplate string
	// PGPDefault is how this account starts a new message: '' unprotected,
	// 'sign' to sign when the account has a key, 'auto' to sign and encrypt
	// whenever every recipient has one.
	PGPDefault string
	// PasswordPromptDismissed records that the user asked the missing-password
	// prompt to stop interrupting for this account. The account still cannot
	// sync; the ui marks it instead of asking. Storing a password clears it.
	PasswordPromptDismissed bool
	// Local marks the Local Folders account, which holds imported mail and has
	// no server behind it. Sync, idle and the mailbox backup all skip it, and
	// its Email is LocalAccountEmail rather than a real address.
	Local bool
	// SyncMaxParallel overrides the global parallel-sync setting for this
	// account. Nil means use the global value. The caller clamps it to the
	// allowed range before storing.
	SyncMaxParallel *int
	// TrustedCerts are SHA-256 fingerprints of server certificates the user
	// accepted for this mailbox, and CAPEM extra root certificates they
	// supplied. Both widen verification for the mailbox's imap and smtp
	// servers only; empty means the system roots alone.
	TrustedCerts []string
	CAPEM        string
	// Proxy is the route this mailbox's connections take. The zero value
	// follows the app-wide setting.
	Proxy AccountProxy
}

// Account proxy modes. The empty mode follows the app-wide setting, the
// others match the proxy package's modes.
const (
	AccountProxyGlobal = ""
	AccountProxyDirect = "off"
	AccountProxySystem = "system"
	AccountProxyManual = "manual"
)

// AccountProxy is a mailbox's own route. Scheme, Host, Port and Username only
// apply to AccountProxyManual; the password lives in the keyring.
//
// The mailbox's contacts sync and its sign-in refresh take the same route as
// its mail unless ContactsUseGlobal or OAuthUseGlobal sends them the app-wide
// way instead, so the zero value keeps everything on one route.
type AccountProxy struct {
	Mode              string
	Scheme            string
	Host              string
	Port              int
	Username          string
	ContactsUseGlobal bool
	OAuthUseGlobal    bool
}

// Label returns the name to show for this account in the app: the local label
// when there is one and it is switched on, the outward display name otherwise.
// An account with neither returns an empty string and the caller falls back to
// the address.
func (a Account) Label() string {
	if a.UseLocalLabel && a.LocalLabel != "" {
		return a.LocalLabel
	}
	return a.DisplayName
}

// LocalAccountEmail is the reserved address of the Local Folders account. It is
// not a routable address; it exists so the account row has the stable, unique
// identifier every other lookup in the app keys off.
const LocalAccountEmail = "local@pelton.invalid"

// LocalAccountName is the Local Folders account's stored display name. The ui
// localizes the label it shows, this is only the fallback.
const LocalAccountName = "Local Folders"

// accountColumns is the select list every account query shares, in the order
// scanAccount reads them.
// The sidebar rank comes from the layout join rather than the account row: the
// order of the sections belongs to a profile (#325), which is also the only
// place that can see the whole list.
const accountColumns = `a.id, a.email, a.display_name, a.username, a.imap_host, a.imap_port,
       a.smtp_host, a.smtp_port, a.imap_tls, a.smtp_tls, a.created_at,
       coalesce(o.position, 0), a.is_local,
       a.export_on_archive, a.export_dir, a.export_subfolders, a.export_name_template,
       a.pgp_default, a.password_prompt_dismissed, a.local_label, a.use_local_label,
       a.sync_max_parallel,
       a.trusted_certs, a.ca_pem,
       a.proxy_mode, a.proxy_scheme, a.proxy_host, a.proxy_port, a.proxy_username,
       a.proxy_contacts_global, a.proxy_oauth_global`

// accountFrom joins an account to the active profile's section order. Every
// query using accountColumns takes the layout profile id as its first argument.
const accountFrom = `
FROM accounts a
LEFT JOIN profile_account_order o ON o.account_id = a.id AND o.profile_id = ?`

// accountOrder puts the sections the user arranged first, then the rest by id,
// so an install that never reordered keeps creation order.
const accountOrder = `ORDER BY coalesce(o.position, 0) = 0, o.position, a.id`

// CreateAccount inserts an account and returns its new id. CreatedAt is set to
// now if the caller left it zero.
func (d *DB) CreateAccount(ctx context.Context, a *Account) (int64, error) {
	created := a.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}

	const query = `
INSERT INTO accounts (email, display_name, username, imap_host, imap_port, smtp_host, smtp_port, imap_tls, smtp_tls, created_at, is_local, local_label, use_local_label, trusted_certs, ca_pem,
                      proxy_mode, proxy_scheme, proxy_host, proxy_port, proxy_username, proxy_contacts_global, proxy_oauth_global)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	p := a.Proxy
	res, err := d.sql.ExecContext(ctx, query,
		a.Email, a.DisplayName, a.Username, a.IMAPHost, a.IMAPPort, a.SMTPHost, a.SMTPPort,
		a.IMAPTLS, a.SMTPTLS, formatTime(created), boolToInt(a.Local),
		a.LocalLabel, boolToInt(a.UseLocalLabel), joinLines(a.TrustedCerts), a.CAPEM,
		p.Mode, p.Scheme, p.Host, p.Port, p.Username, boolToInt(p.ContactsUseGlobal), boolToInt(p.OAuthUseGlobal))
	if err != nil {
		return 0, fmt.Errorf("storage: insert account %q: %w", a.Email, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: account insert id: %w", err)
	}
	a.ID = id
	a.CreatedAt = created
	// an account nobody can see would be invisible mail, so it joins the profile
	// that created it. Other profiles opt in from the profile editor.
	if err := d.AddAccountToProfiles(ctx, id, []int64{d.ScopedProfileID()}); err != nil {
		return 0, err
	}
	return id, nil
}

// GetAccount returns one account by id, or ErrAccountNotFound.
func (d *DB) GetAccount(ctx context.Context, id int64) (*Account, error) {
	const query = `
SELECT ` + accountColumns + accountFrom + `
WHERE a.id = ?`
	a, err := scanAccount(d.sql.QueryRowContext(ctx, query, d.layoutProfile(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get account %d: %w", id, err)
	}
	return a, nil
}

// ListAccounts returns all accounts in sidebar order: the ones the user
// reordered first, in that order, then the rest by id.
func (d *DB) ListAccounts(ctx context.Context) ([]Account, error) {
	const query = `
SELECT ` + accountColumns + accountFrom + `
WHERE a.id IN (SELECT account_id FROM profile_accounts WHERE profile_id = ?)
` + accountOrder
	return d.listAccounts(ctx, query, d.layoutProfile(), d.ScopedProfileID())
}

// ListAllAccounts returns every account on the install, whether or not the
// current profile shows it. Only the profile editor and account deletion want
// this; everything else works with what the profile can see.
func (d *DB) ListAllAccounts(ctx context.Context) ([]Account, error) {
	const query = `
SELECT ` + accountColumns + accountFrom + `
` + accountOrder
	return d.listAccounts(ctx, query, d.layoutProfile())
}

func (d *DB) listAccounts(ctx context.Context, query string, args ...any) ([]Account, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan account: %w", err)
		}
		accounts = append(accounts, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate accounts: %w", err)
	}
	return accounts, nil
}

// UpdateAccount updates the mutable fields of an existing account.
func (d *DB) UpdateAccount(ctx context.Context, a *Account) error {
	const query = `
UPDATE accounts
SET email = ?, display_name = ?, username = ?, imap_host = ?, imap_port = ?, smtp_host = ?, smtp_port = ?,
    imap_tls = ?, smtp_tls = ?, local_label = ?, use_local_label = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query,
		a.Email, a.DisplayName, a.Username, a.IMAPHost, a.IMAPPort, a.SMTPHost, a.SMTPPort,
		a.IMAPTLS, a.SMTPTLS, a.LocalLabel, boolToInt(a.UseLocalLabel), a.ID)
	if err != nil {
		return fmt.Errorf("storage: update account %d: %w", a.ID, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// DeleteAccount removes an account. Its folders, messages and attachment rows
// cascade away; attachment files on disk are the caller's concern.
func (d *DB) DeleteAccount(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete account %d: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountArchiveExport stores an account's export-on-archive configuration.
// Turning it on with an empty dir is allowed but exports nothing: the caller
// decides whether to insist on a directory.
func (d *DB) SetAccountArchiveExport(ctx context.Context, id int64, on bool, dir, subfolders, template string) error {
	const query = `
UPDATE accounts
SET export_on_archive = ?, export_dir = ?, export_subfolders = ?, export_name_template = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query, boolToInt(on), dir, subfolders, template, id)
	if err != nil {
		return fmt.Errorf("storage: set account %d archive export: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountSyncMaxParallel stores the account's parallel-sync override, or
// clears it when n is nil so the account follows the global setting.
func (d *DB) SetAccountSyncMaxParallel(ctx context.Context, id int64, n *int) error {
	var v any
	if n != nil {
		v = *n
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE accounts SET sync_max_parallel = ? WHERE id = ?`, v, id)
	if err != nil {
		return fmt.Errorf("storage: set account %d sync parallelism: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountPGPDefault stores how an account starts a new message: ” for
// unprotected, 'sign' or 'auto'. Validating the value is the caller's job.
func (d *DB) SetAccountPGPDefault(ctx context.Context, id int64, value string) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE accounts SET pgp_default = ? WHERE id = ?`, value, id)
	if err != nil {
		return fmt.Errorf("storage: set account %d pgp default: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountPasswordPromptDismissed records whether the missing-password prompt
// should stay quiet for an account.
func (d *DB) SetAccountPasswordPromptDismissed(ctx context.Context, id int64, dismissed bool) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE accounts SET password_prompt_dismissed = ? WHERE id = ?`, boolToInt(dismissed), id)
	if err != nil {
		return fmt.Errorf("storage: set account %d password prompt dismissed: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountCertTrust stores the certificates and CA an account trusts beyond
// the system roots, replacing what was there.
func (d *DB) SetAccountCertTrust(ctx context.Context, id int64, trustedCerts []string, caPEM string) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE accounts SET trusted_certs = ?, ca_pem = ? WHERE id = ?`,
		joinLines(trustedCerts), caPEM, id)
	if err != nil {
		return fmt.Errorf("storage: set account %d certificate trust: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// SetAccountProxy stores the route an account's connections take, replacing
// what was there. Validating it is the caller's job.
func (d *DB) SetAccountProxy(ctx context.Context, id int64, p AccountProxy) error {
	const query = `
UPDATE accounts
SET proxy_mode = ?, proxy_scheme = ?, proxy_host = ?, proxy_port = ?, proxy_username = ?,
    proxy_contacts_global = ?, proxy_oauth_global = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query, p.Mode, p.Scheme, p.Host, p.Port, p.Username,
		boolToInt(p.ContactsUseGlobal), boolToInt(p.OAuthUseGlobal), id)
	if err != nil {
		return fmt.Errorf("storage: set account %d proxy: %w", id, err)
	}
	return requireOneRow(res, ErrAccountNotFound)
}

// joinLines and splitLines store a list as one text column, one entry per
// line, empty entries dropped.
func joinLines(items []string) string {
	return strings.Join(items, "\n")
}

func splitLines(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// SetAccountPositions rewrites the sidebar order of the account sections in one
// transaction. Accounts not listed keep their current position. Positions start
// at 1, since 0 means "never reordered".
func (d *DB) SetAccountPositions(ctx context.Context, orderedIDs []int64) error {
	return d.setLayoutPositions(ctx, `
INSERT INTO profile_account_order (profile_id, account_id, position)
VALUES (?1, ?2, ?3)
ON CONFLICT(profile_id, account_id) DO UPDATE SET position = excluded.position`,
		orderedIDs, "accounts")
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row rowScanner) (*Account, error) {
	var (
		a         Account
		created   string
		local     int
		exportOn  int
		dismissed int
		useLabel  int
		trusted   string
		contacts  int
		oauth     int
		// nullable column: NULL means no per-account override.
		syncParallel sql.NullInt64
	)
	if err := row.Scan(&a.ID, &a.Email, &a.DisplayName, &a.Username, &a.IMAPHost, &a.IMAPPort,
		&a.SMTPHost, &a.SMTPPort, &a.IMAPTLS, &a.SMTPTLS, &created, &a.Position, &local,
		&exportOn, &a.ExportDir, &a.ExportSubfolders, &a.ExportNameTemplate,
		&a.PGPDefault, &dismissed, &a.LocalLabel, &useLabel,
		&syncParallel,
		&trusted, &a.CAPEM,
		&a.Proxy.Mode, &a.Proxy.Scheme, &a.Proxy.Host, &a.Proxy.Port, &a.Proxy.Username,
		&contacts, &oauth); err != nil {
		return nil, err
	}
	a.Proxy.ContactsUseGlobal = contacts != 0
	a.Proxy.OAuthUseGlobal = oauth != 0
	a.TrustedCerts = splitLines(trusted)
	a.ExportOnArchive = exportOn != 0
	a.PasswordPromptDismissed = dismissed != 0
	a.UseLocalLabel = useLabel != 0
	if syncParallel.Valid {
		n := int(syncParallel.Int64)
		a.SyncMaxParallel = &n
	}
	t, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	a.CreatedAt = t
	a.Local = local != 0
	return &a, nil
}

// requireOneRow turns a no rows affected result into notFound, so updates and
// deletes against a missing id report it instead of succeeding silently.
func requireOneRow(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: rows affected: %w", err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}

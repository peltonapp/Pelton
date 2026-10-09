package desktop

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/peltonapp/Pelton/internal/autoconfig"
	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/credentials"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/oauth"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// oauthFlowTimeout bounds how long the interactive consent flow may take.
const oauthFlowTimeout = 5 * time.Minute

// DiscoveredDTO is the autodiscovery result for the wizard.
type DiscoveredDTO struct {
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	// IMAPTLS and SMTPTLS are the security the source stated ("ssl" or
	// "starttls"), empty when it said nothing usable.
	IMAPTLS string `json:"imapTls"`
	SMTPTLS string `json:"smtpTls"`
	OAuth   bool   `json:"oauth"`
	// OAuthProvider is the provider key to sign in with when the servers belong
	// to one Pelton supports ("google"), empty otherwise.
	OAuthProvider string `json:"oauthProvider"`
	Source        string `json:"source"`
}

// DiscoverConfig resolves likely imap/smtp settings for an email address using
// autoconfig (ISPDB, the domain's well-known/autoconfig, then a guess). The
// wizard pre-fills the form with this; the user can still edit before testing.
func (a *App) DiscoverConfig(email string) (DiscoveredDTO, error) {
	d, err := autoconfig.Discover(a.ctx, a.httpClient(10*time.Second), email)
	if err != nil {
		return DiscoveredDTO{}, err
	}
	return DiscoveredDTO{
		IMAPHost:      d.IMAPHost,
		IMAPPort:      d.IMAPPort,
		SMTPHost:      d.SMTPHost,
		SMTPPort:      d.SMTPPort,
		IMAPTLS:       d.IMAPTLS,
		SMTPTLS:       d.SMTPTLS,
		OAuth:         d.OAuth,
		OAuthProvider: d.OAuthProvider,
		Source:        d.Source,
	}, nil
}

// ListOAuthProviders returns the supported oauth provider keys and labels so the
// wizard knows which providers can use the sign-in flow.
func (a *App) ListOAuthProviders() (map[string]string, error) {
	return oauth.Providers(), nil
}

// TestConnectionRequest carries the settings to verify before saving (password
// auth). OAuth is verified by the sign-in flow itself, so it is not tested here.
type TestConnectionRequest struct {
	Email string `json:"email"`
	// Username is the login name when it differs from the email; empty logs in
	// with Email.
	Username string `json:"username"`
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	// IMAPTLS pins the connection security: "ssl", "starttls", or empty to
	// derive it from the port. Sent so the test uses the same transport the
	// account will, instead of testing a different one.
	IMAPTLS  string `json:"imapTls"`
	Password string `json:"password"`
	// SMTPHost, SMTPPort and SMTPTLS are only checked for the certificate the
	// server presents, so one that needs trusting is caught here too.
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	SMTPTLS  string `json:"smtpTls"`
	// TrustedCerts and CAPEM are what the new mailbox will trust beyond the
	// system roots: fingerprints accepted in an earlier test, and a CA file.
	TrustedCerts []string `json:"trustedCerts"`
	CAPEM        string   `json:"caPem"`
	// Proxy is the route the new mailbox will take, so a mailbox only
	// reachable through its own proxy can pass the test before it exists.
	Proxy AccountProxyDTO `json:"proxy"`
}

// TestConnection verifies imap credentials by connecting and logging in, so the
// wizard can confirm before creating the account. A server certificate that
// does not verify is not an error: it comes back in the result, from both
// servers at once, for the user to review and trust (#446).
func (a *App) TestConnection(req TestConnectionRequest) (ConnectionTestDTO, error) {
	if req.CAPEM != "" {
		if _, err := certtrust.ParseCA(req.CAPEM); err != nil {
			return ConnectionTestDTO{}, err
		}
	}
	_, route, err := a.routeFromDTO(req.Proxy, 0)
	if err != nil {
		return ConnectionTestDTO{}, err
	}
	dial := route.DialContext()
	trust := certtrust.Trust{Pins: normalizePins(req.TrustedCerts), CAPEM: req.CAPEM}
	untrusted := a.probeCertificates(
		pimap.Config{Host: req.IMAPHost, Port: req.IMAPPort, TLS: imapTLSMode(req.IMAPTLS), Trust: trust, Dial: dial},
		psmtp.Config{Host: req.SMTPHost, Port: req.SMTPPort, TLS: smtpTLSMode(req.SMTPTLS), Trust: trust, Dial: dial},
	)
	if len(untrusted) > 0 {
		return ConnectionTestDTO{Untrusted: untrusted}, nil
	}

	username := req.Username
	if username == "" {
		username = req.Email
	}
	client, err := a.connectIMAP(pimap.Config{
		Host:     req.IMAPHost,
		Port:     req.IMAPPort,
		Username: username,
		Password: req.Password,
		TLS:      imapTLSMode(req.IMAPTLS),
		Trust:    trust,
		Dial:     dial,
	})
	if err != nil {
		return ConnectionTestDTO{}, err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return ConnectionTestDTO{}, err
	}
	return ConnectionTestDTO{}, client.Logout()
}

// AddAccountRequest is the metadata the wizard collected. For password auth
// Password is set; for oauth Provider and ClientID are set and the flow runs.
type AddAccountRequest struct {
	Email string `json:"email"`
	// DisplayName is the From name recipients see. LocalLabel is what this app
	// calls the mailbox instead when UseLocalLabel is set, and goes nowhere near
	// an outgoing message.
	DisplayName   string `json:"displayName"`
	LocalLabel    string `json:"localLabel"`
	UseLocalLabel bool   `json:"useLocalLabel"`
	// Username is the login name when it differs from the email; empty logs in
	// with Email.
	Username string `json:"username"`
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	// IMAPTLS and SMTPTLS pin the connection security: "ssl", "starttls", or
	// empty to derive it from the port.
	IMAPTLS string `json:"imapTls"`
	SMTPTLS string `json:"smtpTls"`
	// auth
	Password string `json:"password"`
	Provider string `json:"provider"`
	ClientID string `json:"clientId"`
	// ClientSecret is required by Google Desktop app clients and optional for
	// Microsoft Entra apps registered as confidential clients. Empty keeps the
	// public-client PKCE flow.
	ClientSecret string `json:"clientSecret"`
	// TrustedCerts and CAPEM are the certificates and CA the mailbox trusts
	// beyond the system roots, as accepted in the connection test.
	TrustedCerts []string `json:"trustedCerts"`
	CAPEM        string   `json:"caPem"`
	// Proxy is the route the mailbox's connections take (#457).
	Proxy AccountProxyDTO `json:"proxy"`
}

// AddPasswordAccount creates a password-authenticated account: it stores the
// metadata, files the password in the keyring, discovers the folder tree and
// runs an initial sync.
func (a *App) AddPasswordAccount(req AddAccountRequest) (AccountDTO, error) {
	if err := a.ready(); err != nil {
		return AccountDTO{}, err
	}
	secret := credentials.Secret{Method: credentials.MethodPassword, Password: req.Password}
	return a.createAccount(req, secret)
}

// AddOAuthAccount creates an oauth account: it runs the interactive PKCE flow
// (opening the system browser), stores the resulting refresh token in the
// keyring, then discovers folders and syncs. ClientID is the user's own
// registered desktop client id.
func (a *App) AddOAuthAccount(req AddAccountRequest) (AccountDTO, error) {
	if err := a.ready(); err != nil {
		return AccountDTO{}, err
	}
	route, err := accountProxyFromDTO(req.Proxy)
	if err != nil {
		return AccountDTO{}, err
	}
	client := a.httpClient(oauthTimeout)
	if !route.OAuthUseGlobal {
		_, cfg, err := a.routeFromDTO(req.Proxy, 0)
		if err != nil {
			return AccountDTO{}, err
		}
		client = cfg.HTTPClient(oauthTimeout)
	}
	secret, err := a.authorizeOAuth(client, req.Provider, req.ClientID, req.ClientSecret, req.Email)
	if err != nil {
		return AccountDTO{}, err
	}
	return a.createAccount(req, secret)
}

// authorizeOAuth runs the interactive consent flow in the system browser and
// returns the keyring secret for the tokens it yields. client carries the code
// exchange, which is the one part of the flow the app sends itself.
func (a *App) authorizeOAuth(client *http.Client, provider, clientID, clientSecret, email string) (credentials.Secret, error) {
	ctx, cancel := context.WithTimeout(oauthContext(a.ctx, client), oauthFlowTimeout)
	defer cancel()

	token, err := a.authorize(ctx, provider, clientID, clientSecret, email, func(url string) {
		wailsruntime.BrowserOpenURL(a.ctx, url)
	})
	if err != nil {
		return credentials.Secret{}, err
	}
	if token.RefreshToken == "" {
		return credentials.Secret{}, fmt.Errorf("pelton: provider returned no refresh token; re-consent may be required")
	}
	return credentials.Secret{
		Method:       credentials.MethodOAuth,
		Provider:     provider,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: token.RefreshToken,
		AccessToken:  token.AccessToken,
		Expiry:       token.Expiry,
	}, nil
}

// createAccount is the shared path for both auth methods: persist metadata, store
// the secret, discover folders, sync, and start idling. On any failure after the
// row is created it rolls the account back so a half-created account is not left.
func (a *App) createAccount(req AddAccountRequest, secret credentials.Secret) (AccountDTO, error) {
	if !validTLSMode(req.IMAPTLS) || !validTLSMode(req.SMTPTLS) {
		return AccountDTO{}, errUnknownTLSMode
	}
	route, err := accountProxyFromDTO(req.Proxy)
	if err != nil {
		return AccountDTO{}, err
	}
	account := &storage.Account{
		Email:         req.Email,
		DisplayName:   req.DisplayName,
		LocalLabel:    req.LocalLabel,
		UseLocalLabel: req.UseLocalLabel,
		Username:      req.Username,
		IMAPHost:      req.IMAPHost,
		IMAPPort:      req.IMAPPort,
		SMTPHost:      req.SMTPHost,
		SMTPPort:      req.SMTPPort,
		IMAPTLS:       req.IMAPTLS,
		SMTPTLS:       req.SMTPTLS,
		TrustedCerts:  normalizePins(req.TrustedCerts),
		CAPEM:         req.CAPEM,
		Proxy:         route,
	}
	if req.CAPEM != "" {
		if _, err := certtrust.ParseCA(req.CAPEM); err != nil {
			return AccountDTO{}, err
		}
	}
	id, err := a.store.CreateAccount(a.ctx, account)
	if err != nil {
		return AccountDTO{}, err
	}

	if err := credentials.Store(id, secret); err != nil {
		_ = a.store.DeleteAccount(a.ctx, id)
		return AccountDTO{}, err
	}
	if err := saveAccountProxyPassword(id, route, req.Proxy); err != nil {
		_ = credentials.Delete(id)
		_ = a.store.DeleteAccount(a.ctx, id)
		return AccountDTO{}, err
	}

	if err := a.discoverFolders(*account); err != nil {
		// keep the account; folders can be (re)discovered on next sync. surface
		// the error so the wizard can warn, but the account exists.
		a.log.Error("discover folders", "account", account.Email, "err", err)
	}

	// no sync yet. The wizard shows the discovered folders next so a huge
	// archive can be unchecked before anything is fetched, and calls
	// StartAccountSync once that choice is made (#173). Starting here would
	// download the folders the user is about to say they do not want.
	return toAccountDTO(*account), nil
}

// StartAccountSync runs the first sync of an account and parks it on idle,
// replacing any existing worker for that account.
func (a *App) StartAccountSync(accountID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if _, err := a.store.GetAccount(a.ctx, accountID); err != nil {
		return err
	}
	a.startAccountWorker(accountID)
	return nil
}

// discoverFolders lists the server's mailboxes and creates the storage folder
// rows, preserving the hierarchy via the per-server delimiter so the sidebar
// tree matches the server.
func (a *App) discoverFolders(account storage.Account) error {
	return a.discoverFoldersCtx(a.ctx, account)
}

func (a *App) discoverFoldersCtx(ctx context.Context, account storage.Account) error {
	return a.protocolFor(account).discoverFolders(ctx, account)
}

// ensureFolders discovers the folder tree over an already-connected client when
// the account has no folder rows yet: accounts restored from a backup import,
// or whose discovery failed during setup. With folders present it is a no-op.
func (a *App) ensureFolders(client mailClient, accountID int64) error {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return err
	}
	if len(folders) > 0 {
		return nil
	}
	serverFolders, err := client.ListFolders()
	if err != nil {
		return err
	}
	return a.createFolderTree(accountID, serverFolders)
}

// createFolderTree inserts folders parent-first so each child can resolve its
// parent id from the path above it, splitting on the server's delimiter.
func (a *App) createFolderTree(accountID int64, folders []pimap.Folder) error {
	// shallowest first so parents exist before their children.
	sort.SliceStable(folders, func(i, j int) bool {
		return depth(folders[i]) < depth(folders[j])
	})

	byPath := make(map[string]int64)
	for _, f := range folders {
		if f.HasAttr(goimap.MailboxAttrNonExistent) {
			continue
		}
		delim := delimString(f.Delimiter)
		name := f.Name
		var parentID *int64
		if delim != "" {
			if parent, last, ok := strings.CutLast(f.Name, delim); ok {
				name = last
				if pid, ok := byPath[parent]; ok {
					parentID = &pid
				}
			}
		}

		row := &storage.Folder{
			AccountID:  accountID,
			Name:       name,
			IMAPPath:   f.Name,
			RemoteID:   f.Name,
			Delimiter:  delim,
			ParentID:   parentID,
			Attributes: attrsToStrings(f.Attrs),
		}
		id, err := a.store.CreateFolder(a.ctx, row)
		if err != nil {
			return err
		}
		byPath[f.Name] = id
	}
	return nil
}

// depth counts how many delimiter segments a folder path has, for sort order.
func depth(f pimap.Folder) int {
	d := delimString(f.Delimiter)
	if d == "" {
		return 0
	}
	return strings.Count(f.Name, d)
}

// delimString renders the rune delimiter as a string, empty for a flat server.
func delimString(r rune) string {
	if r == 0 {
		return ""
	}
	return string(r)
}

// attrsToStrings converts imap mailbox attributes to plain strings for storage.
func attrsToStrings(attrs []goimap.MailboxAttr) []string {
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, string(a))
	}
	return out
}

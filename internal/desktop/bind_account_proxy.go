package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/proxy"
	"github.com/peltonapp/Pelton/internal/storage"
)

// accountProxyGlobal is how the ui names a mailbox that follows the app-wide
// proxy. Storage keeps it as the empty mode, so every existing mailbox reads
// as following it.
const accountProxyGlobal = "global"

// oauthTimeout bounds a token refresh or exchange made on a mailbox's behalf.
const oauthTimeout = 30 * time.Second

var errUnknownProxyMode = errors.New("pelton: unknown proxy mode")

// AccountProxyDTO is a mailbox's route as exchanged with the ui. Mode is
// "global", "off" (direct), "system" or "manual". Password is write-only, as
// in ProxyConfigDTO: empty with HasPassword keeps the stored one.
type AccountProxyDTO struct {
	Mode        string `json:"mode"`
	Scheme      string `json:"scheme"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	HasPassword bool   `json:"hasPassword"`
	// ContactsUseGlobal and OAuthUseGlobal send the mailbox's contacts sync
	// and its sign-in refresh the app-wide way instead of along its route.
	ContactsUseGlobal bool `json:"contactsUseGlobal"`
	OAuthUseGlobal    bool `json:"oauthUseGlobal"`
}

func toAccountProxyDTO(p storage.AccountProxy) AccountProxyDTO {
	mode := p.Mode
	if mode == storage.AccountProxyGlobal {
		mode = accountProxyGlobal
	}
	return AccountProxyDTO{
		Mode:              mode,
		Scheme:            orDefaultScheme(p.Scheme),
		Host:              p.Host,
		Port:              p.Port,
		Username:          p.Username,
		ContactsUseGlobal: p.ContactsUseGlobal,
		OAuthUseGlobal:    p.OAuthUseGlobal,
	}
}

// accountProxyFromDTO checks a route from the ui. Only a manual proxy has
// fields to check, but the others keep what was typed, so switching away and
// back does not lose the host.
func accountProxyFromDTO(dto AccountProxyDTO) (storage.AccountProxy, error) {
	p := storage.AccountProxy{
		Scheme:            dto.Scheme,
		Host:              dto.Host,
		Port:              dto.Port,
		Username:          dto.Username,
		ContactsUseGlobal: dto.ContactsUseGlobal,
		OAuthUseGlobal:    dto.OAuthUseGlobal,
	}
	switch dto.Mode {
	case accountProxyGlobal, "":
		p.Mode = storage.AccountProxyGlobal
	case proxy.ModeOff, proxy.ModeSystem:
		p.Mode = dto.Mode
	case proxy.ModeManual:
		p.Mode = storage.AccountProxyManual
		if err := manualProxy(p, "").Validate(); err != nil {
			return storage.AccountProxy{}, err
		}
	default:
		return storage.AccountProxy{}, errUnknownProxyMode
	}
	return p, nil
}

func manualProxy(p storage.AccountProxy, password string) proxy.Config {
	return proxy.Config{
		Mode:     proxy.ModeManual,
		Scheme:   p.Scheme,
		Host:     p.Host,
		Port:     p.Port,
		Username: p.Username,
		Password: password,
	}
}

// accountRoute is the proxy an account's mail connections take. An account
// with its own proxy never falls back to another route: if the proxy cannot be
// read, the connection fails and says so.
func (a *App) accountRoute(account storage.Account) (proxy.Config, error) {
	switch account.Proxy.Mode {
	case storage.AccountProxyGlobal:
		return a.currentProxy(), nil
	case storage.AccountProxyDirect, storage.AccountProxySystem:
		return proxy.Config{Mode: account.Proxy.Mode}, nil
	case storage.AccountProxyManual:
		password, err := credentials.LoadAccountProxyPassword(account.ID)
		if err != nil {
			return proxy.Config{}, err
		}
		return manualProxy(account.Proxy, password), nil
	default:
		return proxy.Config{}, errUnknownProxyMode
	}
}

// accountDial is the tcp dial hook for an account's imap and smtp clients,
// nil for a direct connection.
func (a *App) accountDial(account storage.Account) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	route, err := a.accountRoute(account)
	if err != nil {
		return nil, err
	}
	return route.DialContext(), nil
}

// accountHTTPClient is an http client for a request made on an account's
// behalf: along its route, or the app-wide way when useGlobal says so.
func (a *App) accountHTTPClient(account storage.Account, useGlobal bool, timeout time.Duration) (*http.Client, error) {
	if useGlobal {
		return a.httpClient(timeout), nil
	}
	route, err := a.accountRoute(account)
	if err != nil {
		return nil, err
	}
	return route.HTTPClient(timeout), nil
}

// contactsHTTPClient is the http client for an address book. A book added by
// hand, or one whose mailbox is gone, has no route of its own and takes the
// app-wide one.
func (a *App) contactsHTTPClient(accountID int64) (*http.Client, error) {
	if accountID == 0 {
		return a.httpClient(contactsTimeout), nil
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if errors.Is(err, storage.ErrAccountNotFound) {
		return a.httpClient(contactsTimeout), nil
	}
	if err != nil {
		return nil, err
	}
	return a.accountHTTPClient(*account, account.Proxy.ContactsUseGlobal, contactsTimeout)
}

// oauthContext carries the http client oauth2 uses for a token request, so a
// refresh or an exchange goes the way the mailbox's settings say.
func oauthContext(ctx context.Context, client *http.Client) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

// accountOAuthClient is the http client for an account's token requests.
func (a *App) accountOAuthClient(account storage.Account) (*http.Client, error) {
	return a.accountHTTPClient(account, account.Proxy.OAuthUseGlobal, oauthTimeout)
}

// routeFromDTO resolves a route the ui has not saved yet, for a test or a new
// mailbox. The typed password is used, or the one stored for accountID when
// the field was left on its placeholder.
func (a *App) routeFromDTO(dto AccountProxyDTO, accountID int64) (storage.AccountProxy, proxy.Config, error) {
	p, err := accountProxyFromDTO(dto)
	if err != nil {
		return storage.AccountProxy{}, proxy.Config{}, err
	}
	if p.Mode != storage.AccountProxyManual {
		route, err := a.accountRoute(storage.Account{ID: accountID, Proxy: p})
		return p, route, err
	}
	password := dto.Password
	if password == "" && dto.HasPassword && accountID != 0 {
		if password, err = credentials.LoadAccountProxyPassword(accountID); err != nil {
			return storage.AccountProxy{}, proxy.Config{}, err
		}
	}
	return p, manualProxy(p, password), nil
}

// saveAccountProxyPassword files the password of a mailbox's own proxy. Only a
// manual proxy keeps one; any other route clears it, like the app-wide
// setting does.
func saveAccountProxyPassword(accountID int64, p storage.AccountProxy, dto AccountProxyDTO) error {
	switch {
	case p.Mode != storage.AccountProxyManual:
		return credentials.DeleteAccountProxyPassword(accountID)
	case dto.Password != "":
		return credentials.StoreAccountProxyPassword(accountID, dto.Password)
	case !dto.HasPassword:
		return credentials.DeleteAccountProxyPassword(accountID)
	}
	return nil
}

// AccountProxyPasswordStored reports whether a mailbox's own proxy has a
// password in the keyring, so the editor can show a placeholder for it without
// the secret ever leaving the backend.
func (a *App) AccountProxyPasswordStored(accountID int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	password, err := credentials.LoadAccountProxyPassword(accountID)
	if err != nil {
		return false, err
	}
	return password != "", nil
}

// RouteTestRequest is a route to try and the servers to try it against, as
// the mailbox editor holds them, saved or not.
type RouteTestRequest struct {
	AccountID int64           `json:"accountId"`
	Proxy     AccountProxyDTO `json:"proxy"`
	IMAPHost  string          `json:"imapHost"`
	IMAPPort  int             `json:"imapPort"`
	SMTPHost  string          `json:"smtpHost"`
	SMTPPort  int             `json:"smtpPort"`
}

// TestAccountRoute opens a connection to the mailbox's imap and smtp servers
// along the given route and closes it again. Reaching them is the whole test:
// nothing is sent, so it needs no login and cannot lock an account out.
func (a *App) TestAccountRoute(req RouteTestRequest) error {
	if err := a.ready(); err != nil {
		return err
	}
	_, route, err := a.routeFromDTO(req.Proxy, req.AccountID)
	if err != nil {
		return err
	}
	dial := route.DialContext()
	if dial == nil {
		dial = (&net.Dialer{Timeout: 20 * time.Second}).DialContext
	}
	servers := []routeServer{
		{"IMAP", req.IMAPHost, req.IMAPPort},
		{"SMTP", req.SMTPHost, req.SMTPPort},
	}
	if req.AccountID != 0 {
		account, err := a.store.GetAccount(a.ctx, req.AccountID)
		if err != nil {
			return err
		}
		servers, err = a.protocolFor(*account).routeServers(*account, servers)
		if err != nil {
			return err
		}
	}
	for _, s := range servers {
		if s.host == "" {
			continue
		}
		addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		conn, err := dial(ctx, "tcp", addr)
		cancel()
		if err != nil {
			return fmt.Errorf("%s %s: %w", s.name, addr, err)
		}
		_ = conn.Close()
	}
	return nil
}

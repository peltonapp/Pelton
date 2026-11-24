package chat

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// userAgent is what Pelton calls itself to the homeserver.
const userAgent = "Pelton"

// Server is a homeserver found from what the user typed, with the ways it
// offers to sign in.
type Server struct {
	// Homeserver is the client API base URL.
	Homeserver string `json:"homeserver"`
	// ServerName is the name in the user's Matrix ID.
	ServerName string `json:"serverName"`
	// User is the user part, when the user typed a full Matrix ID.
	User      string        `json:"user"`
	Password  bool          `json:"password"`
	SSO       bool          `json:"sso"`
	Token     bool          `json:"token"`
	Providers []SSOProvider `json:"providers"`
}

// SSOProvider is one identity provider a homeserver offers for single
// sign-on.
type SSOProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// splitInput reads what the user typed: a Matrix ID, a server name or a URL.
func splitInput(input string) (serverName, user, base string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", "", errors.New("chat: enter a server or a Matrix ID")
	}
	if strings.HasPrefix(input, "@") {
		local, server, ok := strings.Cut(input[1:], ":")
		if !ok || local == "" || server == "" {
			return "", "", "", fmt.Errorf("chat: %q is not a Matrix ID", input)
		}
		return server, local, "", nil
	}
	if strings.Contains(input, "://") {
		u, perr := url.Parse(input)
		if perr != nil || u.Host == "" {
			return "", "", "", fmt.Errorf("chat: %q is not a server address", input)
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return "", "", "", fmt.Errorf("chat: %q is not a server address", input)
		}
		return u.Host, "", strings.TrimRight(input, "/"), nil
	}
	return input, "", "", nil
}

// Discover finds the homeserver behind what the user typed, through its
// .well-known file when it has one, and reads its sign-in methods. Nothing
// else is contacted: no identity server and no integration manager.
func (m *Manager) Discover(ctx context.Context, input string) (Server, error) {
	serverName, user, base, err := splitInput(input)
	if err != nil {
		return Server{}, err
	}
	hc := m.opts.HTTPClient()
	if base == "" {
		base = "https://" + serverName
		if wk, werr := mautrix.DiscoverClientAPIWithClient(ctx, hc, serverName); werr == nil && wk != nil && wk.Homeserver.BaseURL != "" {
			base = strings.TrimRight(wk.Homeserver.BaseURL, "/")
		}
	}
	cli, err := mautrix.NewClient(base, "", "")
	if err != nil {
		return Server{}, fmt.Errorf("chat: %q is not a server address: %w", base, err)
	}
	cli.Client = hc
	cli.UserAgent = userAgent
	if _, err := cli.Versions(ctx); err != nil {
		return Server{}, fmt.Errorf("chat: no Matrix server answered at %s: %w", base, err)
	}
	var flows struct {
		Flows []struct {
			Type      string `json:"type"`
			Providers []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"identity_providers"`
		} `json:"flows"`
	}
	if _, err := cli.MakeRequest(ctx, http.MethodGet, cli.BuildClientURL("v3", "login"), nil, &flows); err != nil {
		return Server{}, fmt.Errorf("chat: read sign-in methods: %w", err)
	}
	out := Server{Homeserver: base, ServerName: serverName, User: user, Providers: []SSOProvider{}}
	for _, f := range flows.Flows {
		switch mautrix.AuthType(f.Type) {
		case mautrix.AuthTypePassword:
			out.Password = true
		case mautrix.AuthTypeSSO:
			out.SSO = true
			for _, p := range f.Providers {
				out.Providers = append(out.Providers, SSOProvider{ID: p.ID, Name: p.Name})
			}
		case mautrix.AuthTypeToken:
			out.Token = true
		}
	}
	return out, nil
}

// newClient returns a client for a homeserver, not yet signed in.
func (m *Manager) newClient(homeserver string) (*mautrix.Client, error) {
	cli, err := mautrix.NewClient(homeserver, "", "")
	if err != nil {
		return nil, err
	}
	cli.Client = m.opts.HTTPClient()
	cli.UserAgent = userAgent
	return cli, nil
}

// LoginPassword signs in with a user name (or full Matrix ID) and password.
func (m *Manager) LoginPassword(ctx context.Context, homeserver, user, password string) error {
	if user == "" || password == "" {
		return errors.New("chat: enter your user name and password")
	}
	return m.login(ctx, homeserver, &mautrix.ReqLogin{
		Type:       mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: user},
		Password:   password,
	})
}

// LoginToken signs in with a login token, the fallback when a server hands
// one out some other way than the browser redirect.
func (m *Manager) LoginToken(ctx context.Context, homeserver, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("chat: enter the login token")
	}
	return m.login(ctx, homeserver, &mautrix.ReqLogin{Type: mautrix.AuthTypeToken, Token: strings.TrimSpace(token)})
}

// ssoTimeout is how long a browser sign-in may take.
const ssoTimeout = 10 * time.Minute

// LoginSSO signs in through the homeserver's single sign-on page in the
// system browser. A listener on the loopback address receives the login token
// the server redirects back with. providerID picks an identity provider, or
// empty for the server's own choice. It returns once signed in, cancelled
// (CancelSSO) or timed out.
func (m *Manager) LoginSSO(ctx context.Context, homeserver, providerID string) error {
	cli, err := m.newClient(homeserver)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("chat: open loopback listener: %w", err)
	}
	defer listener.Close()

	state, err := randomString(16)
	if err != nil {
		return err
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/sso/%s", listener.Addr().(*net.TCPAddr).Port, state)

	tokens := make(chan string, 1)
	srv := &http.Server{Handler: ssoHandler("/sso/"+state, tokens), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer srv.Close()

	path := mautrix.ClientURLPath{"v3", "login", "sso", "redirect"}
	if providerID != "" {
		path = append(path, providerID)
	}
	target := cli.BuildURLWithQuery(path, map[string]string{"redirectUrl": redirect})

	ctx, cancel := context.WithTimeout(ctx, ssoTimeout)
	m.mu.Lock()
	if m.ssoCancel != nil {
		m.ssoCancel()
	}
	m.ssoCancel = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.ssoCancel = nil
		m.mu.Unlock()
	}()

	if m.opts.OpenURL == nil {
		return errors.New("chat: no browser to sign in with")
	}
	m.opts.OpenURL(target)

	select {
	case token := <-tokens:
		return m.login(context.WithoutCancel(ctx), homeserver, &mautrix.ReqLogin{Type: mautrix.AuthTypeToken, Token: token})
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("chat: the browser sign-in timed out")
		}
		return errors.New("chat: the browser sign-in was cancelled")
	}
}

// CancelSSO abandons a browser sign-in that is waiting.
func (m *Manager) CancelSSO() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ssoCancel != nil {
		m.ssoCancel()
	}
}

// ssoHandler serves the redirect back from the homeserver. Only the
// unguessable path counts, so another page on this machine cannot hand in a
// token of its own choosing.
func ssoHandler(path string, tokens chan<- string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("loginToken")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if token == "" {
			fmt.Fprint(w, closePage("Sign-in failed. You can close this window."))
			return
		}
		fmt.Fprint(w, closePage("Signed in. You can close this window and return to Pelton."))
		select {
		case tokens <- token:
		default:
		}
	})
	return mux
}

func closePage(message string) string {
	return "<!doctype html><meta charset=utf-8><title>Pelton</title><body style=\"font-family:sans-serif;padding:40px;text-align:center\"><h2>Pelton</h2><p>" +
		html.EscapeString(message) + "</p></body>"
}

// login exchanges credentials for an access token, then starts the account
// on a fresh database: a new sign-in is a new device with new keys.
func (m *Manager) login(ctx context.Context, homeserver string, req *mautrix.ReqLogin) error {
	if _, err := m.active(); err == nil {
		return errors.New("chat: already signed in")
	}
	cli, err := m.newClient(homeserver)
	if err != nil {
		return err
	}
	req.InitialDeviceDisplayName = m.opts.DeviceName
	resp, err := cli.Login(ctx, req)
	if err != nil {
		return fmt.Errorf("chat: sign-in failed: %w", err)
	}
	if resp.WellKnown != nil && resp.WellKnown.Homeserver.BaseURL != "" {
		homeserver = strings.TrimRight(resp.WellKnown.Homeserver.BaseURL, "/")
	}
	pickle, err := randomString(32)
	if err != nil {
		return err
	}
	secret := Secret{AccessToken: resp.AccessToken, PickleKey: pickle}

	// a database left from an earlier sign-in belongs to another device.
	if err := removeDir(m.opts.Dir); err != nil {
		return err
	}
	st, err := openStore(m.dbPath())
	if err != nil {
		return err
	}
	acc := accountRow{Homeserver: homeserver, UserID: resp.UserID, DeviceID: resp.DeviceID}
	if err := st.saveAccount(ctx, acc); err != nil {
		st.close()
		return err
	}
	if err := m.opts.Secrets.Store(m.opts.SecretKey, secret); err != nil {
		st.close()
		return err
	}
	return m.startSession(ctx, st, acc, secret)
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("chat: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// parseUserID checks a Matrix user ID.
func parseUserID(s string) (id.UserID, error) {
	uid := id.UserID(strings.TrimSpace(s))
	if _, _, err := uid.Parse(); err != nil {
		return "", fmt.Errorf("chat: %q is not a Matrix ID", s)
	}
	return uid, nil
}

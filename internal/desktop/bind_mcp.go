package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

// MCP settings keys. The server is off unless settingMCPEnabled is "true"; the
// port is user-editable and the token is generated on first enable.
const (
	settingMCPEnabled = "mcp_enabled"
	settingMCPPort    = "mcp_port"
	settingMCPToken   = "mcp_token"

	// defaultMCPPort is the loopback port the server listens on until the user
	// changes it. Chosen to sit clear of common dev servers.
	defaultMCPPort = 8765

	// mcpListLimit bounds list_messages when the caller does not ask for a
	// specific count, keeping an agent from pulling a whole mailbox at once.
	mcpListLimit = 50
)

// MCPConfigDTO is the External-settings view of the MCP server: whether it is
// enabled, the loopback URL an agent connects to, the bearer token, and whether
// it is actually listening right now.
type MCPConfigDTO struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	URL     string `json:"url"`
	Running bool   `json:"running"`
}

// GetMCPConfig returns the current MCP server configuration for the settings ui.
func (a *App) GetMCPConfig() (MCPConfigDTO, error) {
	if err := a.ready(); err != nil {
		return MCPConfigDTO{}, err
	}
	port := a.mcpPort()
	return MCPConfigDTO{
		Enabled: a.boolSetting(settingMCPEnabled, false),
		Port:    port,
		Token:   a.mcpToken(),
		URL:     fmt.Sprintf("http://127.0.0.1:%d", port),
		Running: a.mcp != nil && a.mcp.Running(),
	}, nil
}

// SetMCPEnabled turns the server on or off and persists the choice. Enabling it
// generates a token if none exists yet, so the endpoint is never unauthenticated.
func (a *App) SetMCPEnabled(enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if enabled && a.mcpToken() == "" {
		if _, err := a.regenerateMCPToken(); err != nil {
			return err
		}
	}
	if err := a.store.Set(a.ctx, settingMCPEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	return a.applyMCPState()
}

// SetMCPPort changes the loopback port (1024-65535) and restarts the server if
// it is running so the new port takes effect immediately.
func (a *App) SetMCPPort(port int) error {
	if err := a.ready(); err != nil {
		return err
	}
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	if err := a.store.Set(a.ctx, settingMCPPort, strconv.Itoa(port)); err != nil {
		return err
	}
	return a.applyMCPState()
}

// RegenerateMCPToken issues a fresh bearer token, invalidating the old one, and
// restarts the server if running so the new token takes effect. Returns the new
// token for the settings ui to display.
func (a *App) RegenerateMCPToken() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	token, err := a.regenerateMCPToken()
	if err != nil {
		return "", err
	}
	if err := a.applyMCPState(); err != nil {
		return "", err
	}
	return token, nil
}

// regenerateMCPToken generates and persists a new random token without touching
// the server lifecycle.
func (a *App) regenerateMCPToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mcp token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := a.store.Set(a.ctx, settingMCPToken, token); err != nil {
		return "", err
	}
	return token, nil
}

// mcpPort reads the configured port, falling back to the default when unset or
// unparsable.
func (a *App) mcpPort() int {
	return a.intSetting(settingMCPPort, defaultMCPPort)
}

// mcpToken reads the stored bearer token ("" when never generated).
func (a *App) mcpToken() string {
	return a.stringSetting(settingMCPToken, "")
}

// startMCPIfEnabled starts the server at boot when the setting is on. Called
// from startBackgroundServices; a failure is logged, not fatal.
func (a *App) startMCPIfEnabled() {
	if !a.boolSetting(settingMCPEnabled, false) {
		return
	}
	if err := a.applyMCPState(); err != nil {
		a.log.Error("start mcp server", "err", err)
	}
}

// applyMCPState brings the running server in line with the settings: it stops
// any current listener, then starts a fresh one when the feature is enabled.
// Stopping first makes it safe to call after any change (enable, port, token).
func (a *App) applyMCPState() error {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()

	if a.mcp != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.mcp.Stop(ctx)
		cancel()
		a.mcp = nil
	}

	if !a.boolSetting(settingMCPEnabled, false) {
		return nil
	}

	srv := mcpserver.NewWithWriter(&mcpMailbox{app: a}, &mcpWriter{app: a}, a.log).
		WithScripts(&mcpScripts{app: a}, a.scriptLimiter())
	cfg := mcpserver.Config{
		Addr:        fmt.Sprintf("127.0.0.1:%d", a.mcpPort()),
		Token:       a.mcpToken(),
		Version:     a.version,
		Permissions: a.mcpPermissions(),
	}
	if err := srv.Start(cfg); err != nil {
		return err
	}
	a.mcp = srv
	return nil
}

// stopMCP shuts the server down on app shutdown.
func (a *App) stopMCP() {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.mcp.Stop(ctx)
	a.mcp = nil
}

// mcpMailbox adapts the store and search index to mcpserver.Mailbox. It reads
// only; the read-only guarantee of the MCP server rests on there being no
// mutating method here.
type mcpMailbox struct {
	app *App
}

func (m *mcpMailbox) ListAccounts(ctx context.Context) ([]mcpserver.Account, error) {
	accts, err := m.app.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.Account, 0, len(accts))
	for _, a := range accts {
		// an agent runs on this machine, so it gets the name the user calls the
		// mailbox rather than the one recipients see.
		out = append(out, mcpserver.Account{ID: a.ID, Email: a.Email, DisplayName: a.Label()})
	}
	return out, nil
}

func (m *mcpMailbox) ListFolders(ctx context.Context, accountID int64) ([]mcpserver.Folder, error) {
	folders, err := m.app.store.ListFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.Folder, 0, len(folders))
	for _, f := range folders {
		out = append(out, mcpserver.Folder{ID: f.ID, AccountID: f.AccountID, Name: f.Name, Path: f.IMAPPath})
	}
	return out, nil
}

func (m *mcpMailbox) ListMessages(ctx context.Context, folderID int64, limit int) ([]mcpserver.MessageSummary, error) {
	if limit <= 0 {
		limit = mcpListLimit
	}
	msgs, err := m.app.store.ListMessages(mcpCtx(ctx), folderID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.MessageSummary, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, mcpSummary(msg))
	}
	return out, nil
}

func (m *mcpMailbox) GetMessage(ctx context.Context, id int64) (*mcpserver.Message, error) {
	ctx = mcpCtx(ctx)
	msg, err := m.app.store.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	out := mcpserver.Message{
		MessageSummary: mcpSummary(*msg),
		To:             msg.ToAddresses,
		Cc:             msg.CcAddresses,
		MessageID:      msg.MessageID,
		SizeBytes:      msg.SizeBytes,
		BodyText:       msg.BodyPlain,
		BodyHTML:       msg.BodyHTML,
	}
	atts, err := m.app.store.ListAttachments(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, at := range atts {
		out.Attachments = append(out.Attachments, mcpserver.Attachment{
			Filename:    at.Filename,
			ContentType: at.ContentType,
			SizeBytes:   at.SizeBytes,
		})
	}
	return &out, nil
}

func (m *mcpMailbox) Search(ctx context.Context, params mcpserver.SearchParams) ([]mcpserver.MessageSummary, error) {
	if m.app.index == nil {
		return nil, errSearchUnavailable
	}
	q := search.Query{
		Text:    strings.TrimSpace(params.Query),
		From:    strings.TrimSpace(params.From),
		To:      strings.TrimSpace(params.To),
		Subject: strings.TrimSpace(params.Subject),
		Limit:   params.Limit,
	}
	if q.Text == "" && q.From == "" && q.To == "" && q.Subject == "" {
		return []mcpserver.MessageSummary{}, nil
	}
	hits, err := m.app.index.Search(q)
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.MessageSummary, 0, len(hits.Hits))
	for _, h := range hits.Hits {
		msg, err := m.app.store.GetMessage(mcpCtx(ctx), h.ID)
		if err != nil {
			continue
		}
		out = append(out, mcpSummary(*msg))
	}
	return out, nil
}

// mcpSummary projects a stored message into the MCP summary shape.
func mcpSummary(msg storage.Message) mcpserver.MessageSummary {
	return mcpserver.MessageSummary{
		ID:             msg.ID,
		AccountID:      msg.AccountID,
		FolderID:       msg.FolderID,
		Subject:        msg.Subject,
		FromName:       msg.FromName,
		FromAddress:    msg.FromAddress,
		Date:           msg.Date.Format(time.RFC3339),
		Unread:         msg.Flags&storage.FlagSeen == 0,
		Flagged:        msg.Flags&storage.FlagFlagged != 0,
		HasAttachments: msg.HasAttachments,
	}
}

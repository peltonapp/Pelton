package desktop

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/managesieve"
	"github.com/peltonapp/Pelton/internal/sieve"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Scripts Pelton keeps on the server: its own, and the version before its last
// save for "restore previous".
const (
	sieveScriptName = "pelton"
	sieveBackupName = "pelton-previous"
)

// Values of SieveStateDTO.Status.
const (
	sieveOK          = "ok"
	sieveUnsupported = "unsupported"
	sieveUnreachable = "unreachable"
	sieveAuth        = "auth"
	sieveCredentials = "credentials"
	sieveTLS         = "tls"
	sieveError       = "error"
)

// sieveTLSImplicit is the stored and exchanged value for implicit TLS; the
// empty value is STARTTLS.
const sieveTLSImplicit = "ssl"

// errSieveNotEditable refuses a model save over a script Pelton cannot model.
var errSieveNotEditable = errors.New("the script on the server was changed outside Pelton; edit it as text")

// errNothingToRestore means no save has happened that could be undone.
var errNothingToRestore = errors.New("there is no earlier version to restore")

// noSieveHosts are imap host suffixes of providers known to run no sieve
// service, so they are not dialed at all.
var noSieveHosts = []string{"gmail.com", "googlemail.com", "outlook.com", "office365.com", "hotmail.com", "mail.me.com", "yahoo.com"}

// SieveCapsDTO says which of Pelton's controls the server can back.
type SieveCapsDTO struct {
	Implementation string   `json:"implementation"`
	Extensions     []string `json:"extensions"`
	Vacation       bool     `json:"vacation"`
	// Dates is the date and relational pair a bounded out of office needs.
	Dates    bool `json:"dates"`
	FileInto bool `json:"fileInto"`
	Flags    bool `json:"flags"`
	Envelope bool `json:"envelope"`
	Include  bool `json:"include"`
}

// SieveStateDTO is an account's sieve service and Pelton's script on it.
type SieveStateDTO struct {
	AccountID int64 `json:"accountId"`
	// Status is "ok", "unsupported" (no sieve service), "unreachable",
	// "auth" (credentials refused), "credentials" (none stored), "tls" or
	// "error". Detail carries the underlying error text.
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Host, Port and TLS are the connection used; Manual says they were set
	// by hand.
	Host   string `json:"host"`
	Port   int    `json:"port"`
	TLS    string `json:"tls"`
	Manual bool   `json:"manual"`
	// Remembered is true when Unsupported comes from an earlier check.
	Remembered bool         `json:"remembered"`
	Caps       SieveCapsDTO `json:"caps"`
	// Exists and Active describe Pelton's script on the server.
	Exists bool `json:"exists"`
	Active bool `json:"active"`
	// Editable is false when the script cannot be modelled; Raw is its text
	// either way and Script the model when it is editable.
	Editable bool         `json:"editable"`
	Raw      string       `json:"raw"`
	Script   sieve.Script `json:"script"`
	// OtherActive names another script that is active now, "" when none is.
	OtherActive string `json:"otherActive"`
	HasPrevious bool   `json:"hasPrevious"`
	// Addresses are the account's own addresses, for the out of office.
	Addresses []string `json:"addresses"`
}

// SieveSaveDTO is the outcome of a save. When Problem is set the server
// refused the script at CHECKSCRIPT and nothing was written; Line is the line
// it named, 0 for none.
type SieveSaveDTO struct {
	State   SieveStateDTO `json:"state"`
	Problem string        `json:"problem"`
	Line    int           `json:"line"`
}

// SieveState connects to an account's sieve service and reports what is
// there. A server already found to have none is not dialed again; see
// RecheckSieve.
func (a *App) SieveState(accountID int64) (SieveStateDTO, error) {
	if err := a.ready(); err != nil {
		return SieveStateDTO{}, err
	}
	a.sieveMu.Lock()
	defer a.sieveMu.Unlock()
	var state SieveStateDTO
	err := a.withSieve(accountID, &state, func(c *managesieve.Client, _ *storage.AccountSieve) error {
		return loadSieveState(c, &state)
	})
	return state, err
}

// RecheckSieve forgets that the server had no sieve service and looks again.
func (a *App) RecheckSieve(accountID int64) (SieveStateDTO, error) {
	if err := a.ready(); err != nil {
		return SieveStateDTO{}, err
	}
	settings, err := a.store.GetAccountSieve(a.ctx, accountID)
	if err != nil {
		return SieveStateDTO{}, err
	}
	settings.Unsupported = false
	if err := a.store.SetAccountSieve(a.ctx, settings); err != nil {
		return SieveStateDTO{}, err
	}
	return a.SieveState(accountID)
}

// SetSieveConnection sets the sieve host, port and security by hand ("" host
// and 0 port go back to the defaults; tls is "starttls" or "ssl") and checks
// the server again.
func (a *App) SetSieveConnection(accountID int64, host string, port int, tls string) (SieveStateDTO, error) {
	if err := a.ready(); err != nil {
		return SieveStateDTO{}, err
	}
	host = strings.TrimSpace(host)
	if port < 0 || port > 65535 {
		return SieveStateDTO{}, fmt.Errorf("port %d is out of range", port)
	}
	switch tls {
	case "", "starttls":
		tls = ""
	case sieveTLSImplicit:
	default:
		return SieveStateDTO{}, errUnknownTLSMode
	}
	settings, err := a.store.GetAccountSieve(a.ctx, accountID)
	if err != nil {
		return SieveStateDTO{}, err
	}
	settings.Host, settings.Port, settings.TLS, settings.Unsupported = host, port, tls, false
	if err := a.store.SetAccountSieve(a.ctx, settings); err != nil {
		return SieveStateDTO{}, err
	}
	return a.SieveState(accountID)
}

// SaveSieveVacation writes the out of office into Pelton's script, keeping its
// rules, and activates it.
func (a *App) SaveSieveVacation(accountID int64, vacation sieve.Vacation) (SieveSaveDTO, error) {
	return a.saveSieveModel(accountID, func(s *sieve.Script) {
		v := vacation
		v.Addresses = cleanList(v.Addresses)
		v.Subject = strings.TrimSpace(v.Subject)
		v.From = strings.TrimSpace(v.From)
		s.Vacation = &v
	})
}

// SaveSieveRules writes the filter rules into Pelton's script, keeping its out
// of office, and activates it.
func (a *App) SaveSieveRules(accountID int64, rules []sieve.Rule) (SieveSaveDTO, error) {
	return a.saveSieveModel(accountID, func(s *sieve.Script) {
		out := make([]sieve.Rule, len(rules))
		for i, r := range rules {
			for j, c := range r.Conditions {
				if c.Field == sieve.FieldSize {
					r.Conditions[j].Value = strings.ToUpper(strings.TrimSpace(c.Value))
				}
			}
			out[i] = r
		}
		s.Rules = out
	})
}

// CheckSieveScript asks the server whether a script is valid, without
// storing anything. Its State carries the connection status only.
func (a *App) CheckSieveScript(accountID int64, text string) (SieveSaveDTO, error) {
	if err := a.ready(); err != nil {
		return SieveSaveDTO{}, err
	}
	a.sieveMu.Lock()
	defer a.sieveMu.Unlock()
	var out SieveSaveDTO
	err := a.withSieve(accountID, &out.State, func(c *managesieve.Client, _ *storage.AccountSieve) error {
		problem, line, err := checkScript(c, sieve.ToWire(text))
		out.Problem, out.Line = problem, line
		return err
	})
	return out, err
}

// SaveSieveScript writes text as Pelton's script as it is, after the server
// checked it, and activates it.
func (a *App) SaveSieveScript(accountID int64, text string) (SieveSaveDTO, error) {
	return a.saveSieve(accountID, func(*SieveStateDTO) (string, error) {
		return sieve.ToWire(text), nil
	})
}

// RestoreSievePrevious undoes the last save: Pelton's script goes back to the
// version before it and the script that was active then is active again.
// Restoring twice returns to where it started.
func (a *App) RestoreSievePrevious(accountID int64) (SieveStateDTO, error) {
	if err := a.ready(); err != nil {
		return SieveStateDTO{}, err
	}
	a.sieveMu.Lock()
	defer a.sieveMu.Unlock()
	var state SieveStateDTO
	err := a.withSieve(accountID, &state, func(c *managesieve.Client, settings *storage.AccountSieve) error {
		if !settings.HasPrevious {
			return errNothingToRestore
		}
		scripts, err := c.ListScripts()
		if err != nil {
			return err
		}
		active := activeScript(scripts)
		if hasScript(scripts, sieveBackupName) {
			previous, err := c.GetScript(sieveBackupName)
			if err != nil {
				return err
			}
			if hasScript(scripts, sieveScriptName) {
				current, err := c.GetScript(sieveScriptName)
				if err != nil {
					return err
				}
				if err := c.PutScript(sieveBackupName, current); err != nil {
					return err
				}
			}
			if err := c.PutScript(sieveScriptName, previous); err != nil {
				return err
			}
		}
		if err := c.SetActive(settings.PreviousActive); err != nil {
			return err
		}
		settings.PreviousActive = active
		if err := a.store.SetAccountSieve(a.ctx, *settings); err != nil {
			return err
		}
		return loadSieveState(c, &state)
	})
	return state, err
}

// saveSieveModel applies change to Pelton's script model and saves it.
func (a *App) saveSieveModel(accountID int64, change func(*sieve.Script)) (SieveSaveDTO, error) {
	return a.saveSieve(accountID, func(state *SieveStateDTO) (string, error) {
		if !state.Editable {
			return "", errSieveNotEditable
		}
		script := state.Script
		change(&script)
		caps := state.Caps
		other := state.OtherActive
		if other != "" && other != sieveBackupName && caps.Include && !slices.Contains(script.Includes, other) {
			script.Includes = append(script.Includes, other)
		}
		has := func(ext string) bool { return slices.Contains(caps.Extensions, ext) }
		if err := script.Validate(has); err != nil {
			return "", err
		}
		return sieve.ToWire(script.Generate()), nil
	})
}

// saveSieve runs every save: build the text, CHECKSCRIPT it, keep the current
// script as the backup, PUTSCRIPT and SETACTIVE. A script the server refuses
// comes back as Problem with nothing written.
func (a *App) saveSieve(accountID int64, build func(*SieveStateDTO) (string, error)) (SieveSaveDTO, error) {
	if err := a.ready(); err != nil {
		return SieveSaveDTO{}, err
	}
	a.sieveMu.Lock()
	defer a.sieveMu.Unlock()
	var out SieveSaveDTO
	err := a.withSieve(accountID, &out.State, func(c *managesieve.Client, settings *storage.AccountSieve) error {
		if err := loadSieveState(c, &out.State); err != nil {
			return err
		}
		text, err := build(&out.State)
		if err != nil {
			return err
		}
		problem, line, err := checkScript(c, text)
		if err != nil {
			return err
		}
		if problem != "" {
			out.Problem, out.Line = problem, line
			return nil
		}

		scripts, err := c.ListScripts()
		if err != nil {
			return err
		}
		if out.State.Exists {
			if err := c.PutScript(sieveBackupName, out.State.Raw); err != nil {
				return fmt.Errorf("keep the previous script: %w", err)
			}
		} else if hasScript(scripts, sieveBackupName) {
			if err := c.DeleteScript(sieveBackupName); err != nil {
				return err
			}
		}
		if err := c.PutScript(sieveScriptName, text); err != nil {
			return err
		}
		if err := c.SetActive(sieveScriptName); err != nil {
			return err
		}
		settings.PreviousActive = activeScript(scripts)
		settings.HasPrevious = true
		if err := a.store.SetAccountSieve(a.ctx, *settings); err != nil {
			return err
		}
		return loadSieveState(c, &out.State)
	})
	return out, err
}

// checkScript runs CHECKSCRIPT. A refusal is returned as problem and line;
// err is for everything else.
func checkScript(c *managesieve.Client, text string) (problem string, line int, err error) {
	err = c.CheckScript(text)
	var serr *managesieve.ServerError
	if errors.As(err, &serr) && serr.Status == "NO" {
		msg := serr.Message
		if msg == "" {
			msg = serr.Error()
		}
		return strings.TrimSpace(msg), serr.Line(), nil
	}
	return "", 0, err
}

// withSieve opens an authenticated session for an account and runs fn on it.
// Connection problems are not errors: they land in state.Status, and only a
// server with no sieve service at all is remembered as such. err is for
// failures past the connection.
func (a *App) withSieve(accountID int64, state *SieveStateDTO, fn func(*managesieve.Client, *storage.AccountSieve) error) error {
	found, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	account := *found
	if account.Local {
		return errors.New("local folders have no server")
	}
	settings, err := a.store.GetAccountSieve(a.ctx, accountID)
	if err != nil {
		return err
	}
	cfg := sieveConfig(account, settings)
	*state = SieveStateDTO{
		AccountID: accountID,
		Host:      cfg.Host,
		Port:      cfg.Port,
		TLS:       settings.TLS,
		Manual:    settings.Host != "" || settings.Port != 0 || settings.TLS != "",
		Addresses: accountAddresses(account),
	}

	if !state.Manual && (settings.Unsupported || knownWithoutSieve(cfg.Host)) {
		state.Status, state.Remembered = sieveUnsupported, settings.Unsupported
		return nil
	}

	imapCfg, err := a.resolveIMAP(account)
	if err != nil {
		state.Status, state.Detail = sieveError, err.Error()
		if errors.Is(err, errNoCredentials) {
			state.Status = sieveCredentials
		}
		return nil
	}
	cfg.Username, cfg.Password, cfg.OAuth2Token = imapCfg.Username, imapCfg.Password, imapCfg.OAuth2Token
	cfg.Trust = imapCfg.Trust
	if imapCfg.Dial != nil {
		cfg.Dial = managesieve.DialFunc(imapCfg.Dial)
	}

	c, err := managesieve.Dial(cfg)
	if err != nil {
		state.Status, state.Detail = classifySieveDial(err)
		if state.Status == sieveUnsupported && !state.Manual {
			settings.Unsupported, settings.CheckedAt = true, time.Now()
			return a.store.SetAccountSieve(a.ctx, settings)
		}
		return nil
	}
	defer func() { _ = c.Close() }()
	if err := c.Authenticate(); err != nil {
		state.Status, state.Detail = sieveError, err.Error()
		if errors.Is(err, managesieve.ErrAuthFailed) {
			state.Status = sieveAuth
		}
		return nil
	}
	state.Status = sieveOK
	if err := fn(c, &settings); err != nil {
		return err
	}
	state.HasPrevious = settings.HasPrevious
	_ = c.Logout()
	return nil
}

// loadSieveState reads the capabilities and Pelton's script into state.
func loadSieveState(c *managesieve.Client, state *SieveStateDTO) error {
	caps := c.Capabilities()
	state.Caps = SieveCapsDTO{
		Implementation: caps.Implementation,
		Extensions:     caps.Sieve,
		Vacation:       caps.HasExtension(sieve.ExtVacation),
		Dates:          caps.HasExtension(sieve.ExtDate) && caps.HasExtension(sieve.ExtRelational),
		FileInto:       caps.HasExtension(sieve.ExtFileInto),
		Flags:          caps.HasExtension(sieve.ExtIMAP4Flags),
		Envelope:       caps.HasExtension(sieve.ExtEnvelope),
		Include:        caps.HasExtension(sieve.ExtInclude),
	}
	if state.Caps.Extensions == nil {
		state.Caps.Extensions = []string{}
	}
	scripts, err := c.ListScripts()
	if err != nil {
		return err
	}
	state.Exists, state.Active, state.OtherActive = false, false, ""
	state.Raw, state.Script, state.Editable = "", sieve.Script{}, true
	for _, s := range scripts {
		if s.Name == sieveScriptName {
			state.Exists, state.Active = true, s.Active
		} else if s.Active {
			state.OtherActive = s.Name
		}
	}
	if !state.Exists {
		return nil
	}
	raw, err := c.GetScript(sieveScriptName)
	if err != nil {
		return err
	}
	state.Raw = raw
	parsed, err := sieve.Parse(raw)
	if err != nil {
		state.Editable = false
		return nil
	}
	state.Script = *parsed
	return nil
}

// sieveConfig is where an account's sieve service is: the hand-set values,
// or the imap host on port 4190 with STARTTLS.
func sieveConfig(account storage.Account, settings storage.AccountSieve) managesieve.Config {
	cfg := managesieve.Config{Host: account.IMAPHost, Port: managesieve.DefaultPort, TLS: managesieve.TLSStartTLS}
	if settings.Host != "" {
		cfg.Host = settings.Host
	}
	if settings.Port != 0 {
		cfg.Port = settings.Port
	}
	if settings.TLS == sieveTLSImplicit {
		cfg.TLS = managesieve.TLSImplicit
	}
	return cfg
}

// classifySieveDial turns a failed connection into a status. Refused,
// silent and non-sieve ports mean the server has no sieve service; a name
// that does not resolve or a missing route mean the machine is offline.
func classifySieveDial(err error) (status, detail string) {
	detail = err.Error()
	var dnsErr *net.DNSError
	switch {
	case untrusted(err), errors.Is(err, managesieve.ErrNoStartTLS):
		return sieveTLS, detail
	case errors.As(err, &dnsErr):
		return sieveUnreachable, detail
	case errors.Is(err, managesieve.ErrNotManageSieve):
		return sieveUnsupported, detail
	}
	msg := strings.ToLower(detail)
	for _, needle := range []string{"network is unreachable", "no route to host", "host is down"} {
		if strings.Contains(msg, needle) {
			return sieveUnreachable, detail
		}
	}
	for _, needle := range []string{"connection refused", "i/o timeout", "deadline exceeded", "connection reset", "connection lost"} {
		if strings.Contains(msg, needle) {
			return sieveUnsupported, detail
		}
	}
	return sieveError, detail
}

func untrusted(err error) bool {
	_, ok := certtrust.Untrusted(err)
	return ok
}

// knownWithoutSieve reports whether host belongs to a provider that runs no
// sieve service.
func knownWithoutSieve(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range noSieveHosts {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// accountAddresses are the addresses an account receives mail at that the
// app knows of: its email, and its login name when that is an address too.
func accountAddresses(account storage.Account) []string {
	out := []string{account.Email}
	if strings.Contains(account.Username, "@") && !strings.EqualFold(account.Username, account.Email) {
		out = append(out, account.Username)
	}
	return out
}

func activeScript(scripts []managesieve.Script) string {
	for _, s := range scripts {
		if s.Active {
			return s.Name
		}
	}
	return ""
}

func hasScript(scripts []managesieve.Script, name string) bool {
	for _, s := range scripts {
		if s.Name == name {
			return true
		}
	}
	return false
}

// cleanList trims entries and drops empty ones and repeats.
func cleanList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

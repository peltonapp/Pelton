// Package ai lets scripts ask a language model: the providers the user set
// up, the kinds of provider Pelton can talk to, and the ai module with its
// block actions and conditions, registered through the public scripting API.
//
// Nothing is sent anywhere until the user adds a provider. Every call names
// where the data goes in the permission it needs: "ai:<provider id>" for a
// provider that may leave this device, "ai_local:<provider id>" for one whose
// address is on this device or the local network. Providers are called over
// plain HTTP with JSON; there is no vendor SDK.
package ai

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// KeyNeed says whether a kind of provider takes an API key.
type KeyNeed string

// The key needs.
const (
	KeyRequired KeyNeed = "required"
	KeyOptional KeyNeed = "optional"
	KeyNone     KeyNeed = "none"
)

// Kind is a kind of provider: how to talk to it and what a new one starts
// with.
type Kind struct {
	ID string `json:"id"`
	// Label is the English name shown when the frontend has no string for
	// "ai.kind.<id>".
	Label string `json:"label"`
	// BaseURL prefills a new provider of this kind; empty when the user must
	// enter one.
	BaseURL string  `json:"baseUrl"`
	Key     KeyNeed `json:"key"`
	// Model prefills the default model, empty when the user picks one from
	// the fetched list.
	Model  string `json:"model"`
	Driver Driver `json:"-"`
}

// Driver speaks one provider protocol.
type Driver interface {
	// Chat sends one request and returns the model's answer. It never
	// streams.
	Chat(ctx context.Context, e *Endpoint, req Request) (Response, error)
	// Models lists the model ids the provider offers.
	Models(ctx context.Context, e *Endpoint) ([]string, error)
}

var (
	kindsMu sync.RWMutex
	kinds   []Kind
)

var kindID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// RegisterKind adds a kind of provider, so another package can teach Pelton a
// further protocol.
func RegisterKind(k Kind) error {
	if !kindID.MatchString(k.ID) {
		return fmt.Errorf("ai: %q is not a valid kind id", k.ID)
	}
	if k.Driver == nil {
		return fmt.Errorf("ai: kind %q has no driver", k.ID)
	}
	switch k.Key {
	case KeyRequired, KeyOptional, KeyNone:
	default:
		return fmt.Errorf("ai: kind %q has key need %q", k.ID, k.Key)
	}
	kindsMu.Lock()
	defer kindsMu.Unlock()
	if slices.ContainsFunc(kinds, func(o Kind) bool { return o.ID == k.ID }) {
		return fmt.Errorf("ai: kind %q is already registered", k.ID)
	}
	kinds = append(kinds, k)
	return nil
}

// Kinds returns every kind in registration order.
func Kinds() []Kind {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	return slices.Clone(kinds)
}

// KindByID returns a registered kind.
func KindByID(id string) (Kind, bool) {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	i := slices.IndexFunc(kinds, func(k Kind) bool { return k.ID == id })
	if i < 0 {
		return Kind{}, false
	}
	return kinds[i], true
}

func init() {
	for _, k := range []Kind{
		{ID: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1", Key: KeyRequired, Driver: openAI{tokens: "max_completion_tokens", format: formatSchema}},
		{ID: "anthropic", Label: "Anthropic", BaseURL: "https://api.anthropic.com/v1", Key: KeyRequired, Driver: anthropic{}},
		{ID: "mistral", Label: "Mistral", BaseURL: "https://api.mistral.ai/v1", Key: KeyRequired, Driver: openAI{tokens: "max_tokens", format: formatObject}},
		{ID: "ollama", Label: "Ollama", BaseURL: "http://localhost:11434", Key: KeyOptional, Driver: ollama{}},
		{ID: "vllm", Label: "vLLM", BaseURL: "http://localhost:8000/v1", Key: KeyOptional, Driver: openAI{tokens: "max_tokens", format: formatSchema}},
		{ID: "openai_compatible", Label: "OpenAI-compatible", Key: KeyOptional, Driver: openAI{tokens: "max_tokens"}},
	} {
		if err := RegisterKind(k); err != nil {
			panic(err)
		}
	}
}

// Provider is one model service the user set up. Its API key is not part of
// it: the app keeps keys in the credential store under the provider's id.
type Provider struct {
	// ID is a short name made from the name when the provider is added. It
	// never changes, since approvals name it.
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl"`
	// Model is the model used when a step names none.
	Model string `json:"model"`
	// Models is the list last fetched from the provider, for the pickers.
	Models  []string `json:"models,omitempty"`
	Default bool     `json:"default,omitempty"`
}

// ErrNoProvider is returned when a step needs a provider and none is set up.
var ErrNoProvider = errors.New("no AI provider is set up; add one in Settings")

var idPart = regexp.MustCompile(`[^a-z0-9]+`)

// maxIDLen keeps a provider id short enough to read in a permission.
const maxIDLen = 32

// NewID makes a provider id from a name that none of taken uses: lower case
// letters and digits joined by hyphens, with a number added when needed.
func NewID(name string, taken []string) string {
	base := strings.Trim(idPart.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(base) > maxIDLen {
		base = strings.TrimRight(base[:maxIDLen], "-")
	}
	if base == "" {
		base = "provider"
	}
	id := base
	for n := 2; slices.Contains(taken, id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// Validate checks a provider: a name, a known kind and an http or https base
// URL without a user name or password.
func (p Provider) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("ai: a provider needs a name")
	}
	if _, ok := KindByID(p.Kind); !ok {
		return fmt.Errorf("ai: unknown kind of provider %q", p.Kind)
	}
	_, err := parseBase(p.BaseURL)
	return err
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: the address is not valid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("ai: the address must start with http:// or https://")
	}
	if u.User != nil {
		return nil, errors.New("ai: the address cannot carry a user name or password; use the API key")
	}
	if u.Hostname() == "" {
		return nil, errors.New("ai: the address has no host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("ai: the address cannot have a query or a fragment")
	}
	return u, nil
}

// localAddr reports whether an address is on this device or a private
// network.
func localAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// Local reports whether a base URL names this device or the local network
// without looking anything up: "localhost" or a loopback or private address
// written out. Any other host name counts as leaving the device, which is
// what the settings show; a call still checks the addresses the name
// resolves to.
func Local(baseURL string) bool {
	u, err := parseBase(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && localAddr(ip)
}

// Pick returns the provider a step names, by id or by name ignoring case, or
// the default for "". With no default marked the first provider is used.
func Pick(list []Provider, want string) (Provider, error) {
	if len(list) == 0 {
		return Provider{}, ErrNoProvider
	}
	want = strings.TrimSpace(want)
	if want == "" {
		for _, p := range list {
			if p.Default {
				return p, nil
			}
		}
		return list[0], nil
	}
	for _, p := range list {
		if p.ID == want {
			return p, nil
		}
	}
	for _, p := range list {
		if strings.EqualFold(p.Name, want) {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("there is no AI provider %q", want)
}

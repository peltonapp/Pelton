package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/ai"
)

// The AI provider settings. Both belong to this device: a backup leaves them
// out, and the providers' API keys are only ever in the credential store.
const (
	// settingAIProviders holds the providers as a JSON list.
	settingAIProviders = "ai_providers"
	// settingAIScriptOptions holds each script's AI switches, as a JSON
	// object of script id to options.
	settingAIScriptOptions = "ai_script_options"
)

// aiTestTimeout bounds a connection test or a model list fetch.
const aiTestTimeout = 30 * time.Second

// aiMu serializes the read, change and write of the AI settings.
var aiMu sync.Mutex

// aiRecord is a provider as stored, with whether it has a key in the
// credential store, so listing never reads the store.
type aiRecord struct {
	ai.Provider
	HasKey bool `json:"hasKey,omitempty"`
}

// AIKindDTO is a kind of provider the user can add.
type AIKindDTO struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	BaseURL string `json:"baseUrl"`
	// Key is "required", "optional" or "none".
	Key   string `json:"key"`
	Model string `json:"model"`
}

// AIProviderDTO is a provider as the settings show it.
type AIProviderDTO struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	BaseURL string   `json:"baseUrl"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
	Default bool     `json:"default"`
	HasKey  bool     `json:"hasKey"`
	// Local is set when the address names this device or a private network
	// outright; anything else counts as leaving the device.
	Local bool `json:"local"`
}

// AIProviderInput is a provider being added (empty ID) or changed. Key is a
// new API key, "" to keep the stored one; ClearKey removes it.
type AIProviderInput struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	BaseURL  string   `json:"baseUrl"`
	Model    string   `json:"model"`
	Models   []string `json:"models"`
	Key      string   `json:"key"`
	ClearKey bool     `json:"clearKey"`
}

// AITestDTO is the outcome of a connection test.
type AITestDTO struct {
	OK bool `json:"ok"`
	// Message is the provider's error when the test failed.
	Message string `json:"message"`
	// Local is set when the address resolved to this device or a private
	// network only.
	Local bool `json:"local"`
}

// AIScriptOptionsDTO are the AI switches of one script.
type AIScriptOptionsDTO struct {
	// LogText puts the text sent to the model and its answers in the run log.
	LogText bool `json:"logText"`
}

func (a *App) aiNetwork() ai.Network {
	return ai.Network{Dial: a.scriptDial, Lookup: a.resolveAIHost}
}

func (a *App) resolveAIHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if a.aiLookup != nil {
		return a.aiLookup(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func (a *App) aiRecords() []aiRecord {
	var out []aiRecord
	if raw := a.stringSetting(settingAIProviders, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			a.log.Warn("ai: read the providers", "err", err)
			return nil
		}
	}
	return out
}

func (a *App) saveAIRecords(list []aiRecord) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingAIProviders, string(b)); err != nil {
		return err
	}
	if a.scripts != nil {
		ai.SetAvailable(a.scripts.Registry(), len(list) > 0)
	}
	return nil
}

// registerScriptAI adds the ai module to the scripts' registry. It reads the
// providers on every call, so a change in Settings applies to the next step.
func (a *App) registerScriptAI(r *scripting.Registry) error {
	err := ai.Register(r, ai.Config{
		Network: a.aiNetwork(),
		Providers: func(context.Context) ([]ai.Provider, error) {
			aiMu.Lock()
			defer aiMu.Unlock()
			recs := a.aiRecords()
			out := make([]ai.Provider, 0, len(recs))
			for _, rec := range recs {
				out = append(out, rec.Provider)
			}
			return out, nil
		},
		Key: func(id string) (string, error) {
			aiMu.Lock()
			has := slices.ContainsFunc(a.aiRecords(), func(rec aiRecord) bool { return rec.ID == id && rec.HasKey })
			aiMu.Unlock()
			if !has {
				return "", nil
			}
			return credentials.LoadAIKey(id)
		},
		Options: func(scriptID int64) ai.ScriptOptions {
			o := a.aiScriptOptions()[strconv.FormatInt(scriptID, 10)]
			return ai.ScriptOptions{LogText: o.LogText}
		},
	})
	if err != nil {
		return err
	}
	ai.SetAvailable(r, len(a.aiRecords()) > 0)
	return nil
}

func providerDTO(r aiRecord) AIProviderDTO {
	models := r.Models
	if models == nil {
		models = []string{}
	}
	return AIProviderDTO{
		ID: r.ID, Name: r.Name, Kind: r.Kind, BaseURL: r.BaseURL, Model: r.Model, Models: models,
		Default: r.Default, HasKey: r.HasKey, Local: ai.Local(r.BaseURL),
	}
}

// AIProviderKinds lists the kinds of provider that can be added.
func (a *App) AIProviderKinds() []AIKindDTO {
	kinds := ai.Kinds()
	out := make([]AIKindDTO, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, AIKindDTO{ID: k.ID, Label: k.Label, BaseURL: k.BaseURL, Key: string(k.Key), Model: k.Model})
	}
	return out
}

// AIProviders lists the providers the user set up. It contacts nothing.
func (a *App) AIProviders() ([]AIProviderDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	list := a.aiRecords()
	out := make([]AIProviderDTO, 0, len(list))
	for _, r := range list {
		out = append(out, providerDTO(r))
	}
	return out, nil
}

// inputProvider turns an input into a checked provider.
func inputProvider(in AIProviderInput) (ai.Provider, ai.Kind, error) {
	p := ai.Provider{
		ID: in.ID, Name: strings.TrimSpace(in.Name), Kind: in.Kind,
		BaseURL: strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), Model: strings.TrimSpace(in.Model),
	}
	for _, m := range in.Models {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(p.Models, m) {
			p.Models = append(p.Models, m)
		}
	}
	if err := p.Validate(); err != nil {
		return ai.Provider{}, ai.Kind{}, err
	}
	kind, _ := ai.KindByID(p.Kind)
	return p, kind, nil
}

// SaveAIProvider adds a provider, or changes the one with the input's id.
// The first provider becomes the default. A new key goes to the credential
// store, never to the settings.
func (a *App) SaveAIProvider(in AIProviderInput) (AIProviderDTO, error) {
	if err := a.ready(); err != nil {
		return AIProviderDTO{}, err
	}
	p, kind, err := inputProvider(in)
	if err != nil {
		return AIProviderDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, aiTestTimeout)
	err = a.aiNetwork().CheckAddress(ctx, p)
	cancel()
	if err != nil {
		return AIProviderDTO{}, err
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	list := a.aiRecords()
	rec := aiRecord{Provider: p}
	i := -1
	if in.ID == "" {
		ids := make([]string, 0, len(list))
		for _, r := range list {
			ids = append(ids, r.ID)
		}
		rec.ID = ai.NewID(p.Name, ids)
		rec.Default = len(list) == 0
	} else {
		i = slices.IndexFunc(list, func(r aiRecord) bool { return r.ID == in.ID })
		if i < 0 {
			return AIProviderDTO{}, fmt.Errorf("ai: there is no provider %q", in.ID)
		}
		rec.Default, rec.HasKey = list[i].Default, list[i].HasKey
	}
	switch {
	case kind.Key == ai.KeyNone || in.ClearKey:
		if rec.HasKey {
			if err := credentials.DeleteAIKey(rec.ID); err != nil {
				return AIProviderDTO{}, err
			}
		}
		rec.HasKey = false
	case in.Key != "":
		if err := credentials.StoreAIKey(rec.ID, strings.TrimSpace(in.Key)); err != nil {
			return AIProviderDTO{}, err
		}
		rec.HasKey = true
	}
	if kind.Key == ai.KeyRequired && !rec.HasKey {
		return AIProviderDTO{}, fmt.Errorf("ai: %s needs an API key", kind.Label)
	}
	if i < 0 {
		list = append(list, rec)
	} else {
		list[i] = rec
	}
	if err := a.saveAIRecords(list); err != nil {
		return AIProviderDTO{}, err
	}
	return providerDTO(rec), nil
}

// DeleteAIProvider removes a provider and its key. When it was the default,
// the first one left becomes the default.
func (a *App) DeleteAIProvider(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	list := a.aiRecords()
	i := slices.IndexFunc(list, func(r aiRecord) bool { return r.ID == id })
	if i < 0 {
		return nil
	}
	gone := list[i]
	list = slices.Delete(list, i, i+1)
	if gone.Default && len(list) > 0 {
		list[0].Default = true
	}
	if err := a.saveAIRecords(list); err != nil {
		return err
	}
	return credentials.DeleteAIKey(id)
}

// SetDefaultAIProvider makes a provider the one steps use when they name
// none.
func (a *App) SetDefaultAIProvider(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	list := a.aiRecords()
	if !slices.ContainsFunc(list, func(r aiRecord) bool { return r.ID == id }) {
		return fmt.Errorf("ai: there is no provider %q", id)
	}
	for i := range list {
		list[i].Default = list[i].ID == id
	}
	return a.saveAIRecords(list)
}

// aiKey returns the key a test uses: the one typed, else the stored one.
func (a *App) aiKey(in AIProviderInput) (string, error) {
	if in.Key != "" || in.ClearKey || in.ID == "" {
		return strings.TrimSpace(in.Key), nil
	}
	for _, r := range a.aiRecords() {
		if r.ID == in.ID && r.HasKey {
			return credentials.LoadAIKey(r.ID)
		}
	}
	return "", nil
}

// openAI resolves an input's provider and connects to it for a test.
func (a *App) openAI(ctx context.Context, in AIProviderInput) (*ai.Endpoint, ai.Target, ai.Kind, error) {
	p, kind, err := inputProvider(in)
	if err != nil {
		return nil, ai.Target{}, ai.Kind{}, err
	}
	key, err := a.aiKey(in)
	if err != nil {
		return nil, ai.Target{}, ai.Kind{}, err
	}
	if kind.Key == ai.KeyRequired && key == "" {
		return nil, ai.Target{}, ai.Kind{}, fmt.Errorf("ai: %s needs an API key", kind.Label)
	}
	ep, t, err := a.aiNetwork().Open(ctx, p, key, aiTestTimeout)
	if err != nil {
		return nil, ai.Target{}, ai.Kind{}, err
	}
	return ep, t, kind, nil
}

// TestAIProvider sends one tiny request to a provider, saved or not, with
// its model. It is the only time the settings contact a provider besides
// fetching the model list, and only when the user asks.
func (a *App) TestAIProvider(in AIProviderInput) (AITestDTO, error) {
	if err := a.ready(); err != nil {
		return AITestDTO{}, err
	}
	if strings.TrimSpace(in.Model) == "" {
		return AITestDTO{}, errors.New("ai: pick a model to test with")
	}
	ctx, cancel := context.WithTimeout(a.ctx, aiTestTimeout)
	defer cancel()
	ep, t, kind, err := a.openAI(ctx, in)
	if err != nil {
		return AITestDTO{}, err
	}
	_, err = kind.Driver.Chat(ctx, ep, ai.Request{
		Model:     strings.TrimSpace(in.Model),
		Messages:  []ai.Message{{Role: "user", Text: "Reply with the word OK."}},
		MaxTokens: 32,
	})
	if err != nil {
		return AITestDTO{Message: err.Error(), Local: t.Local}, nil
	}
	return AITestDTO{OK: true, Local: t.Local}, nil
}

// FetchAIModels asks a provider for its models. For a saved provider the list
// is kept for the pickers.
func (a *App) FetchAIModels(in AIProviderInput) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, aiTestTimeout)
	defer cancel()
	ep, _, kind, err := a.openAI(ctx, in)
	if err != nil {
		return nil, err
	}
	models, err := kind.Driver.Models(ctx, ep)
	if err != nil {
		return nil, err
	}
	if in.ID != "" {
		aiMu.Lock()
		defer aiMu.Unlock()
		list := a.aiRecords()
		if i := slices.IndexFunc(list, func(r aiRecord) bool { return r.ID == in.ID }); i >= 0 {
			list[i].Models = models
			if err := a.saveAIRecords(list); err != nil {
				return nil, err
			}
		}
	}
	return models, nil
}

// TestScriptAskingAI dry runs a script as TestScript does, except that its AI
// steps really ask the model, so a branch on the answer can be tried. Every
// change, including those the model asks for, stays a planned step. Only a
// provider the user approved for the saved script scriptID is asked, and
// only while the code in the editor is the code they approved.
func (a *App) TestScriptAskingAI(scriptID int64, in ScriptInput, messageID int64) (ScriptRunDTO, error) {
	return a.testScript(ai.WithModelInTest(a.ctx), in, messageID, scriptID)
}

func (a *App) aiScriptOptions() map[string]AIScriptOptionsDTO {
	out := map[string]AIScriptOptionsDTO{}
	if raw := a.stringSetting(settingAIScriptOptions, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			a.log.Warn("ai: read the script options", "err", err)
		}
	}
	return out
}

// AIScriptOptions returns a script's AI switches, all off when never set.
func (a *App) AIScriptOptions(scriptID int64) (AIScriptOptionsDTO, error) {
	if err := a.ready(); err != nil {
		return AIScriptOptionsDTO{}, err
	}
	return a.aiScriptOptions()[strconv.FormatInt(scriptID, 10)], nil
}

// SetAIScriptOptions changes a script's AI switches.
func (a *App) SetAIScriptOptions(scriptID int64, opts AIScriptOptionsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	all := a.aiScriptOptions()
	key := strconv.FormatInt(scriptID, 10)
	if opts == (AIScriptOptionsDTO{}) {
		delete(all, key)
	} else {
		all[key] = opts
	}
	return a.saveAIScriptOptions(all)
}

func (a *App) saveAIScriptOptions(all map[string]AIScriptOptionsDTO) error {
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return a.store.Set(a.ctx, settingAIScriptOptions, string(b))
}

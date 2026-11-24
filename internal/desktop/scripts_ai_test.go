package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/ai"
)

const aiTestKey = "sk-desktop-test-9f8e7d6c5b4a"

// fakeAI is an OpenAI-style server that answers every chat with text and
// lists two models.
func fakeAI(t *testing.T, text string) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"m2"},{"id":"m1"}]}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": text}}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 2},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &auths
}

func TestAIProviderKeyStaysInTheCredentialStore(t *testing.T) {
	a, _ := newScriptTestApp(t)
	srv, auths := fakeAI(t, "fine")
	if a, _ := a.scripts.Registry().Action("ai.ask"); a.Missing == "" {
		t.Fatal("AI steps are offered with no provider set up")
	}
	p, err := a.SaveAIProvider(AIProviderInput{Name: "My OpenAI", Kind: "openai", BaseURL: srv.URL + "/v1/", Model: "m1", Key: aiTestKey})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "my-openai" || !p.Default || !p.HasKey || !p.Local || p.BaseURL != srv.URL+"/v1" {
		t.Fatalf("provider %+v", p)
	}
	if act, _ := a.scripts.Registry().Action("ai.ask"); act.Missing != "" {
		t.Fatal("AI steps stay missing with a provider set up")
	}
	if key, err := credentials.LoadAIKey(p.ID); err != nil || key != aiTestKey {
		t.Fatalf("stored key %q %v", key, err)
	}

	sc, err := a.CreateScript(luaInput(`local r = ai.ask("private prompt text") print(r.ok, r.text)`, "ai:"+p.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	run, err := a.RunScript(sc.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ok" {
		t.Fatalf("%s %s", run.Status, run.Error)
	}
	if (*auths)[0] != "Bearer "+aiTestKey {
		t.Fatalf("authorization %q", (*auths)[0])
	}

	settings, err := a.store.AllSettings(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := a.exportSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exported[settingAIProviders]; ok {
		t.Fatal("the providers travel in a backup")
	}
	script, err := a.ExportScript(sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := a.ScriptRuns(sc.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	everything, _ := json.Marshal([]any{settings, exported, script, runs})
	if strings.Contains(string(everything), aiTestKey) {
		t.Fatal("the key reached the database, a backup, an export or the run log")
	}
	if strings.Contains(string(everything), "private prompt text") {
		t.Fatal("the prompt reached the run log without the log text switch")
	}

	if err := a.SetAIScriptOptions(sc.ID, AIScriptOptionsDTO{LogText: true}); err != nil {
		t.Fatal(err)
	}
	if opts, _ := a.AIScriptOptions(sc.ID); !opts.LogText {
		t.Fatalf("options %+v", opts)
	}
	run, _ = a.RunScript(sc.ID, 0)
	var logged []string
	for _, s := range run.Steps {
		if s.Kind == scripting.StepLog {
			logged = append(logged, s.Summary)
		}
	}
	if !strings.Contains(strings.Join(logged, "\n"), "ai sent: private prompt text") {
		t.Fatalf("logged %q", logged)
	}

	if err := a.DeleteAIProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	if key, _ := credentials.LoadAIKey(p.ID); key != "" {
		t.Fatal("the key outlived its provider")
	}
	if list, _ := a.AIProviders(); len(list) != 0 {
		t.Fatalf("providers %v", list)
	}
}

func TestScriptTestRunAsksTheModelOnlyWhenTold(t *testing.T) {
	a, _ := newScriptTestApp(t)
	srv, auths := fakeAI(t, "Friends")
	p, err := a.SaveAIProvider(AIProviderInput{Name: "Box", Kind: "openai_compatible", BaseURL: srv.URL + "/v1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	in := luaInput(`print(ai.classify("hi", {"Bills", "Friends"}).label)`, "ai_local:"+p.ID)
	run, err := a.TestScript(in, 0)
	if err != nil || run.Status != "ok" || len(*auths) != 0 {
		t.Fatalf("%v %s %s, %d requests", err, run.Status, run.Error, len(*auths))
	}
	run, err = a.TestScriptAskingAI(0, in, 0)
	if err != nil || run.Status != "error" || len(*auths) != 0 {
		t.Fatalf("an unsaved, unapproved script asked the model: %v %s %s, %d requests", err, run.Status, run.Error, len(*auths))
	}
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if run, _ = a.TestScriptAskingAI(sc.ID, in, 0); run.Status != "error" || len(*auths) != 0 {
		t.Fatalf("a saved script not yet approved asked the model: %s, %d requests", run.Status, len(*auths))
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	edited := in
	edited.Source += "\nprint(1)"
	if run, _ = a.TestScriptAskingAI(sc.ID, edited, 0); run.Status != "error" || len(*auths) != 0 {
		t.Fatalf("code changed since the approval asked the model: %s, %d requests", run.Status, len(*auths))
	}
	run, err = a.TestScriptAskingAI(sc.ID, in, 0)
	if err != nil || run.Status != "ok" || len(*auths) != 1 {
		t.Fatalf("%v %s %s, %d requests", err, run.Status, run.Error, len(*auths))
	}
	var printed []string
	for _, s := range run.Steps {
		if s.Kind == scripting.StepLog {
			printed = append(printed, s.Summary)
		}
	}
	if printed[len(printed)-1] != "Friends" {
		t.Fatalf("printed %v", printed)
	}
}

func TestCloudAIProviderNeedsHTTPS(t *testing.T) {
	a, _ := newScriptTestApp(t)
	var resolved atomic.Value
	resolved.Store(netip.MustParseAddr("203.0.113.20"))
	a.aiLookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{resolved.Load().(netip.Addr)}, nil
	}
	plain := AIProviderInput{Name: "Cloud", Kind: "openai_compatible", BaseURL: "http://ai.example/v1", Model: "m"}
	if _, err := a.SaveAIProvider(plain); !errors.Is(err, ai.ErrPlainHTTP) {
		t.Fatalf("saved a cloud provider over http: %v", err)
	}
	if _, err := a.TestAIProvider(plain); !errors.Is(err, ai.ErrPlainHTTP) {
		t.Fatalf("tested a cloud provider over http: %v", err)
	}
	if _, err := a.SaveAIProvider(AIProviderInput{Name: "Cloud", Kind: "openai_compatible", BaseURL: "https://ai.example/v1", Model: "m"}); err != nil {
		t.Fatalf("refused an https provider: %v", err)
	}

	resolved.Store(netip.MustParseAddr("192.168.4.4"))
	box, err := a.SaveAIProvider(AIProviderInput{Name: "Box", Kind: "ollama", BaseURL: "http://gpu-box.lan:11434", Model: "m"})
	if err != nil {
		t.Fatalf("refused a server on the local network over http: %v", err)
	}
	sc, err := a.CreateScript(luaInput(`local r = ai.ask("x", {provider = "box"}) print(r.ok)`, "ai:"+box.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	resolved.Store(netip.MustParseAddr("203.0.113.21"))
	run, err := a.RunScript(sc.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "error" || !strings.Contains(run.Error, "https://") {
		t.Fatalf("a provider that moved off the network was asked over http: %s %s", run.Status, run.Error)
	}
}

func TestAIProviderSettings(t *testing.T) {
	a, _ := newScriptTestApp(t)
	srv, auths := fakeAI(t, "OK")
	if _, err := a.SaveAIProvider(AIProviderInput{Name: "Cloud", Kind: "anthropic", BaseURL: "https://api.anthropic.com/v1"}); err == nil {
		t.Fatal("saved a provider that needs a key without one")
	}
	first, err := a.SaveAIProvider(AIProviderInput{Name: "Box", Kind: "ollama", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.SaveAIProvider(AIProviderInput{Name: "Box", Kind: "openai_compatible", BaseURL: srv.URL + "/v1", Key: aiTestKey})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "box-2" || second.Default || !first.Default {
		t.Fatalf("%+v %+v", first, second)
	}
	if err := a.SetDefaultAIProvider(second.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := a.AIProviders()
	if list[0].Default || !list[1].Default {
		t.Fatalf("defaults %+v", list)
	}

	in := AIProviderInput{ID: second.ID, Name: "Box", Kind: "openai_compatible", BaseURL: srv.URL + "/v1"}
	models, err := a.FetchAIModels(in)
	if err != nil || strings.Join(models, ",") != "m1,m2" {
		t.Fatalf("models %v %v", models, err)
	}
	if list, _ := a.AIProviders(); strings.Join(list[1].Models, ",") != "m1,m2" {
		t.Fatalf("models not kept: %v", list[1].Models)
	}
	in.Model = "m1"
	res, err := a.TestAIProvider(in)
	if err != nil || !res.OK || !res.Local {
		t.Fatalf("test %+v %v", res, err)
	}
	if last := (*auths)[len(*auths)-1]; last != "Bearer "+aiTestKey {
		t.Fatalf("the test did not use the stored key: %q", last)
	}

	in.Model = "m1"
	in.ClearKey = true
	if _, err := a.SaveAIProvider(in); err != nil {
		t.Fatal(err)
	}
	if key, _ := credentials.LoadAIKey(second.ID); key != "" {
		t.Fatal("clearing the key left it stored")
	}
	if err := a.DeleteAIProvider(second.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := a.AIProviders(); len(list) != 1 || !list[0].Default {
		t.Fatalf("after deleting the default: %+v", list)
	}
}

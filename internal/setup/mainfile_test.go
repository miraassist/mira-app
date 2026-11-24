package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira-app/internal/api"
	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
)

// modelServer answers the trial request with a tool call and, when context
// is above 0, names that context size in its list of models.
func modelServer(t *testing.T, context int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			if context == 0 {
				w.WriteHeader(404)
				return
			}
			fmt.Fprintf(w, `{"data":[{"id":"flash","context_length":%d},{"id":"qwen","context_length":%[1]d}]}`, context)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":"turn_on","arguments":"{}"}}]}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// mainSetup is a service waiting to be set up, with its mira.yaml.
type mainSetup struct {
	h         *Handler
	file      config.File
	url       string
	token     string
	restarted *atomic.Bool
}

func newMainSetup(t *testing.T, file config.File) mainSetup {
	t.Helper()
	st := newStore(t)
	s, _ := Load(context.Background(), st)
	var restarted atomic.Bool
	h := &Handler{Store: st, Token: s.Token, ID: "id1", Version: "t", Pairing: true, SetupMode: true,
		Restart: func() { restarted.Store(true) }, Log: slog.Default(),
		Clock: clock.NewFake(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)), Config: file}
	mux := api.NewMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return mainSetup{h: h, file: file, url: srv.URL, token: s.Token, restarted: &restarted}
}

// post sends a body as the integration does, raw JSON.
func (m mainSetup) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", m.url+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

func llmJSON(kind, url, model, key string) string {
	raw, _ := json.Marshal(map[string]string{"kind": kind, "base_url": url, "model": model, "api_key": key})
	return string(raw)
}

// loadFile is the file as Mira loads it at her start, its secrets filled in.
func loadFile(t *testing.T, f config.File) config.Config {
	t.Helper()
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(raw, f.Secrets.Lookup(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatalf("mira.yaml does not load: %v\n%s", err, raw)
	}
	return cfg
}

// mainEntry is models.main as written in the file.
func mainEntry(t *testing.T, f config.File) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		Models map[string]map[string]any `yaml:"models"`
	}
	if err := yaml.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	return top.Models[config.MainModel]
}

func secrets(t *testing.T, f config.File) map[string]string {
	t.Helper()
	all, err := f.Secrets.Load()
	if err != nil {
		t.Fatal(err)
	}
	return all
}

// MF-01 The first setup with a provider writes main into mira.yaml
func TestMF01_TheFirstSetupWithAProviderWritesMainIntoMiraYaml(t *testing.T) {
	// Given a service that was never paired and has no mira.yaml
	m := newMainSetup(t, newFile(t))
	models := modelServer(t, 0)
	// and a model of an older setup in the database (it is gone after)
	m.h.Store.SetKV(context.Background(), keyLLM, `{"kind":"cloud","base_url":"https://old/v1","model":"old"}`)
	// When the integration pairs with a provider's model and a key
	code, body := m.post(t, "/v1/pair", `{"code":"","ha_url":"http://ha","ha_token":"x","llm":`+llmJSON("cloud", models+"/", "flash", "sk-1")+`}`)
	if code != 200 {
		t.Fatalf("pairing: %d %s", code, body)
	}
	// Then mira.yaml has models.main with the endpoint, the model, privacy cloud and the token's reference
	e := mainEntry(t, m.file)
	if e["endpoint"] != models || e["model"] != "flash" || e["privacy"] != "cloud" || e["token"] != "${MIRA_MODEL_MAIN_TOKEN}" {
		t.Errorf("main %v", e)
	}
	// And the secrets file holds the key
	if secrets(t, m.file)["MIRA_MODEL_MAIN_TOKEN"] != "sk-1" {
		t.Error("the key is not in the secrets file")
	}
	// And kv has no "setup.llm"
	if s, _ := Load(context.Background(), m.h.Store); s.LLM != nil {
		t.Errorf("setup.llm is still there: %+v", s.LLM)
	}
	// And after the restart the conversation takes that model
	if main := loadFile(t, m.file).Main(); main.Model != "flash" || main.APIKey != "sk-1" || main.Privacy != config.PrivacyCloud {
		t.Errorf("main as loaded: %+v", main)
	}
}

// MF-02 An own server without a key is written as local
func TestMF02_AnOwnServerWithoutAKeyIsWrittenAsLocal(t *testing.T) {
	m := newMainSetup(t, newFile(t))
	models := modelServer(t, 8192)
	if code, body := m.post(t, "/v1/pair", `{"code":"","ha_url":"http://ha","ha_token":"x","llm":`+llmJSON("custom", models, "qwen", "")+`}`); code != 200 {
		t.Fatalf("pairing: %d %s", code, body)
	}
	e := mainEntry(t, m.file)
	if e["privacy"] != "local" || e["cache_prompt"] != true || e["warmup_on_start"] != true || e["warmup_on_prefix_change"] != true {
		t.Errorf("main %v", e)
	}
	if _, ok := e["token"]; ok {
		t.Errorf("a token without a key: %v", e)
	}
	if e["context_tokens"] != 8192 {
		t.Errorf("context %v", e["context_tokens"])
	}
	if main := loadFile(t, m.file).Main(); main.Privacy != config.PrivacyLocal || main.Parallel != 1 {
		t.Errorf("main as loaded: %+v", main)
	}
}

const fileWithMain = `# my models
models:
  main:
    endpoint: http://lmstudio:1234/v1
    model: qwen
    privacy: local
    parallel: 2
    pin_slots: { voice: 0 }
    fallback: lite
    timeout: 90s
    context_tokens: 8192
    warm_slots: true
  lite:
    endpoint: http://lmstudio:1234/v1
    model: small
    privacy: local
`

// MF-03 A setup over an existing main keeps what it does not decide
func TestMF03_ASetupOverAnExistingMainKeepsWhatItDoesNotDecide(t *testing.T) {
	// Given mira.yaml with models.main that has a fallback, a timeout and pin_slots
	file := newFile(t)
	os.WriteFile(file.Path, []byte(fileWithMain), 0o644)
	m := newMainSetup(t, file)
	models := modelServer(t, 1048576)
	// When the integration's "Configure → AI model" sends another model
	if code, body := m.post(t, "/v1/setup", `{"llm":`+llmJSON("cloud", models, "flash", "sk-2")+`}`); code != 200 {
		t.Fatalf("setup: %d %s", code, body)
	}
	// Then models.main has the new endpoint, model, token and privacy
	e := mainEntry(t, file)
	if e["endpoint"] != models || e["model"] != "flash" || e["privacy"] != "cloud" || e["token"] != "${MIRA_MODEL_MAIN_TOKEN}" ||
		e["context_tokens"] != 1048576 {
		t.Errorf("main %v", e)
	}
	// And it keeps its fallback and its timeout
	if e["fallback"] != "lite" || e["timeout"] != "90s" {
		t.Errorf("kept: %v", e)
	}
	// And it has no pin_slots and no parallel (and no warm slot as a cloud model)
	for _, k := range []string{"pin_slots", "parallel", "warm_slots", "cache_prompt", "warmup_on_start"} {
		if _, ok := e[k]; ok {
			t.Errorf("%s stayed: %v", k, e)
		}
	}
	raw, _ := os.ReadFile(file.Path)
	if !strings.HasPrefix(string(raw), "# my models") || !strings.Contains(string(raw), "lite:") {
		t.Errorf("the file's comment or other model is gone:\n%s", raw)
	}
}

// MF-04 A model that does not say its context drops the old size
func TestMF04_AModelThatDoesNotSayItsContextDropsTheOldSize(t *testing.T) {
	file := newFile(t)
	os.WriteFile(file.Path, []byte(fileWithMain), 0o644)
	m := newMainSetup(t, file)
	if code, body := m.post(t, "/v1/setup", `{"llm":`+llmJSON("custom", modelServer(t, 0), "other", "")+`}`); code != 200 {
		t.Fatalf("setup: %d %s", code, body)
	}
	if e := mainEntry(t, file); e["context_tokens"] != nil {
		t.Errorf("the old size stayed: %v", e)
	}
}

// MF-06 A setup and a save on the page at once lose nothing
func TestMF06_ASetupAndASaveOnThePageAtOnceLoseNothing(t *testing.T) {
	file := newFile(t)
	os.WriteFile(file.Path, []byte(fileWithMain), 0o644)
	st := newStore(t)
	waiting := make(chan struct{}, 1)
	config.LockWaits = func(string) { waiting <- struct{}{} }
	t.Cleanup(func() { config.LockWaits = nil })
	// Given a save of the model "smart" on Mira's page that holds the file's lock
	inPage, release, pageDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		pageDone <- file.Edit(func(top *yaml.Node) error {
			close(inPage)
			<-release
			smart := &yaml.Node{Kind: yaml.MappingNode}
			config.MapSet(smart, "endpoint", config.Scalar("https://e/v1"))
			config.MapSet(smart, "model", config.Scalar("big"))
			config.MapSet(smart, "privacy", config.Scalar("cloud"))
			config.MapSet(config.MapChild(top, "models"), "smart", smart)
			return nil
		})
	}()
	<-inPage
	// When the setup sends a model for main at the same time
	setupDone := make(chan error, 1)
	go func() {
		setupDone <- WriteMain(context.Background(), st, file, LLM{Kind: KindCloud, BaseURL: "https://p/v1", Model: "flash", APIKey: "k"})
	}()
	// Then the setup waits for the page's save
	select {
	case <-waiting:
	case err := <-setupDone:
		t.Fatalf("the setup wrote while the page held the file: %v", err)
	}
	close(release)
	if err := <-pageDone; err != nil {
		t.Fatal(err)
	}
	if err := <-setupDone; err != nil {
		t.Fatal(err)
	}
	// And mira.yaml holds both smart and the new main
	cfg := loadFile(t, file)
	if _, ok := cfg.Models["smart"]; !ok || cfg.Main().Model != "flash" {
		t.Errorf("models %v, main %+v", cfg.Models, cfg.Main())
	}
}

// MF-07 A setup into a file that cannot be written leaves everything as it was
func TestMF07_ASetupIntoAFileThatCannotBeWrittenLeavesEverythingAsItWas(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	// Given a mira.yaml that is read only
	file := newFile(t)
	os.WriteFile(file.Path, []byte(fileWithMain), 0o444)
	file.Secrets.Set(map[string]string{"OTHER": "o"})
	before, _ := os.ReadFile(file.Secrets.Path)
	m := newMainSetup(t, file)
	// When the integration's "Configure → AI model" sends a model
	code, body := m.post(t, "/v1/setup", `{"llm":`+llmJSON("cloud", modelServer(t, 0), "flash", "sk-3")+`}`)
	// Then the answer is 422 and names the file and the cause
	if code != 422 || !strings.Contains(body, file.Path) || !strings.Contains(body, "read-only") {
		t.Errorf("answer %d %s", code, body)
	}
	// And the secrets file is as before
	if after, _ := os.ReadFile(file.Secrets.Path); !bytes.Equal(before, after) {
		t.Error("the secrets file changed")
	}
	// And the service does not restart
	if m.restarted.Load() {
		t.Error("restarted")
	}
	// The same at pairing: nothing is kept, the service stays unpaired.
	code, body = m.post(t, "/v1/pair", `{"code":"","ha_url":"http://ha","ha_token":"x","llm":`+llmJSON("cloud", modelServer(t, 0), "flash", "sk-3")+`}`)
	if s, _ := Load(context.Background(), m.h.Store); code != 422 || !s.Open || s.HAURL != "" {
		t.Errorf("pairing into a read-only file: %d %s, %+v", code, body, s)
	}
}

// MF-08 An integration of 0.40 sets the model up as before
func TestMF08_AnIntegrationOf040SetsTheModelUpAsBefore(t *testing.T) {
	// Given the integration's requests of version 0.40 (custom_components/mira/api.py:
	// pair and setup), as they go over the wire
	models := modelServer(t, 0)
	llm := `{"kind": "cloud", "base_url": "` + models + `", "model": "flash", "api_key": "sk-4"}`
	m := newMainSetup(t, newFile(t))
	// When they reach the new service
	if code, body := m.post(t, "/v1/pair", `{"code": "", "ha_url": "http://ha", "ha_token": "x", "llm": `+llm+`}`); code != 200 {
		t.Fatalf("pair: %d %s", code, body)
	}
	if loadFile(t, m.file).Main().Model != "flash" {
		t.Error("pairing: main is not in mira.yaml")
	}
	llm = strings.Replace(llm, `"flash"`, `"pro"`, 1)
	if code, body := m.post(t, "/v1/setup", `{"llm": `+llm+`}`); code != 200 {
		t.Fatalf("setup: %d %s", code, body)
	}
	// Then each is accepted and main is in mira.yaml
	if main := loadFile(t, m.file).Main(); main.Model != "pro" || main.APIKey != "sk-4" {
		t.Errorf("Configure → AI model: main %+v", main)
	}
}

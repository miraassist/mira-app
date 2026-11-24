package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/setup"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/ui"
)

// MF-05 Mira's page edits main after a setup
func TestMF05_MirasPageEditsMainAfterASetup(t *testing.T) {
	a, e, path := editApp(t)
	ctx := context.Background()
	// Given a service whose main came from the integration's setup
	if err := setup.WriteMain(ctx, a.store, e.file, setup.LLM{Kind: setup.KindCloud, BaseURL: "https://provider/v1",
		Model: "flash", APIKey: "setup-key"}); err != nil {
		t.Fatal(err)
	}
	if st := e.State(ctx); st.Models["main"].FromHA || st.Models["main"].Model != "flash" {
		t.Errorf("main on the page: %+v", st.Models["main"])
	}
	// When main is saved on Mira's page with another model and a key
	err := e.SaveModel(ctx, "main", ui.ModelEdit{Endpoint: "https://provider/v1", Model: "pro", Privacy: "cloud", Token: "page-key"})
	// Then the save is accepted
	if err != nil {
		t.Fatal(err)
	}
	// And mira.yaml and the secrets file hold the page's values
	if out := read(t, path); !strings.Contains(out, "model: pro") {
		t.Errorf("file:\n%s", out)
	}
	all, _ := e.file.Secrets.Load()
	if all[setup.MainToken] != "page-key" {
		t.Errorf("secret %q", all[setup.MainToken])
	}
	// And after the restart the conversation takes the page's model
	raw, _ := os.ReadFile(path)
	cfg, err := config.Parse(raw, e.file.Secrets.Lookup(config.OSLookup))
	if err != nil {
		t.Fatal(err)
	}
	if m := cfg.Main(); m.Model != "pro" || m.APIKey != "page-key" {
		t.Errorf("main %+v", m)
	}
}

// oldSetup is a data folder of a service set up before the model went into
// mira.yaml: the setup's model in the database, and the given mira.yaml.
type oldSetup struct {
	dir, path, db string
	log           *bytes.Buffer
}

const oldModel = `{"kind":"cloud","base_url":"https://provider/v1","model":"flash","api_key":"sk-old"}`

func newOldSetup(t *testing.T, file string, llm string) oldSetup {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	o := oldSetup{dir: dir, path: filepath.Join(dir, "mira.yaml"), db: filepath.Join(dir, "mira.db"), log: &bytes.Buffer{}}
	if err := os.WriteFile(o.path, []byte(file+"store:\n  path: "+o.db+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, store.Options{Path: o.db, Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	if llm != "" {
		if err := st.SetKV(ctx, "setup.llm", llm); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	t.Setenv("MIRA_DATA", dir)
	t.Setenv("MIRA_SECRET_KEY", "")
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(o.log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return o
}

// start is Mira's start as far as the setup goes.
func (o oldSetup) start(t *testing.T) prepared {
	t.Helper()
	p, err := prepare(context.Background(), o.path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// kvLLM is what the database holds as the setup's model.
func (o oldSetup) kvLLM(t *testing.T) string {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: o.db, Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v, _ := st.GetKV(context.Background(), "setup.llm")
	return v
}

func (o oldSetup) main(t *testing.T) map[string]any {
	t.Helper()
	var top struct {
		Models map[string]map[string]any `yaml:"models"`
	}
	if err := yaml.Unmarshal([]byte(read(t, o.path)), &top); err != nil {
		t.Fatal(err)
	}
	return top.Models[config.MainModel]
}

// MF-10 An existing installation's model moves into mira.yaml
func TestMF10_AnExistingInstallationsModelMovesIntoMiraYaml(t *testing.T) {
	// Given kv with "setup.llm" for a provider's model with a key, and mira.yaml without models
	o := newOldSetup(t, "", oldModel)
	// When the service starts
	p := o.start(t)
	// Then mira.yaml has models.main with the setup's values
	if m := o.main(t); m["model"] != "flash" || m["endpoint"] != "https://provider/v1" || m["privacy"] != "cloud" {
		t.Errorf("main %v", m)
	}
	// And the secrets file holds the key under MIRA_MODEL_MAIN_TOKEN
	all, _ := config.DefaultSecrets().Load()
	if all["MIRA_MODEL_MAIN_TOKEN"] != "sk-old" {
		t.Errorf("secrets %v", all)
	}
	// And kv has no "setup.llm"
	if v := o.kvLLM(t); v != "" {
		t.Errorf("setup.llm = %q", v)
	}
	// And the log says the model moved into mira.yaml
	if !strings.Contains(o.log.String(), "the setup's model moved into mira.yaml") || strings.Contains(o.log.String(), "sk-old") {
		t.Errorf("log:\n%s", o.log)
	}
	// and Mira runs with it
	if m := p.cfg.Main(); m.Model != "flash" || m.APIKey != "sk-old" || p.mainMoveErr != nil || p.model() != "flash" {
		t.Errorf("main %+v, %v", m, p.mainMoveErr)
	}
}

// MF-11 Moving over a main of the file keeps the model in use
func TestMF11_MovingOverAMainOfTheFileKeepsTheModelInUse(t *testing.T) {
	// Given kv with "setup.llm" for model A, and mira.yaml whose models.main names model B with a fallback
	o := newOldSetup(t, `models:
  main:
    endpoint: https://provider/v1
    model: b
    privacy: cloud
    fallback: lite
  lite:
    endpoint: https://provider/v1
    model: small
    privacy: cloud
`, oldModel)
	// When the service starts
	p := o.start(t)
	// Then models.main names model A and keeps its fallback
	if m := o.main(t); m["model"] != "flash" || m["fallback"] != "lite" {
		t.Errorf("main %v", m)
	}
	if p.cfg.Main().Model != "flash" {
		t.Errorf("main in use %+v", p.cfg.Main())
	}
	// And the log names models.main.model as replaced, without the key
	if log := o.log.String(); !strings.Contains(log, "models.main.model") || strings.Contains(log, "sk-old") {
		t.Errorf("log:\n%s", log)
	}
}

// MF-12 Moving into a read-only file keeps Mira running
func TestMF12_MovingIntoAReadOnlyFileKeepsMiraRunning(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	// Given kv with "setup.llm", and a mira.yaml that is read only
	o := newOldSetup(t, "", oldModel)
	os.Chmod(o.path, 0o444)
	// When the service starts
	p := o.start(t)
	// Then the conversation takes the model of "setup.llm"
	if m := p.cfg.Main(); m.Model != "flash" || m.APIKey != "sk-old" {
		t.Errorf("main %+v", m)
	}
	// And kv still has "setup.llm"
	if o.kvLLM(t) == "" {
		t.Error("setup.llm is gone")
	}
	if p.mainMoveErr == nil || !strings.Contains(o.log.String(), "could not move into mira.yaml") {
		t.Fatalf("no trace: %v\n%s", p.mainMoveErr, o.log)
	}
	// And /v1/status has a problem with source "model_file" and name "main" that names the file
	a, e, _ := editApp(t)
	a.mainMoveErr = p.mainMoveErr
	var found bool
	for _, pr := range a.allProblems() {
		if pr.Source == "model_file" && pr.Name == "main" && strings.Contains(pr.Text, o.path) {
			found = true
		}
	}
	if !found {
		t.Errorf("problems %+v", a.allProblems())
	}
	// And Mira's page shows main read only with the cause
	ctx := context.Background()
	if err := a.store.SetKV(ctx, "setup.llm", oldModel); err != nil {
		t.Fatal(err)
	}
	if m := e.State(ctx).Models["main"]; !m.FromHA || !strings.Contains(m.MoveError, o.path) {
		t.Errorf("main on the page %+v", m)
	}
	err := e.SaveModel(ctx, "main", ui.ModelEdit{Endpoint: "https://provider/v1", Model: "pro", Privacy: "cloud"})
	if err == nil || !strings.Contains(err.Error(), o.path) {
		t.Errorf("saving main: %v", err)
	}
}

// MF-13 Once moved, a start changes nothing
func TestMF13_OnceMovedAStartChangesNothing(t *testing.T) {
	// Given a service whose model was moved into mira.yaml
	o := newOldSetup(t, "", oldModel)
	o.start(t)
	before := read(t, o.path)
	o.log.Reset()
	// When the service starts again
	o.start(t)
	// Then mira.yaml is unchanged
	if read(t, o.path) != before || strings.Contains(o.log.String(), "moved") {
		t.Errorf("changed:\n%s\nlog:\n%s", read(t, o.path), o.log)
	}
}

// MF-14 A reset keeps the model
func TestMF14_AResetKeepsTheModel(t *testing.T) {
	// Given a service whose main is in mira.yaml
	o := newOldSetup(t, "", oldModel)
	o.start(t)
	// When its data are reset and it starts
	if err := os.WriteFile(filepath.Join(o.dir, "options.json"), []byte(`{"reset_data": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := o.start(t)
	if !strings.Contains(o.log.String(), "deleted as asked") {
		t.Fatalf("no reset:\n%s", o.log)
	}
	// Then GET /v1/pair names the model (the integration's overview then shows it as "stays")
	if p.model() != "flash" || !p.open {
		t.Errorf("model %q, open %v", p.model(), p.open)
	}
}

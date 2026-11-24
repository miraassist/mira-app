package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/setup"
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

package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PC-17 A label for a proposal without its number is a problem of the skill
// (docs/spec/private-choices.md, decision 16).
func TestPC17_ALabelForAProposalWithoutItsNumberIsAProblemOfTheSkill(t *testing.T) {
	load := func(choose string) ([]Skill, []Problem) {
		dir := filepath.Join(t.TempDir(), "antwort")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		md := `---
name: antwort
description: Antwortet
metadata:
  mira:
    target:
      chosen: body
      private: true
      labels:
        de: {choose: "` + choose + `", other: Was anderes senden, dismiss: Ignorieren}
      fields:
        - {name: body, description: Text}
        - {name: proposals, description: Vorschläge, optional: true}
      finish:
        action: notify.notify
---
# Antwort
`
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load([]string{filepath.Dir(dir)}, map[string]map[string]any{"antwort": nil})
	}
	// Given a target whose labels name choose without {n}
	_, problems := load("Vorschlag senden")
	// Then the skill has a problem naming the label
	if len(problems) != 1 || !strings.Contains(problems[0].Text("de"), "labels.de.choose") {
		t.Fatalf("problems %+v", problems)
	}
	// With {n} it loads, its labels read.
	loaded, problems := load("Vorschlag {n} senden")
	if len(problems) != 0 || len(loaded) != 1 || loaded[0].Target.Labels["de"].Dismiss != "Ignorieren" {
		t.Errorf("loaded %+v problems %+v", loaded, problems)
	}
}

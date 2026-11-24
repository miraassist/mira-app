package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/miraassist/mira-app/internal/channels/haconv"
	"github.com/miraassist/mira-app/internal/i18n"
	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/ui"
)

// companionRoot is a companion's skills as Mira holds them in memory
// (docs/spec/companions.md, decision 22): name → SKILL.md.
func companionRoot(files map[string]string) skills.Root {
	fsys := fstest.MapFS{}
	for name, text := range files {
		fsys[name+"/SKILL.md"] = &fstest.MapFile{Data: []byte(text)}
	}
	return skills.Root{Name: "http://pro:8792", FS: fsys, Source: true}
}

func catalogByName(a *app) map[string]ui.SkillInfo {
	out := map[string]ui.SkillInfo{}
	for _, s := range a.catalog(context.Background()).Skills {
		out[s.Name] = s
	}
	return out
}

func TestBS20_ACompanionsSkillIsOnWithoutAnEntryAndNotOnTheTabSkills(t *testing.T) {
	a, _, _ := deliveryApp(t)
	dir := t.TempDir()
	writeSkill(t, dir, "frei", "---\nname: frei\ndescription: Ein freier Skill.\n---\nText.\n")
	roots := append(skills.Roots([]string{dir}), companionRoot(map[string]string{
		"fernwartung": "---\nname: fernwartung\ndescription: Fernwartung.\nmetadata:\n  mira:\n    tier: pro\n---\nText.\n",
		"notiz":       "---\nname: notiz\ndescription: Ein Skill des Begleiters ohne Stufe.\n---\nText.\n",
	}))
	// Given no entry under skills in mira.yaml
	var logs bytes.Buffer
	enabled := map[string]map[string]any{}
	addTiered(enabled, roots, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	loaded, problems := skills.LoadWith(roots, enabled, skills.Options{})
	// Then the skill with a tier is on, the others are not
	if len(loaded) != 1 || loaded[0].Name != "fernwartung" || loaded[0].Tier != "pro" || len(problems) != 0 {
		t.Fatalf("loaded %+v, problems %v", loaded, problems)
	}
	if logs.Len() != 0 {
		t.Errorf("log %s", &logs)
	}
	// And the tab "Skills" lists it neither as active nor as available
	a.cfg.Skills.Dirs, a.skillRoots, a.cfg.Skills.Enabled = []string{dir}, roots, map[string]map[string]any{}
	a.skills = &skills.Set{Skills: loaded}
	a.registerAgents(nil)
	by := catalogByName(a)
	if _, listed := by["fernwartung"]; listed {
		t.Errorf("listed: %+v", by["fernwartung"])
	}
	if by["frei"].State != "available" || by["notiz"].State != "available" {
		t.Errorf("the others: %+v", by)
	}
	// An entry's settings stay; enabled: false is not read for it, and the log says so.
	enabled = map[string]map[string]any{"fernwartung": {"x": "y"}}
	addTiered(enabled, roots, map[string]bool{"fernwartung": true}, slog.New(slog.NewTextHandler(&logs, nil)))
	if enabled["fernwartung"]["x"] != "y" || !strings.Contains(logs.String(), "enabled: false under skills is not read") ||
		!strings.Contains(logs.String(), "skill=fernwartung") {
		t.Errorf("enabled %v, log %s", enabled, &logs)
	}
}

func TestBS21_ACompanionsSkillThatCannotLoadIsStillARepair(t *testing.T) {
	a, _, _ := deliveryApp(t)
	roots := []skills.Root{companionRoot(map[string]string{
		"fernwartung": "---\nname: fernwartung\ndescription: Fernwartung.\nmetadata:\n  mira:\n    tier: pro\n" +
			"    settings:\n      zugang: { description: Der Zugang, required: true }\n---\nText.\n",
	})}
	enabled := map[string]map[string]any{}
	addTiered(enabled, roots, nil, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	loaded, problems := skills.LoadWith(roots, enabled, skills.Options{})
	// Its problem goes out as every skill's (the integration shows it as a repair)
	if len(loaded) != 0 || len(problems) != 1 || problems[0].Skill != "fernwartung" || !strings.Contains(problems[0].Text(i18n.EN), "zugang") {
		t.Fatalf("loaded %+v, problems %v", loaded, problems)
	}
	// And the tab "Skills" does not list it
	a.skillRoots, a.cfg.Skills.Enabled = roots, map[string]map[string]any{"fernwartung": nil}
	a.skills = &skills.Set{}
	a.problems = []haconv.Problem{{Source: "skill", Name: "fernwartung", Text: problems[0].Text(i18n.EN)}}
	a.registerAgents(nil)
	if s, listed := catalogByName(a)["fernwartung"]; listed {
		t.Errorf("listed: %+v", s)
	}
}

func TestBS25_ASkillsCategoryReachesTheTabSkills(t *testing.T) {
	a, _, _ := deliveryApp(t)
	dir := t.TempDir()
	writeSkill(t, dir, "wetter", "---\nname: wetter\ndescription: Das Wetter.\nmetadata:\n  mira:\n    category: Haus & Garten\n---\nText.\n")
	writeSkill(t, dir, "kochen", "---\nname: kochen\ndescription: Kochen.\nmetadata:\n  mira:\n    category: Küche\n---\nText.\n")
	writeSkill(t, dir, "frei", "---\nname: frei\ndescription: Ohne Gruppe.\n---\nText.\n")
	roots := skills.Roots([]string{dir})
	enabled := map[string]map[string]any{"wetter": nil}
	loaded, problems := skills.LoadWith(roots, enabled, skills.Options{})
	if len(loaded) != 1 || loaded[0].Category != "Haus & Garten" || len(problems) != 0 {
		t.Fatalf("loaded %+v, problems %v", loaded, problems)
	}
	a.cfg.Skills.Dirs, a.skillRoots, a.cfg.Skills.Enabled = []string{dir}, roots, enabled
	a.skills = &skills.Set{Skills: loaded}
	a.registerAgents(nil)
	by := catalogByName(a)
	if by["wetter"].State != "active" || by["wetter"].Category != "Haus & Garten" || by["kochen"].State != "available" ||
		by["kochen"].Category != "Küche" || by["frei"].Category != "" {
		t.Errorf("catalog %+v", by)
	}
}

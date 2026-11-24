package main

import (
	"log/slog"

	"github.com/miraassist/mira-app/internal/skills"
)

// addTiered switches on every skill of roots that names a tier: a
// companion's skill is on whenever its companion serves it, without an
// entry under skills in mira.yaml (docs/spec/companions.md, decision 28). An
// entry's settings stay; an entry enabled: false is not read for it, and
// the log says so.
func addTiered(enabled map[string]map[string]any, roots []skills.Root, off map[string]bool, log *slog.Logger) {
	for _, name := range skills.Tiered(roots) {
		if off[name] {
			log.Warn("a companion's skill is on whenever its companion serves it; enabled: false under skills is not read for it",
				"skill", name)
		}
		if _, ok := enabled[name]; !ok {
			enabled[name] = nil
		}
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/miraassist/mira-app/internal/ha/updates"
)

// VE-13 No minimum is above the other side's version
// (docs/spec/versions.md, Minimum versions instead of one version, step 5):
// the service's MinIntegration is not above the integration's version, and
// the integration's MIN_SERVICE is not above this service's. The
// integration is in its own repository (miraassist/mira): MIRA_INTEGRATION
// names its checkout (scripts/check.sh sets it); without one the test is
// skipped.
func TestVE13_NoMinimumIsAboveTheOtherSidesVersion(t *testing.T) {
	dir := os.Getenv("MIRA_INTEGRATION")
	if dir == "" {
		t.Skip("MIRA_INTEGRATION names no checkout of the integration")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "custom_components", "mira", "manifest.json"))
	if err != nil {
		t.Fatalf("MIRA_INTEGRATION %s: %v", dir, err)
	}
	var m struct{ Version string }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	code, err := os.ReadFile(filepath.Join(dir, "custom_components", "mira", "const.py"))
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`(?m)^MIN_SERVICE = "([^"]+)"`).FindSubmatch(code)
	if found == nil {
		t.Fatalf("const.py in %s has no MIN_SERVICE", dir)
	}
	minService := string(found[1])
	if updates.Older(m.Version, updates.MinIntegration) {
		t.Errorf("the service's MinIntegration %s is above the integration's version %s", updates.MinIntegration, m.Version)
	}
	if updates.Older(version, minService) {
		t.Errorf("the integration's MIN_SERVICE %s is above this service's version %s", minService, version)
	}
}

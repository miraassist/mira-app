package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The service's own version is the integration's: a build without the
// release's -X main.version reports it, as the eval reports do. 0.16.0 went
// out with it still 0.15.2. The integration is in its own repository
// (miraassist/mira): MIRA_INTEGRATION names its checkout (scripts/check.sh
// sets it); without one the test is skipped.
func TestTheVersionIsTheIntegrations(t *testing.T) {
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
		t.Fatal(err)
	}
	if version != m.Version {
		t.Errorf("cmd/mira says %s, manifest.json %s", version, m.Version)
	}
}

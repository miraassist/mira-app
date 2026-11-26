package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/api"
	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/setup"
	"github.com/miraassist/mira-app/internal/store"
)

// limitedStart is a limited start as runLimited builds it, at every start
// anew: mira.yaml refers to a token that is set nowhere (issue 10: the
// secrets file stayed with another installation), its store in a folder of
// the test. before runs on the store first (a pairing, a setup), as an
// earlier run of the service would have.
type limitedStart struct {
	file, dir string
	cause     error
}

const unsetToken = "MIRA_MODEL_SU_TEST_TOKEN"

func newLimitedStart(t *testing.T) limitedStart {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MIRA_DATA", dir)
	t.Setenv(unsetToken, "")
	os.Unsetenv(unsetToken)
	file := filepath.Join(dir, "mira.yaml")
	body := "store:\n  path: " + filepath.Join(dir, "mira.db") + "\nmodels:\n  main:\n    endpoint: http://127.0.0.1:1/v1\n" +
		"    model: m\n    token: ${" + unsetToken + "}\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, cause := config.LoadOptional(file)
	if cause == nil || !strings.Contains(cause.Error(), unsetToken) {
		t.Fatalf("the file reads, or for another reason: %v", cause)
	}
	return limitedStart{file: file, dir: dir, cause: cause}
}

// store opens the start's store for what an earlier run did.
func (l limitedStart) store(t *testing.T, do func(*store.Store)) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(l.dir, "mira.db"), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	do(st)
	st.Close()
}

// serve starts the limited start's server as runLimited does.
func (l limitedStart) serve(t *testing.T) (*httptest.Server, prepared) {
	t.Helper()
	ctx := context.Background()
	p, err := prepareFrom(ctx, l.file, config.Limited(l.file), false, false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, store.Options{Path: p.cfg.Store.Path, Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	id, err := st.InstanceID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(limitedMux(st, p, id, config.ConfigPath(l.file), l.cause))
	t.Cleanup(srv.Close)
	return srv, p
}

func call(t *testing.T, srv *httptest.Server, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func healthStatus(t *testing.T, srv *httptest.Server) (string, string) {
	t.Helper()
	code, body := call(t, srv, "/v1/health", "")
	var h struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &h); err != nil || code != http.StatusOK {
		t.Fatalf("health %d %q: %v", code, body, err)
	}
	return h.Status, body
}

// SU-01 A limited start that is set up says so in its health
// (docs/spec/setup-recovery.md, decision 1), without the error.
func TestSU01_ALimitedStartThatIsSetUpSaysSoInItsHealth(t *testing.T) {
	l := newLimitedStart(t)
	l.store(t, func(st *store.Store) {
		if err := setup.Save(context.Background(), st, "http://ha.test:8123", "ha-token"); err != nil {
			t.Fatal(err)
		}
	})
	srv, _ := l.serve(t)
	status, body := healthStatus(t, srv)
	if status != "limited" {
		t.Errorf("status %q, want limited: %s", status, body)
	}
	for _, leak := range []string{unsetToken, "mira.yaml", "environment"} {
		if strings.Contains(body, leak) {
			t.Errorf("the health, open without a token, carries %q: %s", leak, body)
		}
	}
}

// SU-02 A limited start that is not set up waits for its setup.
func TestSU02_ALimitedStartThatIsNotSetUpWaitsForItsSetup(t *testing.T) {
	l := newLimitedStart(t)
	srv, _ := l.serve(t)
	if status, body := healthStatus(t, srv); status != "setup" {
		t.Errorf("status %q, want setup: %s", status, body)
	}
}

// SU-03 A pairing in the limited start ends in the limited status: after the
// restart its health says limited, and its status, with the token the
// pairing gave, carries the problem.
func TestSU03_APairingInTheLimitedStartEndsInTheLimitedStatus(t *testing.T) {
	l := newLimitedStart(t)
	srv, _ := l.serve(t)
	if status, _ := healthStatus(t, srv); status != "setup" {
		t.Fatalf("before the pairing: %q", status)
	}
	srv.Close()
	var token string
	l.store(t, func(st *store.Store) {
		var err error
		if token, err = setup.Pair(context.Background(), st, "", "", "http://ha.test:8123", "ha-token", nil); err != nil {
			t.Fatal(err)
		}
	})
	again, _ := l.serve(t)
	if status, body := healthStatus(t, again); status != "limited" {
		t.Errorf("after the pairing: status %q, want limited: %s", status, body)
	}
	code, body := call(t, again, "/v1/status", token)
	if code != http.StatusOK || !strings.Contains(body, unsetToken) || !strings.Contains(body, `"source":"config"`) {
		t.Errorf("status with the pairing's token: %d %s", code, body)
	}
	if code, _ := call(t, again, "/v1/status", ""); code != http.StatusUnauthorized {
		t.Errorf("status without the token: %d", code)
	}
}

// SU-04 The limited start's page says the settings cannot be read, never the
// error; the setup's steps only while the setup is missing.
func TestSU04_TheLimitedStartsPageSaysTheSettingsCannotBeRead(t *testing.T) {
	l := newLimitedStart(t)
	srv, _ := l.serve(t)
	_, page := call(t, srv, "/", "")
	check := func(page string, steps bool) {
		t.Helper()
		if !strings.Contains(page, "Mira kann ihre Einstellungen nicht lesen") || !strings.Contains(page, "Reparaturen") {
			t.Errorf("the page does not say the settings cannot be read: %s", page)
		}
		if strings.Contains(page, unsetToken) || strings.Contains(page, l.file) || strings.Contains(page, "wartet auf die Einrichtung") {
			t.Errorf("the page shows the error or says it waits: %s", page)
		}
		if got := strings.Contains(page, "Entdeckt"); got != steps {
			t.Errorf("the setup's steps shown %v, want %v", got, steps)
		}
	}
	check(page, true)
	srv.Close()
	l.store(t, func(st *store.Store) {
		if err := setup.Save(context.Background(), st, "http://ha.test:8123", "ha-token"); err != nil {
			t.Fatal(err)
		}
	})
	again, _ := l.serve(t)
	_, page = call(t, again, "/", "")
	check(page, false)
}

// SU-10 The limited start's pairing state says limited, without an error
// text (docs/spec/setup-recovery.md, decision 6); a service in setup mode
// with a readable file does not.
func TestSU10_TheLimitedStartsPairingStateSaysLimited(t *testing.T) {
	l := newLimitedStart(t)
	srv, _ := l.serve(t)
	code, body := call(t, srv, "/v1/pair", "")
	var state struct {
		Limited bool `json:"limited"`
	}
	if err := json.Unmarshal([]byte(body), &state); err != nil || code != http.StatusOK || !state.Limited {
		t.Errorf("pairing state %d %s: %v", code, body, err)
	}
	for _, leak := range []string{unsetToken, "mira.yaml", "environment"} {
		if strings.Contains(body, leak) {
			t.Errorf("the pairing state, open without a token, carries %q: %s", leak, body)
		}
	}
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "plain.db"), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	plain := httptest.NewServer(func() http.Handler {
		mux := api.NewMux()
		(&setup.Handler{Store: st, SetupMode: true, Pairing: true, ID: "id-2"}).Register(mux)
		return mux
	}())
	defer plain.Close()
	if _, body := call(t, plain, "/v1/pair", ""); strings.Contains(body, "limited") {
		t.Errorf("setup mode with a readable file says limited: %s", body)
	}
}

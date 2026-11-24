package companions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miraassist/mira-app/internal/access"
	"github.com/miraassist/mira-app/internal/api"
	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/llm"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/store/storetest"
)

const integration = "the-integrations-token"

type fixture struct {
	t         *testing.T
	h         *Handler
	k         *access.Keeper
	srv       http.Handler
	logs      *bytes.Buffer
	restarted chan struct{}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, logs: &bytes.Buffer{}, restarted: make(chan struct{}, 1)}
	log := slog.New(slog.NewTextHandler(f.logs, nil))
	f.k, err = access.New(ctx, st, integration, log)
	if err != nil {
		t.Fatal(err)
	}
	f.h = &Handler{Store: st, Log: log, Restart: func() { f.restarted <- struct{}{} }}
	mux := api.NewMux()
	f.h.Register(mux)
	// The page's routes as internal/ui mounts them (its admin check is the page's own).
	mux.Hidden("GET /ui/companion/{name}/{path...}", "test", http.HandlerFunc(f.h.PassPage))
	mux.Hidden("GET /v1/ui/companion/{name}/{path...}", "test", http.HandlerFunc(f.h.PassData))
	mux.Hidden("GET /v1/ui/companions", "test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list, _ := f.h.List(r.Context())
		writeJSON(w, http.StatusOK, list)
	}))
	f.srv = f.k.Check(mux)
	return f
}

func (f *fixture) partner(name string) string {
	f.t.Helper()
	tok, _, err := f.k.Make(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

func (f *fixture) call(method, path, bearer string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func TestBS01_ACompanionRegistersWithItsPartnerToken(t *testing.T) {
	f := newFixture(t)
	tok := f.partner("Pro")
	rec := f.call("PUT", "/v1/companions/pro", tok, Registration{Title: "Pro", URL: "http://mira-pro:8792", Token: "companion-secret"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("answer %d %s", rec.Code, rec.Body)
	}
	list, err := f.h.List(context.Background())
	if err != nil || len(list) != 1 || list[0] != (Companion{Name: "pro", Title: "Pro"}) {
		t.Errorf("list %+v %v", list, err)
	}
	if !strings.Contains(f.logs.String(), "companion registered") || !strings.Contains(f.logs.String(), "http://mira-pro:8792") ||
		strings.Contains(f.logs.String(), "companion-secret") {
		t.Errorf("log %s", f.logs)
	}
}

func TestBS02_AWrongRegistrationIsRefusedPerField(t *testing.T) {
	f := newFixture(t)
	rec := f.call("PUT", "/v1/companions/Pro!", f.partner("Pro"), Registration{Title: "Pro", Token: "x"})
	var e Errors
	json.NewDecoder(rec.Body).Decode(&e)
	if rec.Code != http.StatusBadRequest || e.Errors["name"] == "" || e.Errors["url"] == "" {
		t.Errorf("answer %d %+v", rec.Code, e)
	}
}

func TestBS03_AnotherTokenMayNotTakeAName(t *testing.T) {
	f := newFixture(t)
	reg := Registration{Title: "Pro", URL: "http://a:1", Token: "x"}
	if rec := f.call("PUT", "/v1/companions/pro", f.partner("One"), reg); rec.Code != http.StatusNoContent {
		t.Fatalf("first %d", rec.Code)
	}
	reg.URL = "http://b:2"
	if rec := f.call("PUT", "/v1/companions/pro", f.partner("Two"), reg); rec.Code != http.StatusConflict {
		t.Errorf("second %d %s", rec.Code, rec.Body)
	}
	all, _ := f.h.Store.Companions(context.Background())
	if len(all) != 1 || all[0].URL != "http://a:1" {
		t.Errorf("registration changed %+v", all)
	}
}

func TestBS04_APartnerTokenStillReachesNothingElse(t *testing.T) {
	f := newFixture(t)
	tok := f.partner("Pro")
	for _, c := range [][2]string{{"GET", "/v1/ui/companions"}, {"POST", "/v1/conversation"}, {"GET", "/v1/ui/companion/pro/state"}} {
		if rec := f.call(c[0], c[1], tok, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", c[0], c[1], rec.Code)
		}
	}
}

func TestBS05_TheCompanionsPageAndDataArePassedOn(t *testing.T) {
	f := newFixture(t)
	var gotAuth string
	comp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ui/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<title>Pro</title>"))
		case "/api/state":
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tier":"pro"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(comp.Close)
	f.call("PUT", "/v1/companions/pro", f.partner("Pro"), Registration{Title: "Pro", URL: comp.URL, Token: "companion-secret"})
	if rec := f.call("GET", "/ui/companion/pro/", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Pro</title>") {
		t.Errorf("page %d %s", rec.Code, rec.Body)
	}
	rec := f.call("GET", "/v1/ui/companion/pro/state", integration, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"tier":"pro"`) {
		t.Errorf("data %d %s", rec.Code, rec.Body)
	}
	if gotAuth != "Bearer companion-secret" {
		t.Errorf("the companion got %q, not its own token", gotAuth)
	}
	if rec := f.call("GET", "/ui/companion/other/", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown companion: %d", rec.Code)
	}
}

func TestBS06_AnUnreachableCompanionIsSaid(t *testing.T) {
	f := newFixture(t)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	f.call("PUT", "/v1/companions/pro", f.partner("Pro"), Registration{Title: "Pro", URL: gone.URL, Token: "x"})
	rec := f.call("GET", "/v1/ui/companion/pro/state", integration, nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "companion pro cannot be reached") {
		t.Errorf("answer %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(f.logs.String(), "a companion cannot be reached") {
		t.Errorf("no log line")
	}
}

func TestBS08_AReloadRestartsMira(t *testing.T) {
	f := newFixture(t)
	if rec := f.call("POST", "/v1/skills/reload", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d", rec.Code)
	}
	if rec := f.call("POST", "/v1/skills/reload", f.partner("Pro"), nil); rec.Code != http.StatusAccepted {
		t.Fatalf("answer %d %s", rec.Code, rec.Body)
	}
	<-f.restarted
}

// fakeModel answers a completion and keeps what it was asked.
type fakeModel struct {
	reply string
	err   error
	got   []llm.ChatRequest
}

func (m *fakeModel) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.got = append(m.got, req)
	if m.err != nil {
		return nil, m.err
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: m.reply}}},
		Usage: llm.Usage{PromptTokens: 42, CompletionTokens: 3}}, nil
}

func (f *fixture) models(main, local *fakeModel) {
	f.h.Models = func() map[string]Model {
		return map[string]Model{
			config.MainModel: {Settings: config.LLM{Privacy: config.PrivacyCloud}, Client: main},
			"lokal":          {Settings: config.LLM{Privacy: config.PrivacyLocal}, Client: local},
		}
	}
}

// BS-09 A companion's prompt runs on the model it names
func TestBS09_ACompanionsPromptRunsOnTheModelItNames(t *testing.T) {
	f := newFixture(t)
	main, local := &fakeModel{reply: "sicher"}, &fakeModel{reply: "safe"}
	f.models(main, local)
	tok := f.partner("Pro")
	// When a partner token calls POST /v1/complete with a model, a system prompt and a text
	rec := f.call("POST", "/v1/complete", tok, Completion{Model: "lokal", System: "Prüfe.", Text: "Eine Mail."})
	var out Completed
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	// Then the answer has the model's text, its name, its privacy and the tokens
	if rec.Code != http.StatusOK || out != (Completed{Text: "safe", Model: "lokal", Privacy: config.PrivacyLocal,
		InputTokens: 42, OutputTokens: 3}) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(local.got) != 1 || local.got[0].Messages[0].Content != "Prüfe." || local.got[0].Messages[1].Content != "Eine Mail." {
		t.Errorf("the model got %+v", local.got)
	}
	// And without a model the main model answers
	rec = f.call("POST", "/v1/complete", tok, Completion{Text: "Eine Mail."})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"model":"main"`) || len(main.got) != 1 {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	// The integration's token may call it too.
	if rec := f.call("POST", "/v1/complete", integration, Completion{Text: "x"}); rec.Code != http.StatusOK {
		t.Errorf("the integration's token: %d %s", rec.Code, rec.Body)
	}
}

// BS-10 The call needs a token, also from the home network
func TestBS10_TheCallNeedsATokenAlsoFromTheHomeNetwork(t *testing.T) {
	f := newFixture(t)
	main := &fakeModel{reply: "x"}
	f.models(main, &fakeModel{})
	for _, bearer := range []string{"", "a-wrong-token"} {
		rec := f.call("POST", "/v1/complete", bearer, Completion{Text: "x"})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("bearer %q: %d %s", bearer, rec.Code, rec.Body)
		}
	}
	if len(main.got) != 0 {
		t.Errorf("the model was called %d times", len(main.got))
	}
}

// BS-11 local_only refuses a cloud model
func TestBS11_LocalOnlyRefusesACloudModel(t *testing.T) {
	f := newFixture(t)
	main, local := &fakeModel{reply: "x"}, &fakeModel{reply: "y"}
	f.models(main, local)
	tok := f.partner("Pro")
	rec := f.call("POST", "/v1/complete", tok, Completion{Text: "x", LocalOnly: true})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "main is a cloud model") || len(main.got) != 0 {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if rec := f.call("POST", "/v1/complete", tok, Completion{Model: "lokal", Text: "x", LocalOnly: true}); rec.Code != http.StatusOK {
		t.Errorf("a local model: %d %s", rec.Code, rec.Body)
	}
}

// BS-12 What does not fit is refused and never logged
func TestBS12_WhatDoesNotFitIsRefusedAndNeverLogged(t *testing.T) {
	f := newFixture(t)
	main := &fakeModel{err: errors.New("503 overloaded")}
	f.models(main, &fakeModel{})
	tok := f.partner("Pro")
	secret := "Geheimer Text der Mail"
	if rec := f.call("POST", "/v1/complete", tok, Completion{Text: strings.Repeat("x", maxComplete+1)}); rec.Code != http.StatusBadRequest {
		t.Errorf("too long: %d", rec.Code)
	}
	if rec := f.call("POST", "/v1/complete", tok, Completion{Model: "weg", Text: secret}); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "there are: lokal, main") {
		t.Errorf("unknown model: %d %s", rec.Code, rec.Body)
	}
	if rec := f.call("POST", "/v1/complete", tok, Completion{Text: secret, System: "System geheim"}); rec.Code != http.StatusBadGateway ||
		!strings.Contains(rec.Body.String(), "503 overloaded") {
		t.Errorf("a failing model: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(f.logs.String(), secret) || strings.Contains(f.logs.String(), "System geheim") {
		t.Errorf("a text in the log:\n%s", f.logs)
	}
	if !strings.Contains(f.logs.String(), "companion model call failed") {
		t.Errorf("no log line for the failure:\n%s", f.logs)
	}
}

// BS-13, the registration's part: once kept, it wakes the servers under the
// companion's address, without the answer waiting; a refused one does not.
func TestBS13_ARegistrationWakesTheCompanionsServers(t *testing.T) {
	f := newFixture(t)
	woken := make(chan string, 2)
	f.h.Wake = func(_ context.Context, url string) { woken <- url }
	tok := f.partner("Pro")
	if rec := f.call("PUT", "/v1/companions/pro", tok, Registration{Title: "Pro", URL: "http://mira-pro:8792/", Token: "s"}); rec.Code != http.StatusNoContent {
		t.Fatalf("answer %d %s", rec.Code, rec.Body)
	}
	if got := <-woken; got != "http://mira-pro:8792" {
		t.Errorf("woken %q", got)
	}
	if rec := f.call("PUT", "/v1/companions/pro", tok, Registration{Title: "", URL: "http://mira-pro:8792", Token: "s"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("answer %d", rec.Code)
	}
	select {
	case got := <-woken:
		t.Errorf("a refused registration woke %q", got)
	default:
	}
}

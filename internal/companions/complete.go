package companions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira-app/internal/access"
	"github.com/miraassist/mira-app/internal/api"
	"github.com/miraassist/mira-app/internal/clock"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/llm"
)

// A companion asks one of Mira's models (docs/spec/companions.md, decisions
// 13 to 20): one completion, the key never leaving Mira.

// maxComplete bounds the system prompt and the text together (decision 19).
const maxComplete = 64 << 10

// completeTimeout bounds the call (decision 19); the model's own timeout
// applies when shorter.
const completeTimeout = 60 * time.Second

// Chatter is the part of a model's client a completion uses.
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// Model is one of Mira's models as a companion's call reaches it.
type Model struct {
	Settings config.LLM
	Client   Chatter
}

// Completion is POST /v1/complete's body.
type Completion struct {
	// Model names a model of mira.yaml; empty is the main model.
	Model  string `json:"model,omitempty"`
	System string `json:"system,omitempty"`
	Text   string `json:"text"`
	// LocalOnly refuses a cloud model (decision 18).
	LocalOnly bool `json:"local_only,omitempty"`
}

// Completed is its answer.
type Completed struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	Privacy      string `json:"privacy"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

func (h *Handler) registerComplete(mux *api.Mux) {
	mux.Handle(api.Route{Method: http.MethodPost, Path: "/v1/complete", Tag: api.TagAccess,
		Summary:  "Run a prompt on one of Mira's models, for a companion service (docs/spec/companions.md, decisions 13 to 20)",
		Request:  Completion{},
		Response: Completed{}, Errors: []int{400, 401, 502, 503}}, h.complete)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	// Always a token, also from the home network (decision 15): a partner
	// token or the integration's, which the access check marks; anyone else
	// is no caller here.
	caller, ok := access.CallerFrom(r.Context())
	if !ok || (caller.Kind != access.KindPartner && caller.Kind != access.KindIntegration) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "a partner token or the integration's is needed"})
		return
	}
	var in Completion
	body := http.MaxBytesReader(w, r.Body, maxComplete+4<<10)
	if err := json.NewDecoder(body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a completion: " + err.Error()})
		return
	}
	switch {
	case strings.TrimSpace(in.Text) == "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is empty"})
		return
	case len(in.Text)+len(in.System) > maxComplete:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("system and text are %d bytes, at most %d", len(in.Text)+len(in.System), maxComplete)})
		return
	}
	models := map[string]Model{}
	if h.Models != nil {
		models = h.Models()
	}
	name := in.Model
	if name == "" {
		name = config.MainModel
	}
	m, ok := models[name]
	if !ok || m.Client == nil {
		names := make([]string, 0, len(models))
		for n := range models {
			names = append(names, n)
		}
		slices.Sort(names)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("no model %q; there are: %s", name, strings.Join(names, ", "))})
		return
	}
	if in.LocalOnly && m.Settings.Privacy != config.PrivacyLocal {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": name + " is a cloud model"})
		return
	}
	msgs := []llm.Message{{Role: "user", Content: in.Text}}
	if strings.TrimSpace(in.System) != "" {
		msgs = append([]llm.Message{{Role: "system", Content: in.System}}, msgs...)
	}
	ctx, cancel := clock.WithTimeout(r.Context(), h.Clock, completeTimeout)
	defer cancel()
	clk := clock.Or(h.Clock)
	began := clk.Now()
	resp, err := m.Client.Chat(ctx, llm.ChatRequest{Messages: msgs, Temperature: m.Settings.Temperature,
		MaxTokens: m.Settings.MaxTokens, ReasoningEffort: m.Settings.ReasoningEffort})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("no answer within %s: %w", completeTimeout, err)
		}
		h.log().Warn("companion model call failed", "token", caller.Name, "model", name,
			"ms", clk.Since(began).Milliseconds(), "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the model " + name + " failed: " + err.Error()})
		return
	}
	out := Completed{Text: strings.TrimSpace(resp.Message().Content), Model: name, Privacy: m.Settings.Privacy,
		InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens}
	// Never the texts (decision 20).
	h.log().Info("companion model call", "token", caller.Name, "model", name, "input_tokens", out.InputTokens,
		"output_tokens", out.OutputTokens, "ms", clk.Since(began).Milliseconds())
	writeJSON(w, http.StatusOK, out)
}

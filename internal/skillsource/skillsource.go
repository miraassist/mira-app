// Package skillsource loads the skills of a subscription from a skill source
// at Mira's start (docs/spec/skill-source.md, B): one request, its retries
// and errors, and the skills as one more skill directory that lives in
// memory. The interface is the server's (mira-skills: REQUIREMENTS.md,
// Interface to Mira). Nothing of it is written to disk (decision 8).
package skillsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing/fstest"
	"time"

	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/excerpt"
	"github.com/miraassist/mira-app/internal/retry"
	"github.com/miraassist/mira-app/internal/skills"
)

// Policy is the spec's retry (Errors): three tries, waiting 2 s, then 4 s; a
// Retry-After is kept up to 30 s, a longer one ends at once.
var Policy = retry.Policy{Attempts: 3, First: 2 * time.Second, Max: 30 * time.Second, Growth: retry.Exponential}

// Timeout bounds one try.
const Timeout = 10 * time.Second

// maxAnswer bounds the answer read: a subscription's skills are tens of
// kilobytes.
const maxAnswer = 16 << 20

type Options struct {
	Source  config.SkillSource
	Version string
	Client  *http.Client
	Logger  *slog.Logger
	// Policy replaces the package's (tests: no waits).
	Policy *retry.Policy
}

// Load asks the source once (with its retries) and gives its skills as a
// root after the skill directories; nil when there are none to load. The
// problems are the source's (a repair each) and those of single skills of
// the answer; each is logged here.
func Load(ctx context.Context, o Options) (*skills.Root, []skills.Problem) {
	log := logger(o)
	addr := strings.TrimRight(o.Source.URL, "/")
	if !allowed(addr) {
		log.Error("the skill source's address is not https and not on this machine; it is not asked", "url", addr)
		return nil, []skills.Problem{skills.NewProblem(host(addr), skills.KindSourceURL, addr)}
	}
	return load(ctx, o, log, addr, o.Source.Key, nil)
}

// Companion is a registered companion as a skill source of its own
// (docs/spec/companions.md, decisions 22 to 25): its address and the token
// it registered with. The skill source's rule on https (decision 14 there)
// does not hold: a companion's address is the one it registered, often in
// the house's own network.
type Companion struct {
	Name, Title, URL, Token string
}

// LoadCompanions asks every companion side by side (decision 25) and gives
// their skills as roots in the companions' order; a companion without
// skills, or one that cannot be asked, gives none. Each problem is logged.
func LoadCompanions(ctx context.Context, cs []Companion, o Options) ([]skills.Root, []skills.Problem) {
	log := logger(o)
	roots := make([]*skills.Root, len(cs))
	problems := make([][]skills.Problem, len(cs))
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			roots[i], problems[i] = load(ctx, o, log.With("companion", c.Name), strings.TrimRight(c.URL, "/"), c.Token, &c)
		}()
	}
	wg.Wait()
	var out []skills.Root
	var all []skills.Problem
	for i := range cs {
		if roots[i] != nil {
			out = append(out, *roots[i])
		}
		all = append(all, problems[i]...)
	}
	return out, all
}

func logger(o Options) *slog.Logger {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	return log.With("component", "skillsource")
}

// load asks one source, the subscription's or a companion's (c not nil).
func load(ctx context.Context, o Options, log *slog.Logger, addr, key string, c *Companion) (*skills.Root, []skills.Problem) {
	name := host(addr)
	what := "the skills of the subscription"
	kindDown, kindKey, kindNot := skills.KindSourceDown, skills.KindSourceKey, skills.KindSourceNot
	whole := func(kind string, args ...any) []skills.Problem {
		return []skills.Problem{skills.NewProblem(name, kind, append([]any{addr}, args...)...)}
	}
	if c != nil {
		what = "the skills of the companion " + c.Name
		kindDown, kindKey, kindNot = skills.KindCompanionDown, skills.KindCompanionKey, skills.KindCompanionNot
		whole = func(kind string, args ...any) []skills.Problem {
			return []skills.Problem{skills.NewProblem(c.Title, kind, append([]any{c.Title, addr}, args...)...)}
		}
	}
	p := Policy
	if o.Policy != nil {
		p = *o.Policy
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	var raw []byte
	err := retry.Do(ctx, p, func(ctx context.Context) error {
		var err error
		raw, err = fetch(ctx, client, addr, key, o.Version)
		return err
	}, retry.Warn(log, "loading "+what, p, "url", addr))
	if err != nil {
		var st *statusError
		switch {
		case c != nil && errors.As(err, &st) && st.code == http.StatusNotFound:
			// A companion that brings no skills, or one older than this (decision 24).
			log.Info("the companion brings no skills", "url", addr)
			return nil, nil
		case errors.As(err, &st) && (st.code == http.StatusUnauthorized || st.code == http.StatusForbidden):
			log.Error("the skill source refused the key; mira starts without its skills", "what", what, "url", addr, "status", st.code)
			return nil, whole(kindKey, st.code)
		case errors.As(err, &st) && !retry.Retryable(st):
			log.Error("the address is not a skill source; mira starts without its skills", "what", what, "url", addr, "status", st.code, "answer", st.answer)
			return nil, whole(kindNot, st.code)
		}
		log.Error("could not load "+what+"; mira starts without them and tries again at the next start", "url", addr, "error", err)
		return nil, whole(kindDown, err)
	}
	// Each skill on its own: one whose entry cannot be read is left out with
	// its problem, the others load (docs/spec/parsing.md, PA-04). Its version
	// is only logged, so it is taken as it comes.
	var answer struct {
		Skills []json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		log.Error("the skill source's answer is not the expected JSON; mira starts without its skills", "what", what, "url", addr, "error", err, "answer", excerpt.Bytes(raw))
		return nil, whole(kindNot, "the answer is not the expected JSON")
	}
	mem := fstest.MapFS{}
	var problems []skills.Problem
	loaded := 0
	for _, row := range answer.Skills {
		var sk struct {
			Name    string            `json:"name"`
			Version any               `json:"version"`
			Files   map[string]string `json:"files"`
		}
		if err := json.Unmarshal(row, &sk); err != nil {
			var named struct {
				Name any `json:"name"`
			}
			_ = json.Unmarshal(row, &named) // only to name it; its own error is logged below
			skill, _ := named.Name.(string)
			reason := fmt.Sprintf("its entry cannot be read: %v", err)
			log.Warn("a skill of the source is left out", "url", addr, "skill", skill, "reason", reason, "entry", excerpt.Bytes(row))
			problems = append(problems, skills.NewProblem(orName(skill, name), skills.KindSourceSkill, addr, skill, reason))
			continue
		}
		if reason := check(sk.Name, sk.Files); reason != "" {
			log.Warn("a skill of the source is left out", "url", addr, "skill", sk.Name, "reason", reason)
			problems = append(problems, skills.NewProblem(orName(sk.Name, name), skills.KindSourceSkill, addr, sk.Name, reason))
			continue
		}
		for p, text := range sk.Files {
			mem[path.Join(sk.Name, p)] = &fstest.MapFile{Data: []byte(text), Mode: 0o444}
		}
		loaded++
		log.Info("skill loaded from the source", "url", addr, "skill", sk.Name, "version", sk.Version, "files", len(sk.Files))
	}
	log.Info(what+" loaded", "url", addr, "skills", loaded)
	return &skills.Root{Name: addr, FS: mem, Source: true}, problems
}

// check is why a skill of the answer is not taken; "" when it is.
func check(name string, files map[string]string) string {
	switch {
	case name == "" || !fs.ValidPath(name) || strings.Contains(name, "/"):
		return fmt.Sprintf("its name %q is not a folder's name", name)
	case files["SKILL.md"] == "":
		return "it has no SKILL.md"
	}
	for p := range files {
		if !fs.ValidPath(p) || p == "." {
			return fmt.Sprintf("the path %q leaves the skill's folder", p)
		}
	}
	return ""
}

func orName(skill, source string) string {
	if skill != "" && fs.ValidPath(skill) && !strings.Contains(skill, "/") {
		return skill
	}
	return source
}

// statusError is an answer other than 200: 429 and 5xx are tried again
// (a Retry-After kept), anything else is fatal.
type statusError struct {
	code   int
	answer string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("answered %d %s", e.code, e.answer)
}

func (e *statusError) Retryable() bool {
	return e.code == http.StatusTooManyRequests || e.code >= 500
}

func fetch(ctx context.Context, client *http.Client, addr, key, version string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/v1/skills", nil)
	if err != nil {
		return nil, retry.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Mira-Version", version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err // a lost connection or a timeout: tried again
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	se := &statusError{code: resp.StatusCode, answer: excerpt.Bytes(body)}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 && se.Retryable() {
		return nil, retry.After(se, time.Duration(secs)*time.Second)
	}
	return nil, se
}

// allowed: https, or http to this machine itself (decision 14).
func allowed(addr string) bool {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// host is what a problem of the whole source is named by.
func host(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		return u.Host
	}
	return addr
}

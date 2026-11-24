package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/miraassist/mira-app/internal/skills"
	"github.com/miraassist/mira-app/internal/store"
	"github.com/miraassist/mira-app/internal/tools"
)

// toolFinish finishes a thread with a tool of its target's skill
// (docs/spec/threads.md, finishes; docs/spec/inbox.md, section 9): the
// thread's fields are the tool's arguments, by their names; the tool's text is
// the thread's message, its error the thread's error. A thread that asks for a
// yes, or one started by code from a document, calls it only on the yes, and
// that yes is the tool's confirmation too (it goes past the hooks' confirm,
// not past their refusal).
type toolFinish struct {
	m  *Manager
	sk skills.Skill
}

// toolCall is what a tool finish keeps in the thread until the yes.
type toolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

func (f toolFinish) Async() bool { return false }

func (f toolFinish) args(th store.Thread) map[string]any {
	args := map[string]any{}
	for k, v := range th.Fields {
		if !notNeeded(v) {
			args[k] = v
		}
	}
	return args
}

func (f toolFinish) Work(ctx context.Context, th store.Thread) (Result, error) {
	c := toolCall{Tool: f.sk.Target.Finish.Tool, Args: f.args(th)}
	content, _ := json.Marshal(c)
	t := f.m.texts()
	if f.sk.Target.Confirm || th.Source != "" {
		proposal := fmt.Sprintf(t.actionProposal, th.Request, describeData(c.Args))
		if th.Source != "" {
			// The person was offered it already; their answer is the yes.
			proposal += " " + t.sourceReady
		}
		return Result{Proposal: proposal, Content: content}, nil
	}
	text, err := f.call(ctx, th, c)
	if err != nil {
		return Result{}, err
	}
	return Result{Done: text, Content: content}, nil
}

func (f toolFinish) Commit(ctx context.Context, th store.Thread) (string, error) {
	var c toolCall
	if err := json.Unmarshal(th.Content, &c); err != nil || c.Tool == "" {
		return "", errors.New("the thread holds no tool call")
	}
	// This is the yes; what the hooks refuse stays refused.
	if dec := f.m.check(ctx, th, f.sk.Name, c.Tool, c.Args); dec.Outcome == tools.Deny {
		return "", errors.New(dec.Reason)
	}
	return f.call(ctx, th, c)
}

// call runs the tool. A thread started by code from a document holds its
// text: the call counts as after third-party text (inbox.md, decision 41).
func (f toolFinish) call(ctx context.Context, th store.Thread, c toolCall) (string, error) {
	args, err := json.Marshal(c.Args)
	if err != nil {
		return "", err
	}
	text, isError, err := f.m.opts.RunTool(ctx, c.Tool, args, th.Source != "", th.ID)
	keys := slices.Sorted(maps.Keys(c.Args))
	switch {
	case err != nil:
		f.m.log.Warn("thread tool failed", "thread", th.ID, "tool", c.Tool, "args", keys, "error", err)
		return "", err
	case isError:
		f.m.log.Warn("thread tool answered an error", "thread", th.ID, "tool", c.Tool, "args", keys, "answer", clipText(text))
		return "", errors.New(text)
	}
	f.m.log.Info("thread tool called", "thread", th.ID, "tool", c.Tool, "args", keys)
	return text, nil
}

func clipText(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

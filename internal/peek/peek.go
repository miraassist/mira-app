// Package peek marks a context as read-only: code that would change state
// (a link, a skill opened, a note taken) leaves it and says what it left,
// so a speculative run can build a turn's request without changing
// anything (docs/spec/speculative-turns.md, decision 5).
package peek

import (
	"context"
	"sync"
)

// Note collects the writes a peeking call left out.
type Note struct {
	mu   sync.Mutex
	left []string
}

type key struct{}

// With marks ctx as peeking; the Note says what was left out.
func With(ctx context.Context) (context.Context, *Note) {
	n := &Note{}
	return context.WithValue(ctx, key{}, n), n
}

// Active says whether ctx only peeks.
func Active(ctx context.Context) bool {
	_, ok := ctx.Value(key{}).(*Note)
	return ok
}

// Left records a write a peeking call left out; nothing outside a peek.
func Left(ctx context.Context, what string) {
	if n, ok := ctx.Value(key{}).(*Note); ok {
		n.mu.Lock()
		n.left = append(n.left, what)
		n.mu.Unlock()
	}
}

// Left are the writes left out, in order.
func (n *Note) Left() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.left...)
}

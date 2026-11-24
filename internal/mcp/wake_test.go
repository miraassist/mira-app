package mcp

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/testsupport/fakemcp"
)

// BS-13 A companion's registration wakes its MCP servers
// (docs/spec/companions.md, decision 21).
func TestBS13_ACompanionsRegistrationWakesItsMCPServers(t *testing.T) {
	pro, other := fakemcp.HA("pro"), fakemcp.HA("other")
	t.Cleanup(pro.Close)
	t.Cleanup(other.Close)
	var down atomic.Bool
	down.Store(true)
	// The skills' addresses as their folders name them; the fakes answer at their own.
	at := map[string]string{"mail": pro.Endpoint(), "musiklicht": pro.Endpoint(), "else": other.Endpoint()}
	h := New(Options{Logger: slog.New(slog.DiscardHandler), Servers: []config.MCPServer{
		{Name: "mail", Transport: config.TransportHTTP, URL: "http://pro:8792/mcp/mail"},
		{Name: "musiklicht", Transport: config.TransportHTTP, URL: "http://pro:8792/mcp/musiklicht"},
		{Name: "else", Transport: config.TransportHTTP, URL: "http://other:9000/mcp"},
	}, Connector: func(ctx context.Context, client *sdk.Client, cfg config.MCPServer) (*sdk.ClientSession, error) {
		if down.Load() {
			return nil, errors.New("connection refused")
		}
		cfg.URL = at[cfg.Name]
		return DialTransport(ctx, client, cfg)
	}})
	// Given the servers are not connected (the companion was down at the start)
	// Start says that none is reachable; the host runs on.
	if err := h.Start(context.Background()); err == nil {
		t.Fatal("Start connected while the companion was down")
	}
	t.Cleanup(h.Close)
	for _, s := range h.servers {
		if s.connected() {
			t.Fatalf("%s connected while down", s.cfg.Name)
		}
	}
	down.Store(false)
	// When the companion pro registers with the address http://pro:8792
	woken := h.WakeUnder(context.Background(), "http://pro:8792/")
	// Then Mira connects the servers under that address at once, and leaves the other one to its retry loop
	if len(woken) != 2 || !h.byName["mail"].connected() || !h.byName["musiklicht"].connected() || h.byName["else"].connected() {
		t.Fatalf("woken %v, else connected %v", woken, h.byName["else"].connected())
	}
	// And a server under that address that is connected keeps its session
	before := current(h.byName["mail"])
	if again := h.WakeUnder(context.Background(), "http://pro:8792"); len(again) != 0 || current(h.byName["mail"]) != before {
		t.Errorf("woken again %v", again)
	}
	// An address that only begins like it is not under it.
	if w := h.WakeUnder(context.Background(), "http://pro:879"); len(w) != 0 {
		t.Errorf("woken %v", w)
	}
}

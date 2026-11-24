package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/miraassist/mira-app/internal/announce"
	"github.com/miraassist/mira-app/internal/api"
	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/setup"
	"github.com/miraassist/mira-app/internal/store"
)

// prepared is the config as the service runs with it: mira.yaml, if there is
// one, with what the integration set up laid over it
// (docs/spec/setup.md).
type prepared struct {
	cfg config.Config
	// pairing: the token is the service's own, so Home Assistant pairs (the
	// first time without a code). A token in mira.yaml is entered instead.
	pairing bool
	// open: never paired, so the first pairing needs no code.
	open bool
	// ready: Home Assistant and a model are set; otherwise the service
	// waits to be set up.
	ready    bool
	hasModel bool
	// haFromSetup: Home Assistant's address and token came from the
	// integration's setup, not from mira.yaml.
	haFromSetup bool
	// logTo is where the service logs; standard error when nil (tests
	// read the log to learn what the service did).
	logTo io.Writer
	// code: the pairing code for another Home Assistant, made when one
	// let go.
	code string
	// path is the mira.yaml Mira read and the page writes
	// (docs/spec/editing.md).
	path string
	// mainMoveErr: the setup's model of before could not move into
	// mira.yaml and is still laid over it (docs/spec/main-in-file.md,
	// decision 3).
	mainMoveErr error
}

// moveMain writes the model the database holds from before into mira.yaml
// and reads the file anew. When the file cannot take it, the model stays in
// the database and is laid over the file as before; p says why, for the
// problem and the page, and the next start tries again.
func moveMain(ctx context.Context, st *store.Store, path, file string, cfg config.Config, hasLLM bool,
	s *setup.Settings, p *prepared) (config.Config, bool, error) {
	// What of main in the file the setup's values replace, by name only.
	covered := slices.DeleteFunc(setup.Covered(cfg, *s), func(name string) bool {
		return !strings.HasPrefix(name, "models.main.")
	})
	err := setup.WriteMain(ctx, st, config.File{Path: file, Secrets: config.DefaultSecrets()}, *s.LLM)
	switch {
	case errors.Is(err, setup.ErrNotDeleted):
		// The file holds the same values the database lays over it; the
		// next start deletes the copy.
		slog.Error("the setup's model moved into mira.yaml, but its copy in the database could not be deleted; the next start tries again",
			"file", file, "error", err)
	case err != nil:
		slog.Warn("the setup's model could not move into mira.yaml; it is laid over the file as before, and the next start tries again",
			"file", file, "error", err)
		p.mainMoveErr = fmt.Errorf("%s: %w", file, err)
		return cfg, hasLLM, nil
	default:
		s.LLM = nil
		slog.Info("the setup's model moved into mira.yaml", "file", file, "replaced", strings.Join(covered, ", "))
	}
	moved, has, err := config.LoadOptional(path)
	if err != nil {
		return cfg, hasLLM, configError{err}
	}
	return moved, has, nil
}

func prepare(ctx context.Context, path string) (prepared, error) {
	cfg, hasLLM, err := config.LoadOptional(path)
	if err != nil {
		return prepared{}, configError{err}
	}
	return prepareFrom(ctx, path, cfg, hasLLM, true)
}

// configError is a mira.yaml that cannot be read or does not fit: Mira
// then starts limited (docs/spec/parsing.md, PA-07).
type configError struct{ err error }

func (e configError) Error() string { return e.err.Error() }
func (e configError) Unwrap() error { return e.err }

// prepareFrom is prepare with the config already read; validate is false
// for the limited start, whose config is only its defaults and addresses.
func prepareFrom(ctx context.Context, path string, cfg config.Config, hasLLM, validate bool) (prepared, error) {
	p := prepared{pairing: cfg.Server.Token == "", path: config.ConfigPath(path)}
	// The Home Assistant app keeps its options next to the data.
	options := ""
	if dir := os.Getenv("MIRA_DATA"); dir != "" {
		options = filepath.Join(dir, "options.json")
	}
	if reset, err := setup.ResetIfAsked(cfg.Store.Path, options); err != nil {
		return prepared{}, fmt.Errorf("reset: %w", err)
	} else if reset {
		slog.Warn("Mira's data were deleted as asked (reset); mira.yaml stays. Mira waits to be set up, like a new install")
	}
	st, err := store.Open(ctx, store.Options{Path: cfg.Store.Path})
	if err != nil {
		return prepared{}, err
	}
	defer st.Close()
	s, err := setup.Load(ctx, st)
	if err != nil {
		return prepared{}, err
	}
	if s.LLM != nil && validate {
		// The setup's model of before moves into mira.yaml
		// (docs/spec/main-in-file.md, decision 3).
		if cfg, hasLLM, err = moveMain(ctx, st, path, p.path, cfg, hasLLM, &s, &p); err != nil {
			return prepared{}, err
		}
	}
	if covered := setup.Covered(cfg, s); len(covered) > 0 {
		// docs/spec/kv-cleanup.md, decision 9: what the integration set
		// wins; the log says which settings of mira.yaml it covers.
		slog.Info("setup covers mira.yaml: the integration's values are used", "settings", strings.Join(covered, ", "))
	}
	p.cfg = setup.Apply(cfg, s)
	p.haFromSetup = s.HAURL != "" && s.HAToken != ""
	if validate {
		if err := p.cfg.Validate(); err != nil {
			return prepared{}, configError{err}
		}
	}
	p.ready = setup.Ready(p.cfg, hasLLM, s)
	if cfg.LacksMain() && s.LLM == nil {
		// MO-02: main comes from the integration; until it is set up, Mira
		// waits for the setup and says why.
		slog.Warn("mira.yaml has models but not main, and the integration has set no model yet: Mira waits for the setup in Home Assistant (or add models.main to mira.yaml)")
	}
	p.hasModel = hasLLM || s.LLM != nil
	if p.pairing {
		p.open = s.Open
		if p.code, err = setup.PendingCode(ctx, st); err != nil {
			return prepared{}, err
		}
	}
	return p, nil
}

// errNotSetUp is for commands that need a set-up service.
var errNotSetUp = errors.New("Mira is not set up yet: add Mira in Home Assistant (Settings → Devices & services)")

// restart starts the service anew with what setup saved: the same program,
// arguments and environment.
func restart() {
	exe, err := os.Executable()
	if err != nil {
		slog.Error("cannot restart", "error", err)
		os.Exit(1)
	}
	slog.Info("starting anew with the new settings")
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		slog.Error("cannot restart", "error", err)
		os.Exit(1)
	}
}

// model is the model set, or "" while there is none.
func (p prepared) model() string {
	if !p.hasModel {
		return ""
	}
	return p.cfg.Main().Model
}

func logPairing(log *slog.Logger, p prepared) {
	switch {
	case p.code != "":
		log.Info("paired before: the same Home Assistant pairs again without a code; another one needs this pairing code (Settings → Devices & services → Mira)",
			"code", setup.ShowCode(p.code))
	case p.open:
		log.Info("not paired yet: add Mira in Home Assistant under Settings → Devices & services (Discovered)")
	}
}

// runSetupMode serves only what setup needs until the integration has set
// the service up; then it starts anew.
func runSetupMode(ctx context.Context, p prepared) error {
	log := slog.Default()
	st, err := store.Open(ctx, store.Options{Path: p.cfg.Store.Path})
	if err != nil {
		return err
	}
	defer st.Close()
	id, err := st.InstanceID(ctx)
	if err != nil {
		return err
	}
	log.Info("waiting to be set up by Home Assistant", "listen", p.cfg.Server.Listen)
	logPairing(log, p)
	mux := api.NewMux()
	(&setup.Handler{Store: st, Token: p.cfg.Server.Token, ID: id, Version: version, Pairing: p.pairing,
		SetupMode: true, Model: p.model(), Restart: restart, Log: log,
		Config: config.File{Path: p.path, Secrets: config.DefaultSecrets()}}).Register(mux)
	ln, err := net.Listen("tcp", p.cfg.Server.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: serverRead, WriteTimeout: serverWrite, IdleTimeout: serverIdle}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if p.cfg.Server.Announce && !announce.Loopback(p.cfg.Server.Listen) {
		go func() {
			if err := announce.Run(ctx, announce.Options{Instance: p.cfg.Server.Name, ID: id, Version: version,
				Listen: p.cfg.Server.Listen, Logger: log}); err != nil && ctx.Err() == nil {
				log.Warn("mdns announcement failed", "error", err)
			}
		}()
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runReset asks for a reset: at the next start Mira deletes its data,
// keeps mira.yaml and pairs again without a code, like a new install.
func runReset(p prepared) error {
	if err := setup.AskReset(p.cfg.Store.Path); err != nil {
		return err
	}
	fmt.Println("Mira deletes its data (memory, messages, settings from Home Assistant, the model) at the next start; mira.yaml stays. Restart Mira now, e.g. docker restart mira.")
	return nil
}

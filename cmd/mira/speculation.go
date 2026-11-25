package main

import (
	"context"
	"fmt"

	"github.com/miraassist/mira-app/internal/ha/structure"
	haws "github.com/miraassist/mira-app/internal/ha/ws"
	"github.com/miraassist/mira-app/internal/sidechannel"
)

// runSideChannel subscribes to the side channel of the speech-to-text
// (docs/spec/speculative-turns.md, step 1): its turns get the satellite in
// listening from a watch of the satellites' states.
func (a *app) runSideChannel(ctx context.Context, snapshot func() structure.Snapshot) error {
	w, err := a.ha.WatchStates(ctx, haws.StateFilter{Domains: []string{"assist_satellite"}}, nil)
	if err != nil {
		return fmt.Errorf("side channel: cannot watch the satellites: %w", err)
	}
	defer w.Stop()
	c := sidechannel.New(sidechannel.Options{
		URL:    a.cfg.Speculation.EventsURL,
		Logger: a.log,
		Listening: func() []string {
			snap := snapshot()
			var out []string
			for _, s := range w.States() {
				if s.State == "listening" {
					if d := snap.Entities[s.EntityID].DeviceID; d != "" {
						out = append(out, d)
					}
				}
			}
			return out
		},
		Handle: a.engine.Speculate,
	})
	return c.Run(ctx)
}

package setup

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira-app/internal/config"
	"github.com/miraassist/mira-app/internal/store"
)

// MainToken is the secret main's key is kept under, the name Mira's page
// gives it too.
var MainToken = config.SecretName("MODEL", config.MainModel, "TOKEN")

// mainChange is what the setup's model changes: the key in the secrets file
// and models.main in mira.yaml (docs/spec/main-in-file.md, What the setup
// writes). What the setup decides is replaced, the rest of main stays.
func mainChange(l LLM) (map[string]string, func(top *yaml.Node) error) {
	secrets := map[string]string{MainToken: strings.TrimSpace(l.APIKey)}
	local := l.Kind != KindCloud
	return secrets, func(top *yaml.Node) error {
		models := config.MapChild(top, "models")
		n := config.MapGet(models, config.MainModel)
		if n == nil || n.Kind != yaml.MappingNode {
			n = &yaml.Node{Kind: yaml.MappingNode}
			config.MapSet(models, config.MainModel, n)
		}
		str := func(key, v string) {
			config.MapSet(n, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
		}
		str("endpoint", strings.TrimRight(strings.TrimSpace(l.BaseURL), "/"))
		str("model", l.Model)
		if secrets[MainToken] != "" {
			str("token", "${"+MainToken+"}")
		} else {
			config.MapDelete(n, "token")
		}
		privacy := config.PrivacyCloud
		if local {
			privacy = config.PrivacyLocal
		}
		str("privacy", privacy)
		if l.ContextTokens > 0 {
			config.MapSet(n, "context_tokens", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(l.ContextTokens)})
		} else {
			// The size said before belonged to another model.
			config.MapDelete(n, "context_tokens")
		}
		for _, key := range []string{"cache_prompt", "warmup_on_start", "warmup_on_prefix_change"} {
			if local {
				config.MapSet(n, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			} else {
				config.MapDelete(n, key)
			}
		}
		if !local {
			config.MapDelete(n, "warm_slots")
		}
		// The default by privacy applies, as it did when the setup's model
		// was laid over the file.
		config.MapDelete(n, "parallel")
		config.MapDelete(n, "pin_slots")
		return nil
	}
}

// CheckMain says whether the setup's model can go into mira.yaml, without
// writing anything.
func CheckMain(f config.File, l LLM) error {
	secrets, fn := mainChange(l)
	return f.Check(secrets, fn)
}

// WriteMain puts the setup's model into mira.yaml and its key into the
// secrets file; then a model the database still holds from before is gone,
// so it is no longer laid over the file.
func WriteMain(ctx context.Context, st *store.Store, f config.File, l LLM) error {
	secrets, fn := mainChange(l)
	if err := f.Change(secrets, fn); err != nil {
		return err
	}
	if err := st.DeleteKV(ctx, keyLLM); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

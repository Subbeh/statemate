package cli

import (
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/secrets"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/template"
)

func newScanner(cfg *config.Config, profileName string) (*source.Scanner, error) {
	tmplCtx, err := template.NewContext(cfg, profileName)
	if err != nil {
		return nil, err
	}

	var enc *encrypt.AgeEncryptor
	if cfg.Age != nil {
		enc, _ = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
	}

	identitySource := ""
	if cfg.Age != nil {
		identitySource = cfg.Age.Identity
	}
	mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
	if err == nil {
		tmplCtx.SecretLookup = func(item, typ, field string) (string, error) {
			key := secrets.CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
			return mgr.Get(key)
		}
	}

	renderer := func(data []byte) ([]byte, error) {
		return template.Render(data, tmplCtx)
	}

	return source.NewScannerWithRenderer(cfg.TargetBase, cfg.SourceDir(), renderer), nil
}

package cli

import (
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/template"
)

func newScanner(cfg *config.Config, profileName string) (*source.Scanner, error) {
	tmplCtx, err := template.NewContext(cfg, profileName)
	if err != nil {
		return nil, err
	}

	renderer := func(data []byte) ([]byte, error) {
		return template.Render(data, tmplCtx)
	}

	return source.NewScannerWithRenderer(cfg.TargetBase, cfg.SourceDir(), renderer), nil
}

package secrets

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/subbeh/statemate/internal/config"
)

type CommandProvider struct{}

func NewCommandProvider() *CommandProvider {
	return &CommandProvider{}
}

func (c *CommandProvider) Name() string {
	return "command"
}

func (c *CommandProvider) Available() error {
	return nil
}

func (c *CommandProvider) Fetch(items []config.SecretItem) (map[string]string, error) {
	results := make(map[string]string)
	for _, item := range items {
		if item.Cmd == "" {
			return nil, fmt.Errorf("secret %q: command provider requires 'cmd' field", item.Path)
		}
		out, err := exec.Command("sh", "-c", item.Cmd).Output()
		if err != nil {
			return nil, fmt.Errorf("running command for %q: %w", item.Path, err)
		}
		results[item.Path] = strings.TrimSpace(string(out))
	}
	return results, nil
}

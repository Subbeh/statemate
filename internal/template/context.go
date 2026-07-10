package template

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
)

type SecretLookup func(item, typ, field string) (string, error)

type DecryptFunc func(ciphertext []byte) ([]byte, error)

type Context struct {
	Profile      string
	Hostname     string
	OS           string
	Arch         string
	HomeDir      string
	Username     string
	SourceDir    string
	Vars         map[string]any
	Env          map[string]string
	SecretLookup SecretLookup
	Decrypt      DecryptFunc
}

type ContextOption func(*Context)

func WithDecrypt(fn DecryptFunc) ContextOption {
	return func(c *Context) {
		c.Decrypt = fn
	}
}

func NewContext(cfg *config.Config, profileName string, opts ...ContextOption) (*Context, error) {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	ctx := &Context{
		Profile:   profileName,
		Hostname:  hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		HomeDir:   home,
		Username:  username,
		SourceDir: cfg.SourceDir(),
		Vars:      make(map[string]any),
		Env:       make(map[string]string),
	}

	for _, opt := range opts {
		opt(ctx)
	}

	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			ctx.Env[parts[0]] = parts[1]
		}
	}

	for k, v := range cfg.Variables {
		ctx.Vars[k] = v
	}

	for _, varFile := range cfg.VarFiles {
		if err := ctx.loadVarFile(varFile); err != nil {
			return nil, err
		}
	}

	for name, cmd := range cfg.VarCmds {
		val, err := runCommand(cmd)
		if err != nil {
			return nil, err
		}
		ctx.Vars[name] = strings.TrimSpace(val)
	}

	if profileName != "" {
		resolved := profile.Resolve(cfg, profileName)
		if resolved != nil {
			for k, v := range resolved.Variables {
				ctx.Vars[k] = v
			}
		}
	}

	return ctx, nil
}

func (c *Context) loadVarFile(path string) error {
	if strings.HasPrefix(path, "~/") {
		path = c.HomeDir + path[1:]
	} else if !strings.HasPrefix(path, "/") && c.SourceDir != "" {
		path = c.SourceDir + "/" + path
	}

	encrypted := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Try the #encrypted variant
		if _, err := os.Stat(path + "#encrypted"); err == nil {
			path = path + "#encrypted"
			encrypted = true
		} else {
			return nil
		}
	} else if strings.HasSuffix(path, "#encrypted") {
		encrypted = true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if encrypted {
		if c.Decrypt == nil {
			return fmt.Errorf("var_file %s is encrypted but no age identity is configured", path)
		}
		data, err = c.Decrypt(data)
		if err != nil {
			return fmt.Errorf("decrypting var_file %s: %w", path, err)
		}
		// Strip #encrypted suffix for format detection
		path = strings.TrimSuffix(path, "#encrypted")
	}

	vars, err := parseVarFile(data, path)
	if err != nil {
		return err
	}

	for k, v := range vars {
		c.Vars[k] = v
	}

	return nil
}

func parseVarFile(data []byte, path string) (map[string]any, error) {
	vars := make(map[string]any)

	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return parseYAML(data)
	}
	if strings.HasSuffix(path, ".toml") {
		return parseTOML(data)
	}

	return vars, nil
}

func runCommand(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (c *Context) Cmd(cmd string) string {
	out, err := runCommand(cmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

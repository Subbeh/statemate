package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/subbeh/statemate/internal/util"
	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = FindConfig()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{}
	cfg.sourceDir = filepath.Dir(path)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing TOML: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}

	localCfg := loadLocalConfig()
	cfg.ApplyOverrides(localCfg)

	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}

	if err := cfg.loadIncludes(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func FindConfig() (string, error) {
	if envDir := os.Getenv("STATEMATE_DIR"); envDir != "" {
		return findConfigInDir(expandHome(envDir))
	}

	if lc := loadLocalConfig(); lc != nil && lc.SourceDirPath != "" {
		return findConfigInDir(expandHome(lc.SourceDirPath))
	}

	return findConfigInDir(".")
}

func SourceDir() string {
	if envDir := os.Getenv("STATEMATE_DIR"); envDir != "" {
		return expandHome(envDir)
	}

	if lc := loadLocalConfig(); lc != nil && lc.SourceDirPath != "" {
		return expandHome(lc.SourceDirPath)
	}

	cwd, _ := os.Getwd()
	return cwd
}

func LocalConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "statemate", "mate.yaml")
}

func loadLocalConfig() *Config {
	data, err := os.ReadFile(LocalConfigPath())
	if err != nil {
		return nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

func SaveLocalSourceDir(dir string) error {
	path := LocalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	content := fmt.Sprintf("source_dir: %q\n", util.ShortenPath(dir))
	return os.WriteFile(path, []byte(content), 0644)
}

func expandHome(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func findConfigInDir(dir string) (string, error) {
	candidates := []string{"mate.yaml", "mate.yml", "mate.toml"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no config file found in %s (tried: %s)", dir, strings.Join(candidates, ", "))
}

func LoadDirConfig(dir string) (*DirConfig, error) {
	return LoadDirConfigRaw(dir, nil)
}

type TemplateRenderer func([]byte) ([]byte, error)

func LoadDirConfigRaw(dir string, render TemplateRenderer) (*DirConfig, error) {
	path, err := findDirConfig(dir)
	if err != nil {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading dir config: %w", err)
	}

	if render != nil {
		data, err = render(data)
		if err != nil {
			return nil, fmt.Errorf("rendering dir config template: %w", err)
		}
	}

	cfg := &DirConfig{}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing TOML: %w", err)
		}
	}

	return cfg, nil
}


func findDirConfig(dir string) (string, error) {
	candidates := []string{".mate.yaml", ".mate.yml", ".mate.toml"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no dir config found")
}


func (c *Config) setDefaults() error {
	if c.TargetBase == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		c.TargetBase = home
	} else if c.TargetBase == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		c.TargetBase = home
	} else if strings.HasPrefix(c.TargetBase, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		c.TargetBase = filepath.Join(home, c.TargetBase[2:])
	}

	if c.Variables == nil {
		c.Variables = make(map[string]any)
	}
	if c.VarCmds == nil {
		c.VarCmds = make(map[string]string)
	}

	return nil
}

func (c *Config) loadIncludes() error {
	// Load top-level includes
	for _, f := range c.Include {
		partial, err := loadIncludeFile(c.resolveRelPath(f))
		if err != nil {
			return fmt.Errorf("loading include %s: %w", f, err)
		}
		c.mergePartial(partial)
	}

	// Load profile-level includes
	for _, profile := range c.Profiles {
		if profile == nil || len(profile.Include) == 0 {
			continue
		}
		for _, f := range profile.Include {
			partial, err := loadIncludeFile(c.resolveRelPath(f))
			if err != nil {
				return fmt.Errorf("loading include %s: %w", f, err)
			}
			profile.mergePartial(partial)
		}
	}

	return nil
}

type includeFile struct {
	Packages  *PackageList   `yaml:"packages" toml:"packages"`
	Variables map[string]any `yaml:"variables" toml:"variables"`
}

func loadIncludeFile(path string) (*includeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var inc includeFile
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &inc); err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &inc); err != nil {
			return nil, fmt.Errorf("parsing TOML: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported format: %s", filepath.Ext(path))
	}

	return &inc, nil
}

func (c *Config) mergePartial(inc *includeFile) {
	c.Packages = mergePackageLists(c.Packages, inc.Packages)
	c.Variables = mergeVariables(c.Variables, inc.Variables)
}

func (p *Profile) mergePartial(inc *includeFile) {
	p.Packages = mergePackageLists(p.Packages, inc.Packages)
	p.Variables = mergeVariables(p.Variables, inc.Variables)
}

func (c *Config) resolveRelPath(path string) string {
	path = expandHome(path)
	if !filepath.IsAbs(path) {
		return filepath.Join(c.sourceDir, path)
	}
	return path
}

func mergePackageLists(base, add *PackageList) *PackageList {
	if add == nil {
		return base
	}
	if base == nil {
		return add
	}

	return &PackageList{
		Common: appendUniqueStrings(base.Common, add.Common...),
		Brew:   appendUniqueStrings(base.Brew, add.Brew...),
		Pacman: appendUniqueStrings(base.Pacman, add.Pacman...),
		AUR:    appendUniqueStrings(base.AUR, add.AUR...),
	}
}

func mergeVariables(base, add map[string]any) map[string]any {
	if add == nil {
		return base
	}
	if base == nil {
		return add
	}
	for k, v := range add {
		base[k] = v
	}
	return base
}

func appendUniqueStrings(slice []string, items ...string) []string {
	seen := make(map[string]bool)
	for _, s := range slice {
		seen[s] = true
	}
	result := slice
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

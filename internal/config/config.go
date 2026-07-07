package config

import "path/filepath"

type Config struct {
	SourceDirPath string              `yaml:"source_dir" toml:"source_dir"`
	Sources       []string            `yaml:"sources" toml:"sources"`
	DefaultSource string              `yaml:"default_source" toml:"default_source"`
	TargetBase    string              `yaml:"target_base" toml:"target_base"`
	Profiles      map[string]*Profile `yaml:"profiles" toml:"profiles"`
	Profile       string              `yaml:"profile" toml:"profile"`
	Editor        string              `yaml:"editor" toml:"editor"`
	Age           *AgeConfig          `yaml:"age" toml:"age"`
	Variables     map[string]any      `yaml:"variables" toml:"variables"`
	VarFiles      []string            `yaml:"var_files" toml:"var_files"`
	VarCmds       map[string]string   `yaml:"variable_commands" toml:"variable_commands"`
	Packages     *PackageList `yaml:"packages" toml:"packages"`
	Include      []string     `yaml:"include" toml:"include"`
	AURHelper    string       `yaml:"aur_helper" toml:"aur_helper"`
	SecretsCache string       `yaml:"secrets_cache" toml:"secrets_cache"`

	sourceDir string
}

func (c *Config) ApplyOverrides(override *Config) {
	if override == nil {
		return
	}
	if len(override.Sources) > 0 {
		c.Sources = override.Sources
	}
	if override.DefaultSource != "" {
		c.DefaultSource = override.DefaultSource
	}
	if override.TargetBase != "" {
		c.TargetBase = override.TargetBase
	}
	if override.Profile != "" {
		c.Profile = override.Profile
	}
	if override.Editor != "" {
		c.Editor = override.Editor
	}
	if override.Age != nil {
		c.Age = override.Age
	}
	if override.Variables != nil {
		c.Variables = override.Variables
	}
	if len(override.VarFiles) > 0 {
		c.VarFiles = override.VarFiles
	}
	if override.VarCmds != nil {
		c.VarCmds = override.VarCmds
	}
	if override.Packages != nil {
		c.Packages = override.Packages
	}
	if override.Profiles != nil {
		c.Profiles = override.Profiles
	}
}

func (c *Config) SourceDir() string {
	return c.sourceDir
}

func (c *Config) AbsoluteSources() []string {
	return c.ResolveSourcePaths(c.Sources)
}

func (c *Config) ResolveSourcePaths(sources []string) []string {
	result := make([]string, len(sources))
	for i, s := range sources {
		if filepath.IsAbs(s) {
			result[i] = s
		} else {
			result[i] = filepath.Join(c.sourceDir, s)
		}
	}
	return result
}

type Profile struct {
	Extends   string         `yaml:"extends" toml:"extends"`
	Sources   []string       `yaml:"sources" toml:"sources"`
	Detection *Detection     `yaml:"detection" toml:"detection"`
	Packages  *PackageList   `yaml:"packages" toml:"packages"`
	Include   []string       `yaml:"include" toml:"include"`
	Variables map[string]any `yaml:"variables" toml:"variables"`
}

type Detection struct {
	Mode     string `yaml:"mode" toml:"mode"`
	Hostname any    `yaml:"hostname" toml:"hostname"`
	User     any    `yaml:"user" toml:"user"`
	OS       string `yaml:"os" toml:"os"`
	Arch     string `yaml:"arch" toml:"arch"`
	Command  string `yaml:"command" toml:"command"`
}

type AgeConfig struct {
	Identity        string   `yaml:"identity" toml:"identity"`
	IdentityCommand string   `yaml:"identity_command" toml:"identity_command"`
	Recipients      []string `yaml:"recipients" toml:"recipients"`
}

type PackageList struct {
	Common []string `yaml:"common" toml:"common"`
	Brew   []string `yaml:"brew" toml:"brew"`
	Pacman []string `yaml:"pacman" toml:"pacman"`
	AUR    []string `yaml:"aur" toml:"aur"`
}

type DirConfig struct {
	Profile  string            `yaml:"profile" toml:"profile"`
	Targets  map[string]string `yaml:"targets" toml:"targets"`
	Owner    string            `yaml:"owner" toml:"owner"`
	Group    string            `yaml:"group" toml:"group"`
	Perm     string            `yaml:"perm" toml:"perm"`
	Packages *PackageList      `yaml:"packages" toml:"packages"`
	Scripts  *DirScripts       `yaml:"scripts" toml:"scripts"`
}

type DirScripts struct {
	BeforeApply []string `yaml:"before_apply" toml:"before_apply"`
	AfterApply  []string `yaml:"after_apply" toml:"after_apply"`
}

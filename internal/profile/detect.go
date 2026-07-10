package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/subbeh/statemate/internal/config"
)

func Detect(cfg *config.Config) string {
	if cfg.Profile != "" {
		return cfg.Profile
	}

	if p := os.Getenv("STATEMATE_PROFILE"); p != "" {
		return p
	}

	if cfg.Profiles == nil {
		return ""
	}

	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	// Sort profiles: children before parents (more specific first), then alphabetically
	names := sortByInheritanceDepth(cfg)

	for _, name := range names {
		profile := cfg.Profiles[name]
		if profile == nil || profile.Detection == nil {
			continue
		}
		if matches(profile.Detection, hostname, username) {
			return name
		}
	}

	return ""
}

func matches(d *config.Detection, hostname, username string) bool {
	mode := d.Mode
	if mode == "" {
		mode = "or"
	}

	var results []bool

	if d.Hostname != nil {
		results = append(results, matchPattern(d.Hostname, hostname))
	}

	if d.User != nil {
		results = append(results, matchPattern(d.User, username))
	}

	if d.OS != "" {
		results = append(results, runtime.GOOS == d.OS)
	}

	if d.Arch != "" {
		results = append(results, runtime.GOARCH == d.Arch)
	}

	if d.Command != "" {
		results = append(results, runCommand(d.Command))
	}

	if len(results) == 0 {
		return false
	}

	if mode == "and" {
		for _, r := range results {
			if !r {
				return false
			}
		}
		return true
	}

	for _, r := range results {
		if r {
			return true
		}
	}
	return false
}

func matchPattern(pattern any, value string) bool {
	switch p := pattern.(type) {
	case string:
		return matchGlob(p, value)
	case []any:
		for _, pat := range p {
			if s, ok := pat.(string); ok && matchGlob(s, value) {
				return true
			}
		}
	case []string:
		for _, pat := range p {
			if matchGlob(pat, value) {
				return true
			}
		}
	}
	return false
}

func matchGlob(pattern, value string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}

func runCommand(command string) bool {
	if strings.HasPrefix(command, "~/") {
		home, _ := os.UserHomeDir()
		command = filepath.Join(home, command[2:])
	}

	cmd := exec.Command("sh", "-c", command)
	err := cmd.Run()
	return err == nil
}

func ResolveSources(cfg *config.Config, profileName string) []string {
	if profileName == "" || cfg.Profiles == nil {
		return cfg.Sources
	}

	profile := cfg.Profiles[profileName]
	if profile == nil {
		return cfg.Sources
	}

	sources := make([]string, len(cfg.Sources))
	copy(sources, cfg.Sources)

	chain := resolveInheritanceChain(cfg, profileName)

	for _, pName := range chain {
		p := cfg.Profiles[pName]
		if p != nil && len(p.Sources) > 0 {
			sources = append(sources, p.Sources...)
		}
	}

	return sources
}

// InheritanceChain returns the full chain of profiles from ancestors to the
// given profile (e.g. ["macos", "work"] for profile "work" which extends "macos").
func InheritanceChain(cfg *config.Config, profileName string) []string {
	return resolveInheritanceChain(cfg, profileName)
}

func resolveInheritanceChain(cfg *config.Config, profileName string) []string {
	var chain []string
	seen := make(map[string]bool)

	current := profileName
	for current != "" && !seen[current] {
		seen[current] = true
		p := cfg.Profiles[current]
		if p == nil {
			break
		}
		if p.Extends != "" {
			chain = append([]string{p.Extends}, chain...)
		}
		current = p.Extends
	}

	chain = append(chain, profileName)
	return chain
}

func sortByInheritanceDepth(cfg *config.Config) []string {
	type profileDepth struct {
		name  string
		depth int
	}

	var profiles []profileDepth
	for name := range cfg.Profiles {
		depth := 0
		current := name
		seen := make(map[string]bool)
		for current != "" && !seen[current] {
			seen[current] = true
			p := cfg.Profiles[current]
			if p == nil || p.Extends == "" {
				break
			}
			depth++
			current = p.Extends
		}
		profiles = append(profiles, profileDepth{name, depth})
	}

	// Sort by depth descending (deepest first), then alphabetically
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].depth != profiles[j].depth {
			return profiles[i].depth > profiles[j].depth
		}
		return profiles[i].name < profiles[j].name
	})

	names := make([]string, len(profiles))
	for i, p := range profiles {
		names[i] = p.name
	}
	return names
}

func AllSources(cfg *config.Config) []string {
	seen := make(map[string]bool)
	var sources []string

	for _, s := range cfg.Sources {
		if !seen[s] {
			seen[s] = true
			sources = append(sources, s)
		}
	}

	if cfg.Profiles != nil {
		for _, p := range cfg.Profiles {
			if p == nil {
				continue
			}
			for _, s := range p.Sources {
				if !seen[s] {
					seen[s] = true
					sources = append(sources, s)
				}
			}
		}
	}

	return sources
}

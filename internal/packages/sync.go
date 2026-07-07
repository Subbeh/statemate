package packages

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/subbeh/statemate/internal/config"
)

type SyncResult struct {
	Manager  string
	Statuses []PackageStatus
}

type syncOptions struct {
	verbose bool
}

type SyncOption func(*syncOptions)

func WithVerbose(v bool) SyncOption {
	return func(o *syncOptions) { o.verbose = v }
}

func (r *SyncResult) Missing() []string {
	var result []string
	for _, s := range r.Statuses {
		if s.Status == StatusMissing {
			result = append(result, s.Name)
		}
	}
	return result
}

func (r *SyncResult) Extra() []string {
	var result []string
	for _, s := range r.Statuses {
		if s.Status == StatusExtra {
			result = append(result, s.Name)
		}
	}
	return result
}

func ComputeSync(cfg *config.Config, profileName string, sources []string, opts ...SyncOption) ([]SyncResult, error) {
	var o syncOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Collect packages with source tracking
	// key: "manager:pkgspec", value: list of sources
	type pkgEntry struct {
		manager string
		spec    string
		sources []string
	}
	entries := make(map[string]*pkgEntry) // key: "manager\x00name"

	addPkgs := func(pkgs *config.PackageList, source string) {
		if pkgs == nil {
			return
		}
		add := func(manager string, specs []string) {
			for _, spec := range specs {
				name, _ := ParsePackageSpec(spec)
				key := manager + "\x00" + name
				if e, ok := entries[key]; ok {
					e.sources = appendUnique(e.sources, source)
				} else {
					entries[key] = &pkgEntry{manager: manager, spec: spec, sources: []string{source}}
				}
			}
		}
		add("common", pkgs.Common)
		add("brew", pkgs.Brew)
		add("pacman", pkgs.Pacman)
		add("aur", pkgs.AUR)
	}

	// Global packages
	if cfg.Packages != nil {
		addPkgs(cfg.Packages, "config")
	}

	// Profile-specific packages
	if profileName != "" {
		if profile, ok := cfg.Profiles[profileName]; ok {
			if profile.Packages != nil {
				addPkgs(profile.Packages, "profile:"+profileName)
			}
		}
	}

	// Source directory packages
	for _, source := range sources {
		dirCfg, _ := config.LoadDirConfig(source)
		if dirCfg != nil && dirCfg.Packages != nil {
			addPkgs(dirCfg.Packages, filepath.Base(source))
		}
	}

	// Resolve common packages to the primary available manager
	primaryManager := getPrimaryManager(cfg.AURHelper)
	for key, e := range entries {
		if e.manager != "common" {
			continue
		}
		if primaryManager == "" {
			continue
		}
		name, _ := ParsePackageSpec(e.spec)
		targetKey := primaryManager + "\x00" + name
		if existing, ok := entries[targetKey]; ok {
			for _, s := range e.sources {
				existing.sources = appendUnique(existing.sources, s)
			}
		} else {
			entries[targetKey] = &pkgEntry{manager: primaryManager, spec: e.spec, sources: e.sources}
		}
		delete(entries, key)
	}

	// Group by manager
	managerPkgs := make(map[string][]*pkgEntry)
	for _, e := range entries {
		managerPkgs[e.manager] = append(managerPkgs[e.manager], e)
	}

	// Ensure all available managers are included
	for _, m := range GetAvailableManagersWithHelper(cfg.AURHelper) {
		if _, ok := managerPkgs[m.Name()]; !ok {
			managerPkgs[m.Name()] = nil
		}
	}

	var results []SyncResult

	for managerName, pkgs := range managerPkgs {
		manager, err := GetManager(managerName, cfg.AURHelper)
		if err != nil {
			continue
		}
		if !manager.IsAvailable() {
			continue
		}

		installed, err := manager.ListInstalled()
		if err != nil {
			return nil, err
		}

		installedMap := make(map[string]Package)
		for _, p := range installed {
			installedMap[p.Name] = p
		}

		wantedMap := make(map[string]*pkgEntry)
		for _, e := range pkgs {
			name, _ := ParsePackageSpec(e.spec)
			wantedMap[name] = e
		}

		result := SyncResult{Manager: managerName}

		for name, e := range wantedMap {
			_, version := ParsePackageSpec(e.spec)
			if inst, ok := installedMap[name]; ok {
				status := PackageStatus{
					Name:      name,
					Version:   version,
					Status:    StatusInstalled,
					Installed: inst.Version,
					Sources:   e.sources,
				}
				if version != "" && inst.Version != "" && inst.Version != version {
					status.Status = StatusVersionMismatch
				}
				result.Statuses = append(result.Statuses, status)
			} else {
				result.Statuses = append(result.Statuses, PackageStatus{
					Name:    name,
					Version: version,
					Status:  StatusMissing,
					Sources: e.sources,
				})
			}
		}

		for name, inst := range installedMap {
			if _, ok := wantedMap[name]; !ok {
				result.Statuses = append(result.Statuses, PackageStatus{
					Name:      name,
					Status:    StatusExtra,
					Installed: inst.Version,
				})
			}
		}

		if o.verbose {
			var names []string
			for i := range result.Statuses {
				names = append(names, result.Statuses[i].Name)
			}
			if descs, err := manager.Describe(names); err == nil {
				for i := range result.Statuses {
					result.Statuses[i].Description = descs[result.Statuses[i].Name]
				}
			}
		}

		sort.Slice(result.Statuses, func(i, j int) bool {
			iExtra := result.Statuses[i].Status == StatusExtra
			jExtra := result.Statuses[j].Status == StatusExtra
			if iExtra != jExtra {
				return !iExtra
			}
			si := strings.Join(result.Statuses[i].Sources, ",")
			sj := strings.Join(result.Statuses[j].Sources, ",")
			if si != sj {
				return si < sj
			}
			return result.Statuses[i].Name < result.Statuses[j].Name
		})

		results = append(results, result)
	}

	return results, nil
}

// getPrimaryManager returns the first available package manager for common packages
func getPrimaryManager(aurHelper string) string {
	managers := []Manager{
		NewBrewManager(),
		NewPacmanManager(),
	}

	for _, m := range managers {
		if m.IsAvailable() {
			return m.Name()
		}
	}
	return ""
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

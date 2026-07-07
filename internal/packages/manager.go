package packages

import (
	"fmt"
	"strings"
)

type Manager interface {
	Name() string
	IsAvailable() bool
	ListInstalled() ([]Package, error)
	Describe(pkgs []string) (map[string]string, error)
	Install(pkgs []string) error
	Uninstall(pkgs []string) error
}

type Package struct {
	Name        string
	Version     string
	Description string
}

func (p Package) String() string {
	if p.Version != "" {
		return p.Name + "@" + p.Version
	}
	return p.Name
}

type PackageStatus struct {
	Name        string
	Version     string
	Status      Status
	Sources     []string
	Installed   string
	Description string
}

type Status int

const (
	StatusInstalled Status = iota
	StatusMissing
	StatusExtra
	StatusVersionMismatch
)

func (s Status) String() string {
	switch s {
	case StatusInstalled:
		return "installed"
	case StatusMissing:
		return "missing"
	case StatusExtra:
		return "extra"
	case StatusVersionMismatch:
		return "version mismatch"
	default:
		return "unknown"
	}
}

func ParsePackageSpec(spec string) (name, version string) {
	if idx := strings.Index(spec, "@"); idx != -1 {
		return spec[:idx], spec[idx+1:]
	}
	return spec, ""
}

func GetManager(name string, aurHelper string) (Manager, error) {
	switch name {
	case "brew":
		return NewBrewManager(), nil
	case "pacman":
		return NewPacmanManager(), nil
	case "aur":
		return NewAURManager(aurHelper), nil
	default:
		return nil, fmt.Errorf("unknown package manager: %s", name)
	}
}

func GetAvailableManagers() []Manager {
	return GetAvailableManagersWithHelper("")
}

func GetAvailableManagersWithHelper(aurHelper string) []Manager {
	managers := []Manager{
		NewBrewManager(),
		NewPacmanManager(),
		NewAURManager(aurHelper),
	}

	var available []Manager
	for _, m := range managers {
		if m.IsAvailable() {
			available = append(available, m)
		}
	}
	return available
}

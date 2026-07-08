package packages

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

type BrewManager struct{}

func NewBrewManager() *BrewManager {
	return &BrewManager{}
}

func (b *BrewManager) Name() string {
	return "brew"
}

func (b *BrewManager) IsAvailable() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func (b *BrewManager) ListInstalled() ([]Package, error) {
	// Use brew leaves to get only explicitly installed packages (not dependencies)
	cmd := exec.Command("brew", "leaves", "--installed-on-request")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var packages []Package
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			packages = append(packages, Package{Name: name})
		}
	}

	// Also get explicitly installed casks
	cmd = exec.Command("brew", "list", "--cask", "-1")
	out.Reset()
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		scanner = bufio.NewScanner(&out)
		for scanner.Scan() {
			name := strings.TrimSpace(scanner.Text())
			if name != "" {
				packages = append(packages, Package{Name: name})
			}
		}
	}

	return packages, scanner.Err()
}

func (b *BrewManager) Describe(pkgs []string) (map[string]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	args := append([]string{"info", "--json=v2"}, pkgs...)
	cmd := exec.Command("brew", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var info struct {
		Formulae []struct {
			Name string `json:"name"`
			Desc string `json:"desc"`
		} `json:"formulae"`
		Casks []struct {
			Token string `json:"token"`
			Desc  string `json:"desc"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, f := range info.Formulae {
		result[f.Name] = f.Desc
	}
	for _, c := range info.Casks {
		result[c.Token] = c.Desc
	}
	return result, nil
}

func (b *BrewManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"install"}, pkgs...)
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (b *BrewManager) Uninstall(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"uninstall"}, pkgs...)
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

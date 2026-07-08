package packages

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
)

type PacmanManager struct{}

func NewPacmanManager() *PacmanManager {
	return &PacmanManager{}
}

func (p *PacmanManager) Name() string {
	return "pacman"
}

func (p *PacmanManager) IsAvailable() bool {
	_, err := exec.LookPath("pacman")
	return err == nil
}

func (p *PacmanManager) ListInstalled() ([]Package, error) {
	cmd := exec.Command("pacman", "-Qentt")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var packages []Package
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 1 {
			pkg := Package{Name: parts[0]}
			if len(parts) >= 2 {
				pkg.Version = parts[1]
			}
			packages = append(packages, pkg)
		}
	}

	return packages, scanner.Err()
}

func (p *PacmanManager) Describe(pkgs []string) (map[string]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	args := append([]string{"-Qi"}, pkgs...)
	cmd := exec.Command("pacman", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	result := make(map[string]string)
	var currentName string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Name") {
			currentName = strings.TrimSpace(strings.TrimPrefix(line, "Name            :"))
		} else if strings.HasPrefix(line, "Description") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "Description     :"))
			if currentName != "" {
				result[currentName] = desc
			}
		}
	}
	return result, scanner.Err()
}

func (p *PacmanManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"-S", "--noconfirm"}, pkgs...)
	cmd := exec.Command("sudo", append([]string{"pacman"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (p *PacmanManager) Uninstall(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"-R", "--noconfirm"}, pkgs...)
	cmd := exec.Command("sudo", append([]string{"pacman"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

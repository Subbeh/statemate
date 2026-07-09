package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

type BitwardenProvider struct{}

func NewBitwardenProvider() *BitwardenProvider {
	return &BitwardenProvider{}
}

func (b *BitwardenProvider) Name() string {
	return "bitwarden"
}

func (b *BitwardenProvider) Available() error {
	_, err := exec.LookPath("bw")
	if err != nil {
		return fmt.Errorf("bw CLI not found in PATH")
	}
	return nil
}

func (b *BitwardenProvider) Fetch(items []FetchItem) (map[string]string, error) {
	if err := b.ensureUnlocked(); err != nil {
		return nil, err
	}

	if err := b.sync(); err != nil {
		return nil, fmt.Errorf("syncing vault: %w", err)
	}

	bwItems, err := b.listItems()
	if err != nil {
		return nil, fmt.Errorf("listing items: %w", err)
	}

	results := make(map[string]string)
	for _, item := range items {
		value, err := b.extractValue(item, bwItems)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", item.Key.String(), err)
		}
		results[item.Key.String()] = value
	}

	return results, nil
}

func (b *BitwardenProvider) ensureUnlocked() error {
	status, err := b.getStatus()
	if err != nil {
		return fmt.Errorf("checking vault status: %w", err)
	}

	switch status {
	case "unlocked":
		return nil
	case "locked":
		return b.unlock()
	case "unauthenticated":
		return fmt.Errorf("not logged in to Bitwarden. Run: bw login")
	default:
		return fmt.Errorf("unexpected vault status: %s", status)
	}
}

func (b *BitwardenProvider) getStatus() (string, error) {
	out, err := exec.Command("bw", "status").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return "", fmt.Errorf("%s", stderr)
			}
		}
		return "", err
	}

	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("failed to parse bw status output: %s", strings.TrimSpace(string(out)))
	}
	return status.Status, nil
}

func (b *BitwardenProvider) unlock() error {
	// Try unlock without password first (biometric/PIN)
	cmd := exec.Command("bw", "unlock", "--raw")
	out, err := cmd.Output()
	if err == nil {
		session := strings.TrimSpace(string(out))
		_ = os.Setenv("BW_SESSION", session)
		return nil
	}

	// Fall back to password prompt
	fmt.Print("Bitwarden master password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}

	cmd = exec.Command("bw", "unlock", "--raw", string(password))
	out, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("unlocking vault: %w", err)
	}

	session := strings.TrimSpace(string(out))
	_ = os.Setenv("BW_SESSION", session)
	return nil
}

func (b *BitwardenProvider) sync() error {
	out, err := exec.Command("bw", "sync").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

type bwItem struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Fields []bwField `json:"fields"`
	Login  *bwLogin  `json:"login"`
	SSHKey *bwSSHKey `json:"sshKey"`
}

type bwField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type bwLogin struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	URIs     []bwURI `json:"uris"`
}

type bwURI struct {
	URI string `json:"uri"`
}

type bwSSHKey struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

func (b *BitwardenProvider) listItems() ([]bwItem, error) {
	out, err := exec.Command("bw", "list", "items").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("%s", stderr)
			}
		}
		return nil, err
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, fmt.Errorf("vault is locked or session is invalid. Run: bw unlock")
	}

	var items []bwItem
	if err := json.Unmarshal(out, &items); err != nil {
		if !strings.HasPrefix(trimmed, "[") {
			return nil, fmt.Errorf("vault is locked or session is invalid. Run: bw unlock")
		}
		return nil, err
	}
	return items, nil
}

func (b *BitwardenProvider) extractValue(item FetchItem, bwItems []bwItem) (string, error) {
	var found *bwItem
	for i := range bwItems {
		if bwItems[i].Name == item.Item {
			found = &bwItems[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("item %q not found in vault", item.Item)
	}

	switch item.Type {
	case "field":
		return b.extractField(found, item.Field)
	case "ssh":
		return b.extractSSH(found, item.Field)
	case "attachment":
		return b.extractAttachment(found, item.Filename)
	case "login":
		return b.extractLogin(found, item.Field)
	case "totp":
		return b.extractTOTP(found)
	default:
		return "", fmt.Errorf("unknown type %q", item.Type)
	}
}

func (b *BitwardenProvider) extractField(item *bwItem, fieldName string) (string, error) {
	for _, f := range item.Fields {
		if f.Name == fieldName {
			return f.Value, nil
		}
	}
	return "", fmt.Errorf("field %q not found on item %q", fieldName, item.Name)
}

func (b *BitwardenProvider) extractSSH(item *bwItem, field string) (string, error) {
	if item.SSHKey == nil {
		return "", fmt.Errorf("item %q is not an SSH key", item.Name)
	}
	switch field {
	case "private":
		return item.SSHKey.PrivateKey, nil
	case "public":
		return item.SSHKey.PublicKey, nil
	default:
		return "", fmt.Errorf("unknown SSH field %q (use 'private' or 'public')", field)
	}
}

func (b *BitwardenProvider) extractAttachment(item *bwItem, filename string) (string, error) {
	tmpFile, err := os.CreateTemp("", "mate-attachment-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	cmd := exec.Command("bw", "get", "attachment", filename, "--itemid", item.ID, "--output", tmpPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("downloading attachment %q: %w", filename, err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}

	return encodeBase64(data), nil
}

func (b *BitwardenProvider) extractLogin(item *bwItem, field string) (string, error) {
	if item.Login == nil {
		return "", fmt.Errorf("item %q is not a login", item.Name)
	}
	switch field {
	case "username":
		return item.Login.Username, nil
	case "password":
		return item.Login.Password, nil
	case "uri":
		if len(item.Login.URIs) > 0 {
			return item.Login.URIs[0].URI, nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unknown login field %q (use 'username', 'password', or 'uri')", field)
	}
}

func (b *BitwardenProvider) extractTOTP(item *bwItem) (string, error) {
	out, err := exec.Command("bw", "get", "totp", item.ID).Output()
	if err != nil {
		return "", fmt.Errorf("generating TOTP: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
)

type CachedValue struct {
	Value     string    `json:"value"`
	FetchedAt time.Time `json:"fetched_at"`
}

type Cache struct {
	FetchedAt time.Time               `json:"fetched_at"`
	Items     map[string]*CachedValue `json:"items"`
}

type Provider interface {
	Name() string
	Available() error
	Fetch(items []config.SecretItem) (map[string]string, error)
}

type ProgressFunc func(path string, changed bool)

type Manager struct {
	cfg        *config.SecretsConfig
	providers  map[string]Provider
	enc        *encrypt.AgeEncryptor
	identity   age.Identity
	cache      *Cache
	cachePath  string
	onProgress ProgressFunc
}

func NewManager(cfg *config.SecretsConfig, enc *encrypt.AgeEncryptor, identitySource string) (*Manager, error) {
	m := &Manager{
		cfg:       cfg,
		providers: make(map[string]Provider),
		enc:       enc,
	}

	m.cachePath = cfg.Cache
	if m.cachePath == "" {
		stateDir, err := defaultCacheDir()
		if err != nil {
			return nil, err
		}
		m.cachePath = filepath.Join(stateDir, "secrets.age")
	} else {
		m.cachePath = expandPath(m.cachePath)
	}

	if identitySource != "" {
		identities, err := loadIdentity(identitySource)
		if err != nil {
			return nil, fmt.Errorf("loading identity for secrets cache: %w", err)
		}
		if len(identities) > 0 {
			m.identity = identities[0]
		}
	}

	m.providers["bitwarden"] = NewBitwardenProvider()
	m.providers["command"] = NewCommandProvider()

	return m, nil
}

func (m *Manager) SetProgress(fn ProgressFunc) {
	m.onProgress = fn
}

func (m *Manager) Fetch(pattern string) (*FetchResult, error) {
	result := &FetchResult{}

	if err := m.loadCache(); err != nil {
		m.cache = &Cache{Items: make(map[string]*CachedValue)}
	}

	for providerName, provCfg := range m.cfg.Providers {
		provider, ok := m.providers[providerName]
		if !ok {
			return nil, fmt.Errorf("unknown provider: %s", providerName)
		}

		if err := provider.Available(); err != nil {
			return nil, fmt.Errorf("provider %s not available: %w", providerName, err)
		}

		items := filterItems(provCfg.Items, pattern)
		if len(items) == 0 {
			continue
		}

		values, err := provider.Fetch(items)
		if err != nil {
			return nil, fmt.Errorf("fetching from %s: %w", providerName, err)
		}

		now := time.Now()
		for path, value := range values {
			old, exists := m.cache.Items[path]
			changed := !exists || old.Value != value
			if changed {
				result.Changed++
			} else {
				result.Unchanged++
			}
			m.cache.Items[path] = &CachedValue{
				Value:     value,
				FetchedAt: now,
			}
			result.Total++
			if m.onProgress != nil {
				m.onProgress(path, changed)
			}
		}
	}

	m.cache.FetchedAt = time.Now()
	if err := m.saveCache(); err != nil {
		return nil, fmt.Errorf("saving cache: %w", err)
	}

	result.Unchanged = result.Total - result.Changed
	return result, nil
}

func (m *Manager) LoadSecrets() (map[string]any, error) {
	if err := m.loadCache(); err != nil {
		return nil, err
	}

	secrets := make(map[string]any)
	for path, cached := range m.cache.Items {
		setNestedValue(secrets, path, cached.Value)
	}
	return secrets, nil
}

func (m *Manager) ListItems() ([]*ListEntry, error) {
	_ = m.loadCache()

	var entries []*ListEntry
	for providerName, provCfg := range m.cfg.Providers {
		for _, item := range provCfg.Items {
			entry := &ListEntry{
				Path:     item.Path,
				Provider: providerName,
				Status:   StatusMissing,
			}
			if m.cache != nil {
				if cached, ok := m.cache.Items[item.Path]; ok {
					entry.Status = StatusCached
					entry.FetchedAt = cached.FetchedAt
				}
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (m *Manager) PendingCount() int {
	_ = m.loadCache()

	count := 0
	for _, provCfg := range m.cfg.Providers {
		for _, item := range provCfg.Items {
			if m.cache == nil {
				count++
				continue
			}
			if _, ok := m.cache.Items[item.Path]; !ok {
				count++
			}
		}
	}
	return count
}

func (m *Manager) HasSecrets() bool {
	for _, provCfg := range m.cfg.Providers {
		if len(provCfg.Items) > 0 {
			return true
		}
	}
	return false
}

func (m *Manager) CachePath() string {
	return m.cachePath
}

func (m *Manager) loadCache() error {
	if m.cache != nil {
		return nil
	}

	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no secrets cache found")
		}
		return err
	}

	var plaintext []byte
	if m.enc != nil && m.enc.CanDecrypt() {
		plaintext, err = m.enc.Decrypt(data)
		if err != nil {
			return fmt.Errorf("decrypting secrets cache: %w", err)
		}
	} else {
		plaintext = data
	}

	m.cache = &Cache{}
	return json.Unmarshal(plaintext, m.cache)
}

func (m *Manager) saveCache() error {
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.cache, "", "  ")
	if err != nil {
		return err
	}

	if m.identity != nil {
		recipient, err := identityToRecipient(m.identity)
		if err != nil {
			return fmt.Errorf("deriving recipient from identity: %w", err)
		}
		localEnc, err := encrypt.NewAgeEncryptor("", "", []string{recipient})
		if err != nil {
			return err
		}
		return localEnc.EncryptToFile(data, m.cachePath)
	}

	return os.WriteFile(m.cachePath, data, 0600)
}

type FetchResult struct {
	Total     int
	Changed   int
	Unchanged int
}

type ListEntry struct {
	Path      string
	Provider  string
	Status    ListStatus
	FetchedAt time.Time
}

type ListStatus int

const (
	StatusCached  ListStatus = iota
	StatusMissing
	StatusNew
)

func (s ListStatus) String() string {
	switch s {
	case StatusCached:
		return "cached"
	case StatusMissing:
		return "missing"
	case StatusNew:
		return "new"
	default:
		return "unknown"
	}
}

func setNestedValue(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
		} else {
			if next, ok := current[part]; ok {
				if nextMap, ok := next.(map[string]any); ok {
					current = nextMap
				} else {
					newMap := make(map[string]any)
					current[part] = newMap
					current = newMap
				}
			} else {
				newMap := make(map[string]any)
				current[part] = newMap
				current = newMap
			}
		}
	}
}

func filterItems(items []config.SecretItem, pattern string) []config.SecretItem {
	if pattern == "" {
		return items
	}
	var filtered []config.SecretItem
	for _, item := range items {
		if matchPattern(item.Path, pattern) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func matchPattern(path, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return path == pattern
}

func defaultCacheDir() (string, error) {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateDir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateDir, "statemate"), nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func loadIdentity(source string) ([]age.Identity, error) {
	if strings.HasPrefix(source, "AGE-SECRET-KEY-") {
		identity, err := age.ParseX25519Identity(source)
		if err != nil {
			return nil, err
		}
		return []age.Identity{identity}, nil
	}

	path := expandPath(source)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return age.ParseIdentities(f)
}

func identityToRecipient(id age.Identity) (string, error) {
	x25519Id, ok := id.(*age.X25519Identity)
	if !ok {
		return "", fmt.Errorf("unsupported identity type for recipient derivation")
	}
	return x25519Id.Recipient().String(), nil
}

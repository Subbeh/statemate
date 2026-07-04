package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/subbeh/statemate/internal/encrypt"
)

type CacheKey struct {
	Provider string `json:"provider"`
	Item     string `json:"item"`
	Type     string `json:"type"`
	Field    string `json:"field"`
}

func (k CacheKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.Provider, k.Item, k.Type, k.Field)
}

type CachedValue struct {
	Value     string    `json:"value"`
	FetchedAt time.Time `json:"fetched_at"`
}

type Cache struct {
	FetchedAt time.Time                `json:"fetched_at"`
	Items     map[string]*CachedValue  `json:"items"`
}

type Provider interface {
	Name() string
	Available() error
	Fetch(items []FetchItem) (map[string]string, error)
}

type FetchItem struct {
	Key      CacheKey
	Item     string
	Type     string
	Field    string
	Filename string
}

type ProgressFunc func(key CacheKey, changed bool)

type Manager struct {
	providers  map[string]Provider
	enc        *encrypt.AgeEncryptor
	identity   age.Identity
	cache      *Cache
	cachePath  string
	onProgress ProgressFunc
}

func NewManager(enc *encrypt.AgeEncryptor, identitySource string, cachePath string) (*Manager, error) {
	m := &Manager{
		providers: make(map[string]Provider),
		enc:       enc,
	}

	if cachePath != "" {
		m.cachePath = expandPath(cachePath)
	} else {
		stateDir, err := defaultCacheDir()
		if err != nil {
			return nil, err
		}
		m.cachePath = filepath.Join(stateDir, "secrets.age")
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

	return m, nil
}

func (m *Manager) SetProgress(fn ProgressFunc) {
	m.onProgress = fn
}

func (m *Manager) Fetch(items []FetchItem) (*FetchResult, error) {
	result := &FetchResult{}

	if err := m.loadCache(); err != nil {
		m.cache = &Cache{Items: make(map[string]*CachedValue)}
	}

	// Group items by provider
	byProvider := make(map[string][]FetchItem)
	for _, item := range items {
		byProvider[item.Key.Provider] = append(byProvider[item.Key.Provider], item)
	}

	for providerName, provItems := range byProvider {
		provider, ok := m.providers[providerName]
		if !ok {
			return nil, fmt.Errorf("unknown provider: %s", providerName)
		}

		if err := provider.Available(); err != nil {
			return nil, fmt.Errorf("provider %s not available: %w", providerName, err)
		}

		values, err := provider.Fetch(provItems)
		if err != nil {
			return nil, fmt.Errorf("fetching from %s: %w", providerName, err)
		}

		now := time.Now()
		for keyStr, value := range values {
			old, exists := m.cache.Items[keyStr]
			changed := !exists || old.Value != value
			if changed {
				result.Changed++
			} else {
				result.Unchanged++
			}
			m.cache.Items[keyStr] = &CachedValue{
				Value:     value,
				FetchedAt: now,
			}
			result.Total++

			if m.onProgress != nil {
				// Parse key back for progress reporting
				key := parseCacheKeyString(keyStr)
				m.onProgress(key, changed)
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

func (m *Manager) Get(key CacheKey) (string, error) {
	if err := m.loadCache(); err != nil {
		return "", fmt.Errorf("secrets cache not available: %w", err)
	}

	cached, ok := m.cache.Items[key.String()]
	if !ok {
		return "", fmt.Errorf("secret not cached: %s (run 'mate secrets fetch')", key.String())
	}
	return cached.Value, nil
}

func (m *Manager) ListCached() map[string]*CachedValue {
	_ = m.loadCache()
	if m.cache == nil {
		return nil
	}
	return m.cache.Items
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

func parseCacheKeyString(s string) CacheKey {
	parts := strings.SplitN(s, ":", 4)
	if len(parts) != 4 {
		return CacheKey{}
	}
	return CacheKey{
		Provider: parts[0],
		Item:     parts[1],
		Type:     parts[2],
		Field:    parts[3],
	}
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

package secrets

import (
	"os"
	"strings"
)

func DiscoverFromDecryptedFiles(paths []string, decryptFn func([]byte) ([]byte, error)) ([]FetchItem, error) {
	seen := make(map[string]bool)
	var all []FetchItem

	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if isEncrypted(content) && decryptFn != nil {
			decrypted, err := decryptFn(content)
			if err == nil {
				content = decrypted
			}
		}

		items := DiscoverFromContent(string(content))
		for _, item := range items {
			keyStr := item.Key.String()
			if !seen[keyStr] {
				seen[keyStr] = true
				all = append(all, item)
			}
		}
	}
	return all, nil
}

func DiscoverFromContent(content string) []FetchItem {
	var items []FetchItem
	// Simple line-by-line scan for bitwarden calls with all-literal args
	// Format: {{ bitwarden "item" "type" "field" }}
	for _, line := range strings.Split(content, "\n") {
		items = append(items, parseBitwardenCalls(line)...)
	}
	return items
}

func parseBitwardenCalls(line string) []FetchItem {
	var items []FetchItem
	rest := line
	for {
		idx := strings.Index(rest, "bitwarden ")
		if idx == -1 {
			break
		}
		rest = rest[idx+len("bitwarden "):]

		// Try to parse quoted args
		args, remaining := parseQuotedArgs(rest)
		if len(args) >= 3 {
			key := CacheKey{
				Provider: "bitwarden",
				Item:     args[0],
				Type:     args[1],
				Field:    args[2],
			}
			filename := ""
			if len(args) >= 4 {
				filename = args[3]
			}
			items = append(items, FetchItem{
				Key:      key,
				Item:     args[0],
				Type:     args[1],
				Field:    args[2],
				Filename: filename,
			})
		}
		rest = remaining
	}
	return items
}

func parseQuotedArgs(s string) ([]string, string) {
	var args []string
	rest := strings.TrimSpace(s)
	for {
		if len(rest) == 0 || rest[0] == '}' {
			break
		}
		if rest[0] != '"' {
			// Non-quoted arg (template variable) — can't resolve statically
			break
		}
		// Find closing quote
		end := strings.Index(rest[1:], "\"")
		if end == -1 {
			break
		}
		args = append(args, rest[1:end+1])
		rest = strings.TrimSpace(rest[end+2:])
	}
	return args, rest
}

func isEncrypted(content []byte) bool {
	return strings.HasPrefix(string(content), "-----BEGIN AGE ENCRYPTED FILE-----")
}

// HasDynamicSecrets checks if content has bitwarden calls with non-literal args
func HasDynamicSecrets(content string) bool {
	rest := content
	for {
		idx := strings.Index(rest, "bitwarden ")
		if idx == -1 {
			return false
		}
		rest = rest[idx+len("bitwarden "):]
		trimmed := strings.TrimSpace(rest)
		if len(trimmed) > 0 && trimmed[0] != '"' {
			return true
		}
	}
}

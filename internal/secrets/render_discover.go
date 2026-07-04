package secrets

import (
	"bytes"
	"os"
	"strings"
	"text/template"

	tmpl "github.com/subbeh/statemate/internal/template"
)

// DiscoverByRendering renders templates and collects all bitwarden() calls made.
// This resolves dynamic arguments (variables in range loops, etc).
func DiscoverByRendering(paths []string, ctx *tmpl.Context, decryptFn func([]byte) ([]byte, error)) []FetchItem {
	seen := make(map[string]bool)
	var all []FetchItem

	// Replace the SecretLookup with one that records calls
	origLookup := ctx.SecretLookup
	defer func() { ctx.SecretLookup = origLookup }()

	ctx.SecretLookup = func(item, typ, field string) (string, error) {
		key := CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
		keyStr := key.String()
		if !seen[keyStr] {
			seen[keyStr] = true
			filename := ""
			if typ == "attachment" {
				filename = field
			}
			all = append(all, FetchItem{
				Key:      key,
				Item:     item,
				Type:     typ,
				Field:    field,
				Filename: filename,
			})
		}
		return "PLACEHOLDER", nil
	}

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

		// Try to render — ignore errors (we just want to discover calls)
		t, err := template.New("").Funcs(discoveryFuncMap(ctx)).Parse(string(content))
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		_ = t.Execute(&buf, ctx)
	}

	return all
}

func discoveryFuncMap(ctx *tmpl.Context) template.FuncMap {
	return template.FuncMap{
		"env": func(name string) string {
			return ctx.Env[name]
		},
		"var": func(name string) any {
			return ctx.Vars[name]
		},
		"cmd": func(cmd string) string {
			return ""
		},
		"default": func(def, val any) any {
			if val == nil || val == "" {
				return def
			}
			return val
		},
		"required": func(val any) (any, error) {
			if val == nil || val == "" {
				return "", nil
			}
			return val, nil
		},
		"base64Decode": func(val string) (string, error) {
			return val, nil
		},
		"bitwarden": func(item, typ, field string) (string, error) {
			return ctx.SecretLookup(item, typ, field)
		},
		"bitwardenAttachment": func(item, filename string) (string, error) {
			return ctx.SecretLookup(item, "attachment", filename)
		},
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"join":      strings.Join,
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"trim":      strings.TrimSpace,
		"replace":   strings.ReplaceAll,
	}
}

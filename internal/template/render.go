package template

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"text/template"
)

func Render(content []byte, ctx *Context) ([]byte, error) {
	tmpl, err := template.New("").Funcs(funcMap(ctx)).Parse(string(content))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func RenderFile(path string, ctx *Context) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Render(content, ctx)
}

func funcMap(ctx *Context) template.FuncMap {
	return template.FuncMap{
		"env": func(name string) string {
			return ctx.Env[name]
		},
		"var": func(name string) any {
			return ctx.Vars[name]
		},
		"cmd": func(cmd string) string {
			return ctx.Cmd(cmd)
		},
		"default": func(def, val any) any {
			if val == nil || val == "" {
				return def
			}
			return val
		},
		"required": func(val any) (any, error) {
			if val == nil || val == "" {
				return nil, fmt.Errorf("required value is missing or empty")
			}
			return val, nil
		},
		"base64Decode": func(val string) (string, error) {
			decoded, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return "", fmt.Errorf("base64 decode: %w", err)
			}
			return string(decoded), nil
		},
	}
}

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/secrets"
	"github.com/subbeh/statemate/internal/template"
)

var evalCmd = &cobra.Command{
	Use:   "eval <file>",
	Short: "Render a template file",
	Long: `Render a template file and output the result to stdout.

Useful for debugging templates or previewing output before applying.
If the file is encrypted, it will be decrypted first (requires age identity).

Example:
  mate eval ~/.statemate/files/config.tmpl
  mate eval --profile work ~/.statemate/files/config.tmpl`,
	Args:              cobra.ExactArgs(1),
	RunE:              runEval,
	ValidArgsFunction: completeSourceFiles,
}

func init() {
	rootCmd.AddCommand(evalCmd)
}

func runEval(cmd *cobra.Command, args []string) error {
	filePath := expandPath(args[0])

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	var enc *encrypt.AgeEncryptor
	if cfg.Age != nil {
		enc, err = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
		if err != nil {
			return fmt.Errorf("setting up encryption: %w", err)
		}
	}

	var ctxOpts []template.ContextOption
	if enc != nil && enc.CanDecrypt() {
		ctxOpts = append(ctxOpts, template.WithDecrypt(enc.Decrypt))
	}
	tmplCtx, err := template.NewContext(cfg, profileName, ctxOpts...)
	if err != nil {
		return fmt.Errorf("creating template context: %w", err)
	}

	if enc != nil && enc.CanDecrypt() {
		identitySource := cfg.Age.Identity
		mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
		if err == nil {
			tmplCtx.SecretLookup = func(item, typ, field string) (string, error) {
				key := secrets.CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
				return mgr.Get(key)
			}
		}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if enc != nil && enc.CanDecrypt() && isEncrypted(content) {
		content, err = enc.Decrypt(content)
		if err != nil {
			return fmt.Errorf("decrypting file: %w", err)
		}
	}

	rendered, err := template.Render(content, tmplCtx)
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	_, _ = os.Stdout.Write(rendered)
	return nil
}

func isEncrypted(content []byte) bool {
	return len(content) > 0 && string(content[:min(len(content), 20)]) == "-----BEGIN AGE ENCR"
}

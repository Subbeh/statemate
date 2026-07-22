package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/secrets"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/target"
	"github.com/subbeh/statemate/internal/template"
	"github.com/subbeh/statemate/internal/util"
)

var diffCmd = &cobra.Command{
	Use:               "diff [path]",
	Short:             "Show pending changes",
	Long: `Show full unified diff of pending changes.

Use --tool to specify an external diff tool (e.g., delta, difft, vimdiff).
This can also be set in config with 'diff_tool'.`,
	RunE:              runDiff,
	ValidArgsFunction: completeManagedFiles,
}

func init() {
	diffCmd.Flags().StringP("tool", "t", "", "external diff tool to use")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
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

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	if tree.HasConflicts() {
		fmt.Fprintln(os.Stderr, "Error: conflicting targets detected")
		for _, c := range tree.Conflicts {
			fmt.Fprintf(os.Stderr, "  %s defined in:\n", util.ShortenPath(c.TargetPath))
			for _, s := range c.Sources {
				fmt.Fprintf(os.Stderr, "    - %s\n", util.ShortenPath(s))
			}
		}
		return fmt.Errorf("resolve conflicts before diffing")
	}

	if profileName != "" {
		tree = tree.FilterByProfile(profile.InheritanceChain(cfg, profileName))
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

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

	{
		identitySource := ""
		if cfg.Age != nil {
			identitySource = cfg.Age.Identity
		}
		mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
		if err == nil {
			tmplCtx.SecretLookup = func(item, typ, field string) (string, error) {
				key := secrets.CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
				return mgr.Get(key)
			}
		}
	}

	changes, err := target.ComputeChanges(tree, db, target.ComputeOpts{
		TmplCtx: tmplCtx,
		Enc:     enc,
	})
	if err != nil {
		return fmt.Errorf("computing changes: %w", err)
	}

	var filterPath string
	if len(args) > 0 {
		filterPath = args[0]
	}

	if filterPath != "" {
		var filtered []*target.Change
		for _, c := range changes {
			if matchesPath(c.Entry, filterPath, cfg.SourceDir()) {
				filtered = append(filtered, c)
			}
		}
		changes = filtered
	}

	if len(changes) == 0 {
		fmt.Println("No changes")
		return nil
	}

	diffTool, _ := cmd.Flags().GetString("tool")
	if diffTool == "" {
		diffTool = cfg.DiffTool
	}

	for _, change := range changes {
		if !change.Entry.Generated && target.IsBinaryFile(change.Entry.SourcePath) {
			fmt.Printf("Binary files differ: %s\n", util.ShortenPath(change.Entry.TargetPath))
			continue
		}

		var diff string
		if change.Entry.Generated {
			diff, err = generateGeneratedDiff(change.Entry, diffTool)
		} else if change.Entry.Attrs.Encrypted && change.Entry.Attrs.Template && enc != nil && enc.CanDecrypt() {
			diff, err = generateEncryptedTemplateDiff(change.Entry, enc, tmplCtx, diffTool)
		} else if change.Entry.Attrs.Encrypted && enc != nil && enc.CanDecrypt() {
			diff, err = generateDecryptedDiff(change.Entry, enc, diffTool)
		} else if change.Entry.Attrs.Template {
			diff, err = generateTemplatedDiff(change.Entry, tmplCtx, diffTool)
		} else {
			diff, err = target.GenerateDiffWithTool(change.Entry.SourcePath, change.Entry.TargetPath, diffTool)
		}
		fmt.Printf("=== %s ===\n", util.ShortenPath(change.Entry.TargetPath))
		if err != nil {
			if strings.Contains(err.Error(), "secret not cached") {
				fmt.Fprintf(os.Stderr, "  (secrets not cached, run 'mate secrets fetch')\n")
			} else {
				return fmt.Errorf("generating diff for %s: %w", util.ShortenPath(change.Entry.TargetPath), err)
			}
		} else if diff != "" {
			if diffTool == "" {
				fmt.Println(target.ColorizeDiff(diff))
			} else {
				fmt.Println(diff)
			}
		} else {
			fmt.Println("(no content difference, state mismatch only)")
		}
	}

	return nil
}

func generateEncryptedTemplateDiff(entry *source.Entry, enc *encrypt.AgeEncryptor, tmplCtx *template.Context, diffTool string) (string, error) {
	ciphertext, err := os.ReadFile(entry.SourcePath)
	if err != nil {
		return "", err
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypting source: %w", err)
	}

	rendered, err := template.Render(plaintext, tmplCtx)
	if err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "mate-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.Write(rendered); err != nil {
		return "", err
	}
	_ = tmpFile.Close()

	return target.GenerateDiffWithTool(tmpFile.Name(), entry.TargetPath, diffTool)
}

func generateDecryptedDiff(entry *source.Entry, enc *encrypt.AgeEncryptor, diffTool string) (string, error) {
	ciphertext, err := os.ReadFile(entry.SourcePath)
	if err != nil {
		return "", err
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypting source: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "mate-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.Write(plaintext); err != nil {
		return "", err
	}
	_ = tmpFile.Close()

	return target.GenerateDiffWithTool(tmpFile.Name(), entry.TargetPath, diffTool)
}

func generateTemplatedDiff(entry *source.Entry, tmplCtx *template.Context, diffTool string) (string, error) {
	rendered, err := template.RenderFile(entry.SourcePath, tmplCtx)
	if err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "mate-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.Write(rendered); err != nil {
		return "", err
	}
	_ = tmpFile.Close()

	return target.GenerateDiffWithTool(tmpFile.Name(), entry.TargetPath, diffTool)
}

func generateGeneratedDiff(entry *source.Entry, diffTool string) (string, error) {
	tmpFile, err := os.CreateTemp("", "mate-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.WriteString(entry.GeneratedContent); err != nil {
		return "", err
	}
	_ = tmpFile.Close()

	return target.GenerateDiffWithTool(tmpFile.Name(), entry.TargetPath, diffTool)
}

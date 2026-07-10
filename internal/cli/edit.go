package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/util"
)

var editCmd = &cobra.Command{
	Use:   "edit <source>",
	Short: "Edit a managed file",
	Long: `Edit a managed file in your editor.

For encrypted files, the file is decrypted to a temporary location,
opened in the editor, and re-encrypted after saving.

The editor is determined by (in order):
  1. The 'editor' field in mate.yaml
  2. $VISUAL environment variable
  3. $EDITOR environment variable
  4. vi (fallback)

Examples:
  mate edit nvim/init.lua
  mate edit secrets.yaml`,
	Args:              cobra.ExactArgs(1),
	RunE:              runEdit,
	ValidArgsFunction: completeSourceFiles,
}

func init() {
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	allSources := profile.AllSources(cfg)
	allSourcePaths := cfg.ResolveSourcePaths(allSources)

	scanner := source.NewScannerWithIgnore(cfg.TargetBase, cfg.SourceDir(), nil, cfg.Ignore)
	tree, err := scanner.Scan(allSourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	srcPattern := args[0]

	var entry *source.Entry
	for _, e := range tree.Files() {
		srcDir := strings.TrimSuffix(e.SourcePath, "/"+e.RelPath)
		relPath := filepath.Join(filepath.Base(srcDir), e.RelPath)
		if relPath == srcPattern || e.SourcePath == srcPattern || e.TargetPath == srcPattern ||
			strings.HasSuffix(relPath, "/"+srcPattern) ||
			strings.HasSuffix(e.SourcePath, "/"+srcPattern) ||
			strings.HasSuffix(e.TargetPath, "/"+srcPattern) {
			entry = e
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("file not found: %s", srcPattern)
	}

	editor := getEditor(cfg)

	if !entry.Attrs.Encrypted {
		return runEditor(editor, entry.SourcePath)
	}

	if cfg.Age == nil || (cfg.Age.Identity == "" && cfg.Age.IdentityCommand == "") {
		return fmt.Errorf("no age identity configured for decryption")
	}
	if cfg.Age == nil || len(cfg.Age.Recipients) == 0 {
		return fmt.Errorf("no age recipients configured for encryption")
	}

	enc, err := encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
	if err != nil {
		return fmt.Errorf("setting up encryption: %w", err)
	}

	ciphertext, err := os.ReadFile(entry.SourcePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return fmt.Errorf("decrypting: %w", err)
	}

	baseName := strings.TrimSuffix(filepath.Base(entry.SourcePath), "#encrypted")
	tmpFile, err := os.CreateTemp("", "mate-edit-*-"+baseName)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.Write(plaintext); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	_ = tmpFile.Close()

	beforeHash, err := state.HashFile(tmpPath)
	if err != nil {
		return fmt.Errorf("hashing temp file: %w", err)
	}

	if err := runEditor(editor, tmpPath); err != nil {
		return err
	}

	afterHash, err := state.HashFile(tmpPath)
	if err != nil {
		return fmt.Errorf("hashing temp file: %w", err)
	}

	if beforeHash == afterHash {
		fmt.Println("No changes made")
		return nil
	}

	newPlaintext, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("reading edited file: %w", err)
	}

	newCiphertext, err := enc.Encrypt(newPlaintext)
	if err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}

	if err := os.WriteFile(entry.SourcePath, newCiphertext, entry.Mode.Perm()); err != nil {
		return fmt.Errorf("writing encrypted file: %w", err)
	}

	fmt.Printf("Saved and encrypted: %s\n", util.ShortenPath(entry.SourcePath))

	return nil
}

func getEditor(cfg *config.Config) string {
	if cfg.Editor != "" {
		return cfg.Editor
	}

	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}

	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	return "vi"
}

func runEditor(editor, path string) error {
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	return nil
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/util"
)

var decryptCmd = &cobra.Command{
	Use:   "decrypt <source>",
	Short: "Decrypt a managed file",
	Long: `Decrypt a file in place.

This reads the encrypted file, decrypts it using the configured age identity,
writes it back, and removes the #encrypted suffix from the filename.

The file can be a managed source file or any file path (e.g. a var_file
in .matedata/). Paths are resolved relative to the current directory,
falling back to the source directory. The #encrypted suffix is optional.

The age identity must be configured in mate.yaml:

  age:
    identity: "~/.config/statemate/key.txt"

Examples:
  mate decrypt nvim/secrets.yaml#encrypted
  mate decrypt .matedata/secrets.yaml`,
	Args:              cobra.ExactArgs(1),
	RunE:              runDecrypt,
	ValidArgsFunction: completeEncryptedSourceFiles,
}

func init() {
	rootCmd.AddCommand(decryptCmd)
}

func runDecrypt(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Age == nil || (cfg.Age.Identity == "" && cfg.Age.IdentityCommand == "") {
		return fmt.Errorf("no age identity configured in mate.yaml")
	}

	enc, err := encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, nil)
	if err != nil {
		return fmt.Errorf("setting up decryption: %w", err)
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

	if entry != nil {
		if !entry.Attrs.Encrypted {
			return fmt.Errorf("file is not encrypted: %s", srcPattern)
		}
		return decryptFileAt(entry.SourcePath, entry.Mode.Perm(), enc)
	}

	// Fall back to resolving as a path (try with #encrypted suffix too)
	filePath := resolveEncryptedFilePath(srcPattern, cfg.SourceDir())
	if filePath == "" {
		return fmt.Errorf("file not found: %s", srcPattern)
	}
	if !strings.HasSuffix(filePath, "#encrypted") {
		return fmt.Errorf("file is not encrypted: %s", srcPattern)
	}
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("file not found: %s", srcPattern)
	}

	return decryptFileAt(filePath, info.Mode().Perm(), enc)
}

func decryptFileAt(path string, perm os.FileMode, enc *encrypt.AgeEncryptor) error {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return fmt.Errorf("decrypting: %w", err)
	}

	newPath := strings.TrimSuffix(path, "#encrypted")

	if err := os.WriteFile(newPath, plaintext, perm); err != nil {
		return fmt.Errorf("writing decrypted file: %w", err)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing encrypted file: %w", err)
	}

	fmt.Printf("Decrypted: %s -> %s\n", util.ShortenPath(path), util.ShortenPath(newPath))
	return nil
}

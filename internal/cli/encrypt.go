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

var encryptCmd = &cobra.Command{
	Use:   "encrypt <source>",
	Short: "Encrypt a managed file",
	Long: `Encrypt a file in place.

This reads the file, encrypts it using the configured age recipients,
writes it back, and adds the #encrypted suffix to the filename.

The file can be a managed source file or any file path (e.g. a var_file
in .matedata/). Paths are resolved relative to the current directory,
falling back to the source directory.

The age recipients must be configured in mate.yaml:

  age:
    recipients:
      - age1...

Examples:
  mate encrypt nvim/secrets.yaml
  mate encrypt .matedata/secrets.yaml`,
	Args:              cobra.ExactArgs(1),
	RunE:              runEncrypt,
	ValidArgsFunction: completeUnencryptedSourceFiles,
}

func init() {
	rootCmd.AddCommand(encryptCmd)
}

func runEncrypt(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Age == nil || len(cfg.Age.Recipients) == 0 {
		return fmt.Errorf("no age recipients configured in mate.yaml")
	}

	enc, err := encrypt.NewAgeEncryptor("", "", cfg.Age.Recipients)
	if err != nil {
		return fmt.Errorf("setting up encryption: %w", err)
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
		if entry.Attrs.Encrypted {
			return fmt.Errorf("file is already encrypted: %s", srcPattern)
		}
		return encryptFileAt(entry.SourcePath, entry.Mode.Perm(), enc)
	}

	// Fall back to resolving as a path relative to the source directory
	filePath := resolveFilePath(srcPattern, cfg.SourceDir())
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not found: %s", srcPattern)
	}
	if strings.HasSuffix(filePath, "#encrypted") {
		return fmt.Errorf("file is already encrypted: %s", srcPattern)
	}

	return encryptFileAt(filePath, info.Mode().Perm(), enc)
}

func resolveFilePath(pattern string, sourceDir string) string {
	if filepath.IsAbs(pattern) {
		return pattern
	}
	// Try relative to cwd first
	if abs, err := filepath.Abs(pattern); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	// Fall back to relative to source directory
	return filepath.Join(sourceDir, pattern)
}

func resolveEncryptedFilePath(pattern string, sourceDir string) string {
	if filepath.IsAbs(pattern) {
		if _, err := os.Stat(pattern); err == nil {
			return pattern
		}
		if !strings.HasSuffix(pattern, "#encrypted") {
			if _, err := os.Stat(pattern + "#encrypted"); err == nil {
				return pattern + "#encrypted"
			}
		}
		return ""
	}
	// Try relative to cwd (exact, then with #encrypted)
	if abs, err := filepath.Abs(pattern); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
		if !strings.HasSuffix(abs, "#encrypted") {
			if _, err := os.Stat(abs + "#encrypted"); err == nil {
				return abs + "#encrypted"
			}
		}
	}
	// Try relative to source directory (exact, then with #encrypted)
	p := filepath.Join(sourceDir, pattern)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if !strings.HasSuffix(p, "#encrypted") {
		if _, err := os.Stat(p + "#encrypted"); err == nil {
			return p + "#encrypted"
		}
	}
	return ""
}

func encryptFileAt(path string, perm os.FileMode, enc *encrypt.AgeEncryptor) error {
	plaintext, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}

	newPath := path + "#encrypted"

	if err := os.WriteFile(newPath, ciphertext, perm); err != nil {
		return fmt.Errorf("writing encrypted file: %w", err)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing original file: %w", err)
	}

	fmt.Printf("Encrypted: %s -> %s\n", util.ShortenPath(path), util.ShortenPath(newPath))
	return nil
}

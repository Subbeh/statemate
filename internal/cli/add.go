package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/source"
)

var addCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a file to source",
	Long: `Add an existing file to the source directory.

The file is copied from its current location to the appropriate source directory,
following stow-style conventions. The original file remains in place.

Examples:
  mate add ~/.config/nvim/init.lua
  mate add --profile work ~/.gitconfig
  mate add --encrypt ~/.ssh/config`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

var (
	addProfile  string
	addEncrypt  bool
	addSource   string
	addTemplate bool
)

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVar(&addProfile, "profile", "", "add file with profile suffix")
	addCmd.Flags().BoolVar(&addEncrypt, "encrypt", false, "encrypt file when adding")
	addCmd.Flags().StringVarP(&addSource, "source", "s", "", "target source directory")
	addCmd.Flags().BoolVar(&addTemplate, "template", false, "mark file as template")

	addCmd.RegisterFlagCompletionFunc("source", completeSources)
}

func runAdd(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profileName := profile.Detect(cfg)
	sources := profile.ResolveSources(cfg, profileName)
	absSources := cfg.ResolveSourcePaths(sources)
	if len(absSources) == 0 {
		return fmt.Errorf("no sources configured")
	}

	targetPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("file not found: %s", targetPath)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot add directories directly, add files individually")
	}

	scanner := source.NewScanner(cfg.TargetBase, cfg.SourceDir())
	tree, err := scanner.Scan(absSources)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	for _, entry := range tree.Files() {
		if entry.TargetPath == targetPath {
			return fmt.Errorf("file already managed: %s (in %s)", targetPath, filepath.Base(filepath.Dir(entry.SourcePath)))
		}
	}

	var sourceDir string
	sourceName := addSource
	if sourceName == "" {
		sourceName = cfg.DefaultSource
	}

	if sourceName != "" {
		if filepath.IsAbs(sourceName) {
			sourceDir = sourceName
		} else {
			sourceDir = filepath.Join(cfg.SourceDir(), sourceName)
		}
		found := false
		for _, s := range absSources {
			if s == sourceDir {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("source %q not in configured sources", sourceName)
		}
	} else {
		idx, err := promptSourceSelection(cfg.Sources)
		if err != nil {
			return err
		}
		sourceDir = absSources[idx]
	}

	relPath, err := computeRelativePath(targetPath, cfg.TargetBase)
	if err != nil {
		return fmt.Errorf("computing relative path: %w", err)
	}

	destName := filepath.Base(relPath)
	if addProfile != "" {
		destName = destName + "#profile:" + addProfile
	}
	if addEncrypt {
		destName = destName + "#encrypted"
	}
	if addTemplate {
		destName = destName + "#template"
	}

	destPath := filepath.Join(sourceDir, filepath.Dir(relPath), destName)

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	if addEncrypt {
		if cfg.Age == nil || len(cfg.Age.Recipients) == 0 {
			return fmt.Errorf("encryption requested but no age recipients configured")
		}

		enc, err := encrypt.NewAgeEncryptor("", "", cfg.Age.Recipients)
		if err != nil {
			return fmt.Errorf("creating encryptor: %w", err)
		}

		content, err = enc.Encrypt(content)
		if err != nil {
			return fmt.Errorf("encrypting: %w", err)
		}
	}

	if err := os.WriteFile(destPath, content, info.Mode()); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	fmt.Printf("Added %s -> %s\n", targetPath, destPath)
	return nil
}

func computeRelativePath(targetPath, targetBase string) (string, error) {
	targetBase = expandPath(targetBase)
	if targetBase == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		targetBase = home
	}

	absBase, err := filepath.Abs(targetBase)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(targetPath, absBase) {
		return "", fmt.Errorf("file %s is not under target base %s", targetPath, absBase)
	}

	rel, err := filepath.Rel(absBase, targetPath)
	if err != nil {
		return "", err
	}

	return rel, nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

func promptSourceSelection(sources []string) (int, error) {
	prompt := promptui.Select{
		Label: "Select source",
		Items: sources,
		Size:  10,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(sources[index]), strings.ToLower(input))
		},
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return 0, err
	}
	return idx, nil
}

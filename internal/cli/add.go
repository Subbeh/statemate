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
	"github.com/subbeh/statemate/internal/template"
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

	_ = addCmd.RegisterFlagCompletionFunc("source", completeSources)
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

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}
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

	tmplCtx, err := template.NewContext(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating template context: %w", err)
	}
	renderer := func(data []byte) ([]byte, error) {
		return template.Render(data, tmplCtx)
	}

	targetBase, err := resolveTargetBaseForAdd(sourceDir, targetPath, cfg.TargetBase, tree, renderer)
	if err != nil {
		return err
	}

	relPath, err := computeRelativePath(targetPath, targetBase)
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

func resolveTargetBaseForAdd(sourceDir, targetPath, globalTargetBase string, tree *source.Tree, renderer config.TemplateRenderer) (string, error) {
	dirCfg, _ := config.LoadDirConfigRaw(sourceDir, renderer)

	// Check if file is under global target base
	globalBase := expandPath(globalTargetBase)
	if globalBase == "" {
		home, _ := os.UserHomeDir()
		globalBase = home
	}
	fileUnderGlobal := strings.HasPrefix(targetPath, globalBase+string(filepath.Separator)) || targetPath == globalBase

	// Case 1: No .mate.yaml exists
	if dirCfg == nil {
		if fileUnderGlobal {
			return globalBase, nil
		}
		// File is outside global target base - need to create .mate.yaml with target_base
		return promptCreateDirConfig(sourceDir, targetPath)
	}

	// Case 2: .mate.yaml exists but no target_base set
	if dirCfg.TargetBase == "" {
		if fileUnderGlobal {
			return globalBase, nil
		}
		// Check if source already has files
		if sourceHasFiles(sourceDir, tree) {
			return "", fmt.Errorf("source %q has existing files under %s; cannot add file from %s", filepath.Base(sourceDir), globalBase, targetPath)
		}
		// Prompt to add target_base to existing .mate.yaml
		return promptAddTargetBase(sourceDir, targetPath)
	}

	// Case 3: .mate.yaml exists with target_base set
	configuredBase := expandPath(dirCfg.TargetBase)
	fileUnderConfigured := strings.HasPrefix(targetPath, configuredBase+string(filepath.Separator)) || targetPath == configuredBase

	if !fileUnderConfigured {
		return "", fmt.Errorf("file %s is not under source's target_base %s", targetPath, configuredBase)
	}

	return configuredBase, nil
}

func sourceHasFiles(sourceDir string, tree *source.Tree) bool {
	for _, entry := range tree.Files() {
		if strings.HasPrefix(entry.SourcePath, sourceDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func promptCreateDirConfig(sourceDir, targetPath string) (string, error) {
	targetBase := inferTargetBase(targetPath)

	prompt := promptui.Prompt{
		Label:     fmt.Sprintf("Create .mate.yaml with target_base: %s", targetBase),
		IsConfirm: true,
	}

	_, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrAbort {
			return "", fmt.Errorf("file %s is not under home directory; specify target_base in source's .mate.yaml", targetPath)
		}
		return "", err
	}

	if err := writeDirConfig(sourceDir, targetBase); err != nil {
		return "", fmt.Errorf("creating .mate.yaml: %w", err)
	}

	fmt.Printf("Created %s/.mate.yaml\n", sourceDir)
	return targetBase, nil
}

func promptAddTargetBase(sourceDir, targetPath string) (string, error) {
	targetBase := inferTargetBase(targetPath)

	prompt := promptui.Prompt{
		Label:     fmt.Sprintf("Add target_base: %s to .mate.yaml", targetBase),
		IsConfirm: true,
	}

	_, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrAbort {
			return "", fmt.Errorf("file %s is not under home directory; add target_base to source's .mate.yaml", targetPath)
		}
		return "", err
	}

	if err := addTargetBaseToDirConfig(sourceDir, targetBase); err != nil {
		return "", fmt.Errorf("updating .mate.yaml: %w", err)
	}

	fmt.Printf("Updated %s/.mate.yaml\n", sourceDir)
	return targetBase, nil
}

func inferTargetBase(targetPath string) string {
	// Walk up the path to find a reasonable base
	// Use the parent of the first path component after root
	dir := filepath.Dir(targetPath)
	for {
		parent := filepath.Dir(dir)
		if parent == "/" || parent == "." {
			return dir
		}
		dir = parent
	}
}

func writeDirConfig(sourceDir, targetBase string) error {
	configPath := filepath.Join(sourceDir, ".mate.yaml")
	content := fmt.Sprintf("target_base: %s\n", targetBase)
	return os.WriteFile(configPath, []byte(content), 0644)
}

func addTargetBaseToDirConfig(sourceDir, targetBase string) error {
	configPath := filepath.Join(sourceDir, ".mate.yaml")
	existing, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	content := fmt.Sprintf("target_base: %s\n%s", targetBase, string(existing))
	return os.WriteFile(configPath, []byte(content), 0644)
}

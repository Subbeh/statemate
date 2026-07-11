package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/scripts"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
)

func completeTrackedFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	db, err := state.Open("")
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	defer func() { _ = db.Close() }()

	files, err := db.ListFiles()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	seen := make(map[string]bool)
	for _, arg := range args {
		seen[arg] = true
	}

	cwd, _ := os.Getwd()

	var completions []string
	for _, f := range files {
		if seen[f.TargetPath] {
			continue
		}
		if rel := relativeTo(f.TargetPath, cwd); rel != "" {
			completions = append(completions, rel)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeManagedFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	cwd, _ := os.Getwd()

	var completions []string
	for _, e := range tree.Files() {
		if rel := cwdRelativeCompletion(e, cwd, sourcePaths); rel != "" {
			completions = append(completions, rel)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeSourceFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	cwd, _ := os.Getwd()
	extraFiles := resolveExtraFiles(cfg)

	var completions []string
	for _, e := range tree.Files() {
		if rel := cwdRelativeCompletion(e, cwd, sourcePaths); rel != "" {
			completions = append(completions, rel)
		}
	}
	for _, f := range extraFiles {
		if rel := relativeToDir(f, cwd, cfg.SourceDir()); rel != "" {
			completions = append(completions, rel)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeEncryptedSourceFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	cwd, _ := os.Getwd()
	extraFiles := resolveExtraFiles(cfg)

	var completions []string
	for _, e := range tree.Files() {
		if e.Attrs.Encrypted {
			if rel := cwdRelativeCompletion(e, cwd, sourcePaths); rel != "" {
				completions = append(completions, rel)
			}
		}
	}
	for _, f := range extraFiles {
		if strings.HasSuffix(f, "#encrypted") {
			if rel := relativeToDir(f, cwd, cfg.SourceDir()); rel != "" {
				completions = append(completions, rel)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeUnencryptedSourceFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	cwd, _ := os.Getwd()
	extraFiles := resolveExtraFiles(cfg)

	var completions []string
	for _, e := range tree.Files() {
		if !e.Attrs.Encrypted {
			if rel := cwdRelativeCompletion(e, cwd, sourcePaths); rel != "" {
				completions = append(completions, rel)
			}
		}
	}
	for _, f := range extraFiles {
		if !strings.HasSuffix(f, "#encrypted") {
			if rel := relativeToDir(f, cwd, cfg.SourceDir()); rel != "" {
				completions = append(completions, rel)
			}
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeSources(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	return sources, cobra.ShellCompDirectiveNoFileComp
}

func completeProfiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var completions []string
	for name := range cfg.Profiles {
		completions = append(completions, name)
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeScripts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), sourcePaths)
	allScripts, err := discoverer.Discover()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var completions []string
	for _, s := range allScripts {
		completions = append(completions, s.Name)
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeOrphanedFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	if profileName != "" {
		tree = tree.FilterByProfile(profile.InheritanceChain(cfg, profileName))
	}

	db, err := state.Open("")
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	defer func() { _ = db.Close() }()

	orphans, err := findOrphans(db, tree)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	cwd, _ := os.Getwd()

	seen := make(map[string]bool)
	for _, arg := range args {
		seen[arg] = true
	}

	var completions []string
	for _, o := range orphans {
		if seen[o] {
			continue
		}
		if rel := relativeTo(o, cwd); rel != "" {
			completions = append(completions, rel)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeSourceDirs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	return sources, cobra.ShellCompDirectiveNoFileComp
}

// resolveExtraFiles returns absolute paths of include and var_files from the config.
// These files live outside the source tree but can be edited/encrypted/decrypted.
func resolveExtraFiles(cfg *config.Config) []string {
	var files []string
	for _, f := range cfg.Include {
		files = append(files, cfg.ResolveRelPath(f))
	}
	for _, f := range cfg.VarFiles {
		files = append(files, cfg.ResolveRelPath(f))
	}
	return files
}

// cwdRelativeCompletion returns a relative path for the entry if cwd is within
// the same source directory or target directory as the entry. Returns "" if no match.
func cwdRelativeCompletion(e *source.Entry, cwd string, sourcePaths []string) string {
	// Check if cwd is within or equal to the entry's source directory
	for _, sp := range sourcePaths {
		if !strings.HasPrefix(e.SourcePath, sp+"/") {
			continue
		}
		if cwd == sp || strings.HasPrefix(cwd, sp+"/") {
			rel, err := filepath.Rel(cwd, e.SourcePath)
			if err == nil {
				return rel
			}
		}
	}

	// Check if the entry's target is under cwd
	if strings.HasPrefix(e.TargetPath, cwd+"/") {
		rel, err := filepath.Rel(cwd, e.TargetPath)
		if err == nil {
			return rel
		}
	}

	return ""
}

// relativeToDir returns a path relative to cwd if the file is under cwd or
// cwd is under sourceDir (so the file can be expressed relative to cwd without
// leaving the source tree). Returns "" if no meaningful relative path exists.
func relativeToDir(absPath, cwd, sourceDir string) string {
	// File is directly under cwd
	if strings.HasPrefix(absPath, cwd+"/") {
		rel, err := filepath.Rel(cwd, absPath)
		if err == nil {
			return rel
		}
	}
	// Cwd is under sourceDir (or is sourceDir), and so is the file
	if (cwd == sourceDir || strings.HasPrefix(cwd, sourceDir+"/")) &&
		strings.HasPrefix(absPath, sourceDir+"/") {
		rel, err := filepath.Rel(cwd, absPath)
		if err == nil {
			return rel
		}
	}
	return ""
}

// relativeTo returns path relative to base if path is under base, otherwise "".
func relativeTo(path, base string) string {
	if rel, ok := strings.CutPrefix(path, base+"/"); ok {
		return rel
	}
	return ""
}

func completeFilesInSourceDir(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveDefault
	}

	sourceDir := cfg.SourceDir()
	if sourceDir == "" {
		return nil, cobra.ShellCompDirectiveDefault
	}

	if toComplete == "" {
		toComplete = sourceDir
	} else if !filepath.IsAbs(toComplete) {
		toComplete = filepath.Join(sourceDir, toComplete)
	}

	dir := toComplete
	if info, err := os.Stat(toComplete); err != nil || !info.IsDir() {
		dir = filepath.Dir(toComplete)
	}

	if !strings.HasPrefix(dir, sourceDir) {
		return nil, cobra.ShellCompDirectiveDefault
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveDefault
	}

	var completions []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			path += "/"
		}
		completions = append(completions, path)
	}

	return completions, cobra.ShellCompDirectiveNoSpace
}

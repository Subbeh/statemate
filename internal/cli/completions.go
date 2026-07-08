package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/scripts"
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

	var completions []string
	for _, f := range files {
		if !seen[f.TargetPath] {
			completions = append(completions, f.TargetPath)
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
		// Include if current dir is under target path, source path, or vice versa
		if strings.HasPrefix(e.TargetPath, cwd+"/") ||
			strings.HasPrefix(cwd, filepath.Dir(e.TargetPath)) ||
			strings.HasPrefix(e.SourcePath, cwd+"/") ||
			strings.HasPrefix(cwd, filepath.Dir(e.SourcePath)) {
			completions = append(completions, e.TargetPath)
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

	var completions []string
	for _, e := range tree.Files() {
		completions = append(completions, e.SourcePath)
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

	var completions []string
	for _, e := range tree.Files() {
		if e.Attrs.Encrypted {
			completions = append(completions, e.SourcePath)
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

	var completions []string
	for _, e := range tree.Files() {
		if !e.Attrs.Encrypted {
			completions = append(completions, e.SourcePath)
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

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), cfg.AbsoluteSources())
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
		tree = tree.FilterByProfile(profileName)
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

	seen := make(map[string]bool)
	for _, arg := range args {
		seen[arg] = true
	}

	var completions []string
	for _, o := range orphans {
		if !seen[o] {
			completions = append(completions, o)
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

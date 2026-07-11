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
	"github.com/subbeh/statemate/internal/scripts"
	"github.com/subbeh/statemate/internal/secrets"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/target"
	"github.com/subbeh/statemate/internal/template"
	"github.com/subbeh/statemate/internal/util"
)

// Note: source import is still needed for source.Entry type

var statusCmd = &cobra.Command{
	Use:               "status [path]",
	Short:             "Show files that would change on apply",
	Args:              cobra.MaximumNArgs(1),
	RunE:              runStatus,
	ValidArgsFunction: completeManagedFiles,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("short", false, "compact output for statuslines (format: +N ~N !N ?N *N sN)")
}

func runStatus(cmd *cobra.Command, args []string) error {
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
		fmt.Fprintln(os.Stderr, "Warning: conflicting targets detected")
		for _, c := range tree.Conflicts {
			fmt.Fprintf(os.Stderr, "  %s defined in:\n", util.ShortenPath(c.TargetPath))
			for _, s := range c.Sources {
				fmt.Fprintf(os.Stderr, "    - %s\n", util.ShortenPath(s))
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	if profileName != "" {
		tree = tree.FilterByProfile(profile.InheritanceChain(cfg, profileName))
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	changes, err := target.ComputeChanges(tree, db)
	if err != nil {
		return fmt.Errorf("computing changes: %w", err)
	}

	orphans, err := findOrphans(db, tree)
	if err != nil {
		return fmt.Errorf("finding orphans: %w", err)
	}

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), sourcePaths)
	allScripts, err := discoverer.Discover()
	if err != nil {
		return fmt.Errorf("discovering scripts: %w", err)
	}

	profileChain := profile.InheritanceChain(cfg, profileName)
	pendingScripts, err := scripts.PendingScripts(allScripts.Automatic().ByProfile(profileChain), db)
	if err != nil {
		return fmt.Errorf("checking pending scripts: %w", err)
	}

	var pendingSecrets int
	{
		var enc *encrypt.AgeEncryptor
		identitySource := ""
		if cfg.Age != nil {
			enc, _ = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
			identitySource = cfg.Age.Identity
		}
		if mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache); err == nil {
			templateFiles := discoverTemplateFiles(cfg, sourcePaths)
			var decryptFn func([]byte) ([]byte, error)
			var ctxOpts []template.ContextOption
			if enc != nil && enc.CanDecrypt() {
				decryptFn = enc.Decrypt
				ctxOpts = append(ctxOpts, template.WithDecrypt(enc.Decrypt))
			}
			tmplCtx, _ := template.NewContext(cfg, profileName, ctxOpts...)
			items := secrets.DiscoverByRendering(templateFiles, tmplCtx, decryptFn)
			cached := mgr.ListCached()
			for _, item := range items {
				if cached == nil {
					pendingSecrets++
				} else if _, ok := cached[item.Key.String()]; !ok {
					pendingSecrets++
				}
			}
		}
	}

	var filterPath string
	if len(args) > 0 {
		filterPath = args[0]
	}

	var filteredChanges []*target.Change
	for _, c := range changes {
		if filterPath != "" && !matchesPath(c.Entry, filterPath, cfg.SourceDir()) {
			continue
		}
		filteredChanges = append(filteredChanges, c)
	}

	var filteredOrphans []string
	for _, o := range orphans {
		if filterPath != "" && !strings.Contains(o, filterPath) && !strings.HasSuffix(o, "/"+filterPath) {
			continue
		}
		filteredOrphans = append(filteredOrphans, o)
	}

	short, _ := cmd.Flags().GetBool("short")
	if short {
		return printShortStatus(filteredChanges, filteredOrphans, pendingScripts, pendingSecrets)
	}

	if len(filteredChanges) == 0 && len(filteredOrphans) == 0 && len(pendingScripts) == 0 && pendingSecrets == 0 {
		fmt.Println("Everything is up to date")
		return nil
	}

	if len(filteredChanges) > 0 {
		maxTargetLen := 0
		for _, c := range filteredChanges {
			targetDisplay := util.ShortenPath(c.Entry.TargetPath)
			if len(targetDisplay) > maxTargetLen {
				maxTargetLen = len(targetDisplay)
			}
		}

		fmt.Printf("  %-*s  %s\n", maxTargetLen, "TARGET", "SOURCE")
		for _, c := range filteredChanges {
			var prefix string
			switch c.Status {
			case target.StatusNew:
				prefix = "+"
			case target.StatusModified:
				prefix = "~"
			case target.StatusConflict:
				prefix = "!"
			}
			targetDisplay := util.ShortenPath(c.Entry.TargetPath)
			sourceDir := extractSourceDir(c.Entry)
			fmt.Printf("%s %-*s  %s\n", prefix, maxTargetLen, targetDisplay, sourceDir)
		}
	}

	if len(filteredOrphans) > 0 {
		fmt.Fprintln(os.Stderr, "\nWarning: orphaned files (no longer in source):")
		for _, o := range filteredOrphans {
			fmt.Fprintf(os.Stderr, "  - %s\n", util.ShortenPath(o))
		}
		fmt.Fprintln(os.Stderr, "Run 'mate forget <path>' to remove from tracking, or delete the target file.")
	}

	if len(pendingScripts) > 0 {
		fmt.Println("\nPending scripts:")
		for _, s := range pendingScripts {
			timing := "before"
			if s.Timing == scripts.TimingAfter {
				timing = "after"
			}
			fmt.Printf("  %s (%s, %s)\n", s.Name, s.Frequency, timing)
		}
	}

	if pendingSecrets > 0 {
		fmt.Printf("\nSecrets:\n  %d secrets need refresh (run 'mate secrets fetch')\n", pendingSecrets)
	}

	return nil
}

func printShortStatus(changes []*target.Change, orphans []string, pending scripts.Scripts, pendingSecrets int) error {
	var added, modified, conflicts int
	for _, c := range changes {
		switch c.Status {
		case target.StatusNew:
			added++
		case target.StatusModified:
			modified++
		case target.StatusConflict:
			conflicts++
		}
	}

	// No changes = no output (for statusline to hide)
	if added == 0 && modified == 0 && conflicts == 0 && len(orphans) == 0 && len(pending) == 0 && pendingSecrets == 0 {
		return nil
	}

	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("~%d", modified))
	}
	if conflicts > 0 {
		parts = append(parts, fmt.Sprintf("!%d", conflicts))
	}
	if len(orphans) > 0 {
		parts = append(parts, fmt.Sprintf("?%d", len(orphans)))
	}
	if len(pending) > 0 {
		parts = append(parts, fmt.Sprintf("*%d", len(pending)))
	}
	if pendingSecrets > 0 {
		parts = append(parts, fmt.Sprintf("s%d", pendingSecrets))
	}

	fmt.Println(strings.Join(parts, " "))
	return nil
}

func extractSourceDir(entry *source.Entry) string {
	srcDir := strings.TrimSuffix(entry.SourcePath, "/"+entry.RelPath)
	return filepath.Base(srcDir)
}

func matchesPath(entry *source.Entry, pattern, sourceDir string) bool {
	srcDir := strings.TrimSuffix(entry.SourcePath, "/"+entry.RelPath)
	relPath := filepath.Join(filepath.Base(srcDir), entry.RelPath)

	if relPath == pattern || entry.SourcePath == pattern || entry.TargetPath == pattern {
		return true
	}
	if strings.HasSuffix(relPath, "/"+pattern) ||
		strings.HasSuffix(entry.SourcePath, "/"+pattern) ||
		strings.HasSuffix(entry.TargetPath, "/"+pattern) {
		return true
	}
	if strings.HasPrefix(relPath, pattern+"/") ||
		strings.HasPrefix(entry.TargetPath, pattern+"/") {
		return true
	}
	return false
}

func findOrphans(db *state.DB, tree *source.Tree) ([]string, error) {
	managed, err := db.ListFiles()
	if err != nil {
		return nil, err
	}

	currentTargets := make(map[string]bool)
	for _, e := range tree.Files() {
		currentTargets[e.TargetPath] = true
	}

	var orphans []string
	for _, m := range managed {
		if !currentTargets[m.TargetPath] {
			if _, err := os.Stat(m.TargetPath); err == nil {
				orphans = append(orphans, m.TargetPath)
			}
		}
	}

	return orphans, nil
}

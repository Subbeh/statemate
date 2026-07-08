package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/util"
)

var managedCmd = &cobra.Command{
	Use:               "managed [path]",
	Short:             "List all managed files",
	Long:              "List all files in source directories that are managed by mate. Optionally filter by path.",
	Args:              cobra.MaximumNArgs(1),
	RunE:              runManaged,
	ValidArgsFunction: completeSourceDirs,
}

func init() {
	rootCmd.AddCommand(managedCmd)
}

func runManaged(cmd *cobra.Command, args []string) error {
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

	allSources := profile.AllSources(cfg)
	allSourcePaths := cfg.ResolveSourcePaths(allSources)

	activeSources := profile.ResolveSources(cfg, profileName)
	activeSourceSet := make(map[string]bool)
	for _, s := range activeSources {
		activeSourceSet[s] = true
	}

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}
	tree, err := scanner.Scan(allSourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	var filterPath string
	if len(args) > 0 {
		filterPath = args[0]
	}

	var data [][]string
	for _, e := range tree.Files() {
		srcDir := strings.TrimSuffix(e.SourcePath, "/"+e.RelPath)
		srcPath := filepath.Join(filepath.Base(srcDir), e.RelPath)

		if filterPath != "" && !matchesManagedFilter(e, srcPath, filterPath) {
			continue
		}

		active := isActiveForProfile(e, profileName, activeSourceSet, cfg.SourceDir())
		destPath := util.ShortenPath(e.TargetPath)
		attrs := formatAttrs(e.Attrs)

		status := ""
		if active {
			status = "*"
		}

		data = append(data, []string{destPath, srcPath, status, attrs})
	}

	table := tablewriter.NewTable(os.Stdout,
		tablewriter.WithHeader([]string{"TARGET", "SOURCE", "ACTIVE", "ATTRIBUTES"}),
		tablewriter.WithAlignment(tw.Alignment{tw.AlignLeft, tw.AlignLeft, tw.AlignCenter, tw.AlignLeft}),
		tablewriter.WithRendition(tw.Rendition{
			Borders: tw.BorderNone,
			Settings: tw.Settings{
				Separators: tw.SeparatorsNone,
				Lines:      tw.LinesNone,
			},
		}),
	)

	_ = table.Bulk(data)
	_ = table.Render()
	return nil
}

func matchesManagedFilter(e *source.Entry, srcPath, filter string) bool {
	// Match against source relative path
	if strings.HasPrefix(srcPath, filter) || strings.HasSuffix(srcPath, "/"+filter) {
		return true
	}
	// Match against target path (absolute or basename)
	if e.TargetPath == filter || strings.HasSuffix(e.TargetPath, "/"+filter) {
		return true
	}
	// Match against relative path within source
	if e.RelPath == filter || strings.HasPrefix(e.RelPath, filter+"/") || strings.HasSuffix(e.RelPath, "/"+filter) {
		return true
	}
	return false
}

func isActiveForProfile(e *source.Entry, profileName string, activeSources map[string]bool, sourceDir string) bool {
	if e.Attrs.Profile != "" && e.Attrs.Profile != profileName {
		return false
	}

	for src := range activeSources {
		fullPath := src
		if !filepath.IsAbs(src) {
			fullPath = filepath.Join(sourceDir, src)
		}
		if strings.HasPrefix(e.SourcePath, fullPath+"/") || e.SourcePath == fullPath {
			return true
		}
	}

	return false
}

func formatAttrs(a source.Attrs) string {
	var parts []string

	if a.Profile != "" {
		parts = append(parts, "profile:"+a.Profile)
	}
	if a.Perm != 0 {
		parts = append(parts, fmt.Sprintf("perm:%04o", a.Perm))
	}
	if a.Owner != "" {
		parts = append(parts, "owner:"+a.Owner)
	}
	if a.Group != "" {
		parts = append(parts, "group:"+a.Group)
	}
	if a.Encrypted {
		parts = append(parts, "encrypted")
	}
	if a.Template {
		parts = append(parts, "template")
	}
	if a.Symlink {
		parts = append(parts, "symlink")
	}

	return strings.Join(parts, ", ")
}

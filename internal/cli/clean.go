package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/util"
)

var cleanCmd = &cobra.Command{
	Use:   "clean [path...]",
	Short: "Remove orphaned files",
	Long: `Remove orphaned files that are no longer in the source.

Orphans are files that were previously managed but are no longer defined
in any source directory. By default, this command prompts for confirmation
before each deletion.

Flags:
  --force   Skip confirmation prompts
  --all     Remove all orphans (otherwise specify paths)

Example:
  mate clean                              # list orphans
  mate clean ~/.config/old/file.conf      # remove specific orphan
  mate clean --all                        # remove all orphans (with prompts)
  mate clean --all --force                # remove all orphans (no prompts)`,
	RunE:              runClean,
	ValidArgsFunction: completeOrphanedFiles,
}

func init() {
	cleanCmd.Flags().Bool("force", false, "skip confirmation prompts")
	cleanCmd.Flags().Bool("all", false, "remove all orphans")
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
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

	if profileName != "" {
		tree = tree.FilterByProfile(profileName)
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	orphans, err := findOrphans(db, tree)
	if err != nil {
		return fmt.Errorf("finding orphans: %w", err)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphaned files")
		return nil
	}

	force, _ := cmd.Flags().GetBool("force")
	all, _ := cmd.Flags().GetBool("all")

	if len(args) == 0 && !all {
		fmt.Println("Orphaned files:")
		for _, o := range orphans {
			fmt.Printf("  %s\n", util.ShortenPath(o))
		}
		fmt.Println("\nUse 'mate clean <path>' to remove specific files, or 'mate clean --all' to remove all.")
		return nil
	}

	var toRemove []string
	if all {
		toRemove = orphans
	} else {
		orphanSet := make(map[string]bool)
		for _, o := range orphans {
			orphanSet[o] = true
		}
		for _, arg := range args {
			path := expandPath(arg)
			if !orphanSet[path] {
				return fmt.Errorf("not an orphan: %s", path)
			}
			toRemove = append(toRemove, path)
		}
	}

	reader := bufio.NewReader(os.Stdin)
	for _, path := range toRemove {
		if !force {
			fmt.Printf("Remove %s? [y/N] ", util.ShortenPath(path))
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("  Skipped")
				continue
			}
		}

		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}

		if err := db.DeleteFile(path); err != nil {
			return fmt.Errorf("removing %s from database: %w", path, err)
		}

		fmt.Printf("Removed %s\n", util.ShortenPath(path))
	}

	return nil
}

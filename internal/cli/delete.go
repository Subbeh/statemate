package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/util"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <path>",
	Short: "Delete file from source and target",
	Long: `Delete a file from both source and target.

This deletes the source file and optionally the target file,
and removes the tracking entry from the database.

Example:
  mate delete ~/.config/nvim/init.lua`,
	Args:              cobra.ExactArgs(1),
	RunE:              runDelete,
	ValidArgsFunction: completeManagedFiles,
}

var (
	deleteForce      bool
	deleteKeepTarget bool
)

func init() {
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "don't prompt for confirmation")
	deleteCmd.Flags().BoolVar(&deleteKeepTarget, "keep-target", false, "keep the target file, only delete source")
}

func runDelete(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	targetPath := expandPath(args[0])
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	var entry *source.Entry
	for _, e := range tree.Files() {
		if e.TargetPath == absTarget {
			entry = e
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("file not found in sources: %s", targetPath)
	}

	if !deleteForce {
		fmt.Printf("Will remove:\n")
		fmt.Printf("  Source: %s\n", util.ShortenPath(entry.SourcePath))
		if !deleteKeepTarget {
			fmt.Printf("  Target: %s\n", util.ShortenPath(entry.TargetPath))
		}
		fmt.Print("Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Println("Aborted")
			return nil
		}
	}

	if err := os.Remove(entry.SourcePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing source: %w", err)
	}
	fmt.Printf("Removed source: %s\n", util.ShortenPath(entry.SourcePath))

	if !deleteKeepTarget {
		if err := os.Remove(entry.TargetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing target: %w", err)
		}
		fmt.Printf("Removed target: %s\n", util.ShortenPath(entry.TargetPath))
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.DeleteFile(entry.TargetPath); err != nil {
		return fmt.Errorf("removing from database: %w", err)
	}

	return nil
}

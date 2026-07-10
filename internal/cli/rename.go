package cli

import (
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

var renameCmd = &cobra.Command{
	Use:   "rename <source> <new-name>",
	Short: "Rename a managed file",
	Long: `Rename a managed file in both source and target.

This renames the source file, the target file, and updates tracking.

Examples:
  mate rename nvim/init.lua init.vim
  mate rename zsh/.zshrc .zshrc.bak`,
	Args:              cobra.ExactArgs(2),
	RunE:              runRename,
	ValidArgsFunction: completeSourceFiles,
}

func init() {
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	allSources := profile.AllSources(cfg)
	allSourcePaths := cfg.ResolveSourcePaths(allSources)

	scanner := source.NewScannerWithIgnore(cfg.TargetBase, cfg.SourceDir(), nil, cfg.Ignore)
	tree, err := scanner.Scan(allSourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	srcPattern := args[0]
	newName := args[1]

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

	if entry == nil {
		return fmt.Errorf("file not found: %s", srcPattern)
	}

	newSourcePath := filepath.Join(filepath.Dir(entry.SourcePath), newName)

	if _, err := os.Stat(newSourcePath); err == nil {
		return fmt.Errorf("target already exists: %s", newSourcePath)
	}

	if err := os.Rename(entry.SourcePath, newSourcePath); err != nil {
		return fmt.Errorf("renaming file: %w", err)
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	newTargetPath := filepath.Join(filepath.Dir(entry.TargetPath), newName)

	if _, err := os.Stat(newTargetPath); err == nil {
		return fmt.Errorf("target already exists: %s", util.ShortenPath(newTargetPath))
	}

	if _, err := os.Stat(entry.TargetPath); err == nil {
		if err := os.Rename(entry.TargetPath, newTargetPath); err != nil {
			return fmt.Errorf("renaming target: %w", err)
		}
	}

	existing, _ := db.GetFile(entry.TargetPath)
	if existing != nil {
		if err := db.DeleteFile(entry.TargetPath); err != nil {
			return fmt.Errorf("updating database: %w", err)
		}

		targetHash, err := state.HashFile(newTargetPath)
		if err != nil {
			return fmt.Errorf("hashing new target: %w", err)
		}

		sourceHash, err := state.HashFile(newSourcePath)
		if err != nil {
			return fmt.Errorf("hashing new source: %w", err)
		}

		if err := db.SaveFile(&state.FileEntry{
			SourcePath:  newSourcePath,
			TargetPath:  newTargetPath,
			SourceHash:  sourceHash,
			AppliedHash: targetHash,
			Mode:        existing.Mode,
		}); err != nil {
			return fmt.Errorf("saving new tracking entry: %w", err)
		}
	}

	fmt.Printf("Renamed:\n")
	fmt.Printf("  source: %s -> %s\n", util.ShortenPath(entry.SourcePath), util.ShortenPath(newSourcePath))
	fmt.Printf("  target: %s -> %s\n", util.ShortenPath(entry.TargetPath), util.ShortenPath(newTargetPath))

	return nil
}

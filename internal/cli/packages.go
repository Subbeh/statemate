package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/packages"
	"github.com/subbeh/statemate/internal/profile"
)

var packagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "Manage packages",
	Long:  "Declarative package management across package managers",
}

var packagesStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show package sync status",
	Long: `Show package sync status across configured package managers.

Packages can be defined in:
  - mate.yaml (global packages)
  - mate.yaml profiles.<name>.packages (profile-specific)
  - <source>/.mate.yaml packages (source-level)
  - Files referenced via 'include' field

Use --all to show extra packages not in config.
Use --verbose to show package descriptions.`,
	RunE: runPackagesStatus,
}

var packagesApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Sync packages",
	Long:  "Install missing packages. Use --prune to also remove packages not in config.",
	RunE:  runPackagesApply,
}

var (
	packagesAutoConfirm bool
	packagesShowAll     bool
	packagesVerbose     bool
	packagesPrune       bool
)

func init() {
	rootCmd.AddCommand(packagesCmd)
	packagesCmd.AddCommand(packagesStatusCmd)
	packagesCmd.AddCommand(packagesApplyCmd)

	packagesStatusCmd.Flags().BoolVar(&packagesShowAll, "all", false, "also show extra packages not in config")
	packagesStatusCmd.Flags().BoolVarP(&packagesVerbose, "verbose", "v", false, "show package descriptions")
	packagesApplyCmd.Flags().BoolVarP(&packagesAutoConfirm, "yes", "y", false, "auto-confirm all changes")
	packagesApplyCmd.Flags().BoolVar(&packagesPrune, "prune", false, "remove packages not in config (dangerous)")
}

func runPackagesStatus(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	results, err := packages.ComputeSync(cfg, profileName, cfg.ResolveSourcePaths(sources), packages.WithVerbose(packagesVerbose))
	if err != nil {
		return fmt.Errorf("computing sync: %w", err)
	}

	if len(results) == 0 {
		managers := packages.GetAvailableManagers()
		if len(managers) == 0 {
			fmt.Println("No package managers available")
		} else {
			fmt.Println("No packages configured")
		}
		return nil
	}

	var data [][]string
	for _, result := range results {
		for _, s := range result.Statuses {
			if s.Status == packages.StatusExtra && !packagesShowAll {
				continue
			}

			var indicator string
			switch s.Status {
			case packages.StatusInstalled:
				indicator = color.GreenString("✓")
			case packages.StatusMissing:
				indicator = color.RedString("+")
			case packages.StatusExtra:
				indicator = color.YellowString("-")
			case packages.StatusVersionMismatch:
				indicator = color.YellowString("~")
			}

			source := strings.Join(s.Sources, ", ")
			row := []string{indicator, s.Name, result.Manager, source}
			if packagesVerbose {
				row = append(row, s.Description)
			}
			data = append(data, row)
		}
	}

	header := []string{"", "PACKAGE", "MANAGER", "SOURCE"}
	alignment := tw.Alignment{tw.AlignCenter, tw.AlignLeft, tw.AlignLeft, tw.AlignLeft}
	if packagesVerbose {
		header = append(header, "DESCRIPTION")
		alignment = append(alignment, tw.AlignLeft)
	}

	var buf bytes.Buffer
	table := tablewriter.NewTable(&buf,
		tablewriter.WithHeader(header),
		tablewriter.WithAlignment(alignment),
		tablewriter.WithRendition(tw.Rendition{
			Borders: tw.BorderNone,
			Settings: tw.Settings{
				Separators: tw.SeparatorsNone,
				Lines:      tw.LinesNone,
			},
		}),
	)
	table.Bulk(data)
	table.Render()

	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		fmt.Println(strings.TrimRight(scanner.Text(), " "))
	}

	for _, result := range results {
		extra := result.Extra()
		if !packagesShowAll && len(extra) > 0 {
			fmt.Printf("(%d extra %s packages not in config, use --all to show)\n", len(extra), result.Manager)
		}
	}

	return nil
}

func runPackagesApply(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	results, err := packages.ComputeSync(cfg, profileName, cfg.ResolveSourcePaths(sources))
	if err != nil {
		return fmt.Errorf("computing sync: %w", err)
	}

	if len(results) == 0 {
		managers := packages.GetAvailableManagers()
		if len(managers) == 0 {
			fmt.Println("No package managers available")
		} else {
			fmt.Println("No packages configured")
		}
		return nil
	}

	anyChanges := false
	for _, result := range results {
		if len(result.Missing()) > 0 || (packagesPrune && len(result.Extra()) > 0) {
			anyChanges = true
			break
		}
	}

	if !anyChanges {
		fmt.Println("All packages are in sync")
		return nil
	}

	for _, result := range results {
		missing := result.Missing()
		extra := result.Extra()
		hasMissing := len(missing) > 0
		hasExtra := packagesPrune && len(extra) > 0

		if !hasMissing && !hasExtra {
			continue
		}

		manager, err := packages.GetManager(result.Manager, cfg.AURHelper)
		if err != nil {
			return err
		}

		fmt.Printf("\n=== %s ===\n", result.Manager)

		if hasMissing {
			fmt.Printf("Will install: %s\n", strings.Join(missing, ", "))
		}

		if hasExtra {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("%s Will remove: %s\n", yellow("WARNING:"), strings.Join(extra, ", "))
		}

		if !packagesAutoConfirm {
			prompt := "Continue? [y/N] "
			if hasExtra {
				prompt = "This will REMOVE packages. Continue? [y/N] "
			}
			fmt.Print(prompt)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			input = strings.TrimSpace(strings.ToLower(input))
			if input != "y" && input != "yes" {
				fmt.Println("Skipped")
				continue
			}
		}

		if hasMissing {
			fmt.Printf("Installing %d packages...\n", len(missing))
			if err := manager.Install(missing); err != nil {
				return fmt.Errorf("installing packages: %w", err)
			}
		}

		if hasExtra {
			fmt.Printf("Removing %d packages...\n", len(extra))
			if err := manager.Uninstall(extra); err != nil {
				return fmt.Errorf("removing packages: %w", err)
			}
		}

		fmt.Println("Done")
	}

	return nil
}

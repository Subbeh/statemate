package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/scripts"
	"github.com/subbeh/statemate/internal/state"
	"github.com/subbeh/statemate/internal/template"
)

var scriptsCmd = &cobra.Command{
	Use:   "scripts",
	Short: "Manage scripts",
	Long:  "List and manage lifecycle scripts",
}

var scriptsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all scripts",
	Long:  "List all discovered scripts and their status",
	RunE:  runScriptsList,
}

var runCmd = &cobra.Command{
	Use:               "run <script>",
	Short:             "Run a script",
	Long:              "Manually run a script by name or path",
	Args:              cobra.ExactArgs(1),
	RunE:              runScript,
	ValidArgsFunction: completeScripts,
}

func init() {
	rootCmd.AddCommand(scriptsCmd)
	rootCmd.AddCommand(runCmd)
	scriptsCmd.AddCommand(scriptsListCmd)

	runCmd.Flags().Bool("dry-run", false, "show what would be done without running")
	runCmd.Flags().BoolP("verbose", "v", false, "verbose output")
}

func runScriptsList(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), cfg.AbsoluteSources())
	allScripts, err := discoverer.Discover()
	if err != nil {
		return fmt.Errorf("discovering scripts: %w", err)
	}

	if len(allScripts) == 0 {
		fmt.Println("No scripts found")
		return nil
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	fmt.Printf("%-10s %-8s %-6s %-30s %s\n", "FREQUENCY", "TIMING", "ORDER", "NAME", "PATH")
	fmt.Println(strings.Repeat("-", 90))

	for _, script := range allScripts {
		status := ""
		switch script.Frequency {
		case scripts.FreqOnce:
			hasRun, _ := db.HasScriptRun(script.Path)
			if hasRun {
				status = " (ran)"
			}
		case scripts.FreqOnchange:
			hasRunWithHash, _ := db.HasScriptRunWithHash(script.Path, script.ContentHash)
			if hasRunWithHash {
				status = " (unchanged)"
			} else {
				prevRun, _ := db.GetScriptRun(script.Path)
				if prevRun != nil {
					status = " (changed)"
				}
			}
		}

		relPath, _ := filepath.Rel(cfg.SourceDir(), script.Path)
		if relPath == "" || strings.HasPrefix(relPath, "..") {
			relPath = script.Path
		}

		flags := ""
		if script.Template {
			flags += " [T]"
		}
		if script.Profile != "" {
			flags += " [" + script.Profile + "]"
		}

		fmt.Printf("%-10s %-8s %-6d %-30s %s%s%s\n",
			script.Frequency,
			script.Timing,
			script.Order,
			script.Name,
			relPath,
			flags,
			status,
		)
	}

	return nil
}

func runScript(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), cfg.AbsoluteSources())
	allScripts, err := discoverer.Discover()
	if err != nil {
		return fmt.Errorf("discovering scripts: %w", err)
	}

	scriptArg := args[0]
	var script *scripts.Script

	for _, s := range allScripts {
		if s.Name == scriptArg || s.Path == scriptArg || filepath.Base(s.Path) == scriptArg {
			script = s
			break
		}
	}

	if script == nil {
		if info, err := os.Stat(scriptArg); err == nil && !info.IsDir() {
			contentHash, err := state.HashFile(scriptArg)
			if err != nil {
				return fmt.Errorf("hashing script: %w", err)
			}
			name, freq, timing, tmpl, prof, order := scripts.ParseScriptName(filepath.Base(scriptArg))
			script = &scripts.Script{
				Path:        scriptArg,
				Name:        name,
				Frequency:   freq,
				Timing:      timing,
				Template:    tmpl,
				Profile:     prof,
				Order:       order,
				ContentHash: contentHash,
			}
		}
	}

	if script == nil {
		return fmt.Errorf("script not found: %s", scriptArg)
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	var tmplCtx *template.Context
	if script.Template {
		tmplCtx, err = template.NewContext(cfg, profileName)
		if err != nil {
			return fmt.Errorf("creating template context: %w", err)
		}
	}

	executor := scripts.NewExecutor(db, tmplCtx, dryRun, verbose)
	if err := executor.ExecuteOne(script); err != nil {
		return fmt.Errorf("running script: %w", err)
	}

	return nil
}

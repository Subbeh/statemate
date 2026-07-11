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

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), sourcePaths)
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

	profileChain := profile.InheritanceChain(cfg, profileName)

	fmt.Printf(" %-10s %-8s %-6s %-10s %-30s %s\n", "FREQUENCY", "TIMING", "ORDER", "SOURCE", "NAME", "STATUS")
	fmt.Println(strings.Repeat("-", 90))

	for _, script := range allScripts {
		active := script.Profile == "" || matchesChain(script.Profile, profileChain)

		marker := " "
		if !active {
			marker = "-"
		}

		var status string
		if !active {
			status = "n/a"
		} else {
			lastRun, _ := db.GetScriptRun(script.Path)
			switch script.Frequency {
			case scripts.FreqOnce:
				if lastRun != nil {
					status = "done (" + lastRun.RunAt.Format("2006-01-02 15:04") + ")"
				} else {
					status = "pending"
				}
			case scripts.FreqOnchange:
				if lastRun != nil {
					hasRunWithHash, _ := db.HasScriptRunWithHash(script.Path, script.ContentHash)
					if hasRunWithHash {
						status = "unchanged (" + lastRun.RunAt.Format("2006-01-02 15:04") + ")"
					} else {
						status = "changed (" + lastRun.RunAt.Format("2006-01-02 15:04") + ")"
					}
				} else {
					status = "pending"
				}
			default:
				if lastRun != nil {
					status = "ran " + lastRun.RunAt.Format("2006-01-02 15:04")
				}
			}
		}

		name := script.Name
		if script.Profile != "" {
			name += " [" + script.Profile + "]"
		}
		if script.Template {
			name += " [T]"
		}

		source := ""
		if script.SourceDir != "" {
			source = filepath.Base(script.SourceDir)
		}

		fmt.Printf("%s %-10s %-8s %-6d %-10s %-30s %s\n",
			marker,
			script.Frequency,
			script.Timing,
			script.Order,
			source,
			name,
			status,
		)
	}

	return nil
}

func matchesChain(target string, chain []string) bool {
	for _, p := range chain {
		if p == target {
			return true
		}
	}
	return false
}

func runScript(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
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

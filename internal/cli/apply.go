package cli

import (
	"fmt"
	"os"
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

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration to target",
	Long:  "Apply files from source directories to their targets",
	RunE:  runApply,
}

var (
	dryRun  bool
	force   bool
	verbose int
)

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be done without making changes")
	applyCmd.Flags().BoolVar(&force, "force", false, "overwrite modified targets without prompting")
	applyCmd.Flags().CountVarP(&verbose, "verbose", "V", "increase verbosity (can be repeated)")
}

func runApply(cmd *cobra.Command, args []string) error {
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

	scanner := source.NewScanner(cfg.TargetBase, cfg.SourceDir())
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return fmt.Errorf("scanning sources: %w", err)
	}

	if tree.HasConflicts() {
		fmt.Fprintln(os.Stderr, "Error: conflicting targets detected")
		for _, c := range tree.Conflicts {
			fmt.Fprintf(os.Stderr, "  %s defined in:\n", util.ShortenPath(c.TargetPath))
			for _, s := range c.Sources {
				fmt.Fprintf(os.Stderr, "    - %s\n", util.ShortenPath(s))
			}
		}
		return fmt.Errorf("resolve conflicts before applying")
	}

	if profileName != "" {
		tree = tree.FilterByProfile(profileName)
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer db.Close()

	tmplCtx, err := template.NewContext(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating template context: %w", err)
	}

	var enc *encrypt.AgeEncryptor
	if cfg.Age != nil {
		enc, err = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
		if err != nil {
			return fmt.Errorf("setting up encryption: %w", err)
		}
	}

	{
		identitySource := ""
		if cfg.Age != nil {
			identitySource = cfg.Age.Identity
		}
		mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
		if err == nil {
			tmplCtx.SecretLookup = func(item, typ, field string) (string, error) {
				key := secrets.CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
				return mgr.Get(key)
			}
		}
	}

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), cfg.AbsoluteSources())
	allScripts, err := discoverer.Discover()
	if err != nil {
		return fmt.Errorf("discovering scripts: %w", err)
	}

	executor := scripts.NewExecutor(db, tmplCtx, dryRun, verbose > 0)

	beforeScripts := allScripts.Automatic().ByTiming(scripts.TimingBefore)
	beforeScripts.Sort()

	if len(beforeScripts) > 0 {
		if verbose > 0 || dryRun {
			fmt.Println("Running before scripts...")
		}
		if _, err := executor.Execute(beforeScripts); err != nil {
			return err
		}

		// Reload config and template context after before scripts
		// (scripts may generate var_files like secrets)
		if !dryRun {
			cfg, err = config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("reloading config: %w", err)
			}
			tmplCtx, err = template.NewContext(cfg, profileName)
			if err != nil {
				return fmt.Errorf("reloading template context: %w", err)
			}
		}
	}

	applier := target.NewApplier(db, tmplCtx, enc, dryRun, force, verbose)
	result, err := applier.Apply(tree)
	if err != nil {
		return err
	}

	afterScripts := allScripts.Automatic().ByTiming(scripts.TimingAfter)
	afterScripts.Sort()

	if len(afterScripts) > 0 {
		if verbose > 0 || dryRun {
			fmt.Println("Running after scripts...")
		}
		if _, err := executor.Execute(afterScripts); err != nil {
			return err
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d files would be applied, %d unchanged\n", result.Applied, result.Skipped)
	} else {
		parts := []string{}
		if result.Applied > 0 {
			parts = append(parts, fmt.Sprintf("applied %d", result.Applied))
		}
		if result.Imported > 0 {
			parts = append(parts, fmt.Sprintf("imported %d", result.Imported))
		}
		if result.Skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d unchanged", result.Skipped))
		}
		if len(parts) == 0 {
			fmt.Println("Nothing to do")
		} else {
			fmt.Printf("%s\n", strings.Join(parts, ", "))
		}
	}

	return nil
}

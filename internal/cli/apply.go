package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/packages"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/scripts"
	"github.com/subbeh/statemate/internal/secrets"
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

	scanner, err := newScanner(cfg, profileName)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}
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

	profileChain := profile.InheritanceChain(cfg, profileName)
	if profileName != "" {
		tree = tree.FilterByProfile(profileChain)
	}

	db, err := state.Open("")
	if err != nil {
		return fmt.Errorf("opening state database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var enc *encrypt.AgeEncryptor
	if cfg.Age != nil {
		enc, err = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
		if err != nil {
			return fmt.Errorf("setting up encryption: %w", err)
		}
	}

	var ctxOpts []template.ContextOption
	if enc != nil && enc.CanDecrypt() {
		ctxOpts = append(ctxOpts, template.WithDecrypt(enc.Decrypt))
	}

	tmplCtx, err := template.NewContext(cfg, profileName, ctxOpts...)
	if err != nil {
		return fmt.Errorf("creating template context: %w", err)
	}

	identitySource := ""
	if cfg.Age != nil {
		identitySource = cfg.Age.Identity
	}
	mgr, mgrErr := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
	if mgrErr == nil {
		tmplCtx.SecretLookup = func(item, typ, field string) (string, error) {
			key := secrets.CacheKey{Provider: "bitwarden", Item: item, Type: typ, Field: field}
			return mgr.Get(key)
		}

		if err := fetchMissingSecrets(cfg, mgr, enc, profileName, sourcePaths, dryRun, verbose); err != nil {
			return err
		}
	}

	discoverer := scripts.NewDiscoverer(cfg.SourceDir(), sourcePaths)
	allScripts, err := discoverer.Discover()
	if err != nil {
		return fmt.Errorf("discovering scripts: %w", err)
	}

	executor := scripts.NewExecutor(db, tmplCtx, dryRun, verbose > 0)

	beforeScripts := allScripts.Automatic().ByProfile(profileChain).ByTiming(scripts.TimingBefore)
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
			tmplCtx, err = template.NewContext(cfg, profileName, ctxOpts...)
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

	if err := promptMissingPackages(cfg, profileName, sourcePaths, dryRun, force); err != nil {
		return err
	}

	afterScripts := allScripts.Automatic().ByProfile(profileChain).ByTiming(scripts.TimingAfter)
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

func fetchMissingSecrets(cfg *config.Config, mgr *secrets.Manager, enc *encrypt.AgeEncryptor, profileName string, sourcePaths []string, dryRun bool, verbose int) error {
	templateFiles := discoverTemplateFiles(cfg, sourcePaths)
	if len(templateFiles) == 0 {
		return nil
	}

	var decryptFn func([]byte) ([]byte, error)
	var ctxOpts []template.ContextOption
	if enc != nil && enc.CanDecrypt() {
		decryptFn = enc.Decrypt
		ctxOpts = append(ctxOpts, template.WithDecrypt(enc.Decrypt))
	}

	tmplCtx, err := template.NewContext(cfg, profileName, ctxOpts...)
	if err != nil {
		return nil
	}

	items := secrets.DiscoverByRendering(templateFiles, tmplCtx, decryptFn)
	if len(items) == 0 {
		return nil
	}

	cached := mgr.ListCached()
	var missing []secrets.FetchItem
	for _, item := range items {
		if cached == nil {
			missing = append(missing, item)
		} else if _, ok := cached[item.Key.String()]; !ok {
			missing = append(missing, item)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	if dryRun {
		fmt.Printf("Would fetch %d missing secrets\n", len(missing))
		return nil
	}

	fmt.Printf("Fetching %d missing secrets...\n", len(missing))
	result, err := mgr.Fetch(missing)
	if err != nil {
		return fmt.Errorf("fetching secrets: %w", err)
	}

	if verbose > 0 {
		fmt.Printf("Fetched %d secrets (%d changed, %d unchanged)\n",
			result.Total, result.Changed, result.Unchanged)
	}

	return nil
}

func promptMissingPackages(cfg *config.Config, profileName string, sourcePaths []string, dryRun bool, autoConfirm bool) error {
	results, err := packages.ComputeSync(cfg, profileName, sourcePaths)
	if err != nil {
		return nil
	}

	for _, result := range results {
		missing := result.Missing()
		if len(missing) == 0 {
			continue
		}

		manager, err := packages.GetManager(result.Manager, cfg.AURHelper)
		if err != nil {
			continue
		}

		if dryRun {
			fmt.Printf("\nMissing %s packages: %s\n", result.Manager, strings.Join(missing, ", "))
			continue
		}

		fmt.Printf("\nMissing %s packages: %s\n", result.Manager, strings.Join(missing, ", "))
		if !autoConfirm {
			fmt.Print("Install? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return nil
			}
			input = strings.TrimSpace(strings.ToLower(input))
			if input != "y" && input != "yes" {
				continue
			}
		}

		if err := manager.Install(missing); err != nil {
			return fmt.Errorf("installing packages: %w", err)
		}
	}

	return nil
}

package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/encrypt"
	"github.com/subbeh/statemate/internal/profile"
	"github.com/subbeh/statemate/internal/secrets"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage secrets",
	Long:  "Fetch, list, and inspect secrets from configured providers",
}

var secretsFetchCmd = &cobra.Command{
	Use:   "fetch [pattern]",
	Short: "Fetch secrets from providers",
	Long:  "Fetch secrets from configured providers and update the encrypted cache. Optionally filter by path pattern (e.g., 'ssh.*')",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSecretsFetch,
}

var secretsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List declared secrets and cache status",
	RunE:  runSecretsList,
}

var secretsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show secrets that need attention",
	RunE:  runSecretsStatus,
}

func init() {
	rootCmd.AddCommand(secretsCmd)
	secretsCmd.AddCommand(secretsFetchCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsStatusCmd)
}

func runSecretsFetch(cmd *cobra.Command, args []string) error {
	mgr, err := newSecretsManager(cmd)
	if err != nil {
		return err
	}

	var pattern string
	if len(args) > 0 {
		pattern = args[0]
	}

	items, _ := mgr.ListItems()
	total := len(items)
	if pattern != "" {
		count := 0
		for _, item := range items {
			if matchSecretsPattern(item.Path, pattern) {
				count++
			}
		}
		total = count
	}

	fmt.Printf("Fetching %d secrets...\n", total)

	green := color.New(color.FgGreen).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	mgr.SetProgress(func(path string, changed bool) {
		if changed {
			fmt.Printf("  %s %s\n", green("✓"), path)
		} else {
			fmt.Printf("  %s %s\n", dim("·"), path)
		}
	})

	result, err := mgr.Fetch(pattern)
	if err != nil {
		// Prompt to continue with cache on failure
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Print("Continue with cached secrets? [y/n]: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			return err
		}
		return nil
	}

	fmt.Printf("Fetched %d secrets (%d changed, %d unchanged)\n",
		result.Total, result.Changed, result.Unchanged)
	return nil
}

func runSecretsList(cmd *cobra.Command, args []string) error {
	mgr, err := newSecretsManager(cmd)
	if err != nil {
		return err
	}

	entries, err := mgr.ListItems()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No secrets configured")
		return nil
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	maxPath := len("PATH")
	maxFetched := len("LAST FETCHED")
	for _, e := range entries {
		if len(e.Path) > maxPath {
			maxPath = len(e.Path)
		}
	}

	fmt.Printf("%-*s  %-*s  %s\n", maxPath, "PATH", maxFetched, "LAST FETCHED", "STATUS")
	for _, e := range entries {
		fetched := "-"
		if !e.FetchedAt.IsZero() {
			fetched = e.FetchedAt.Format("2006-01-02 15:04")
		}

		var status string
		switch e.Status {
		case secrets.StatusCached:
			status = green("cached")
		case secrets.StatusMissing:
			status = yellow("missing")
		case secrets.StatusNew:
			status = cyan("new")
		}

		fmt.Printf("%-*s  %-*s  %s\n", maxPath, e.Path, maxFetched, fetched, status)
	}

	return nil
}

func runSecretsStatus(cmd *cobra.Command, args []string) error {
	mgr, err := newSecretsManager(cmd)
	if err != nil {
		return err
	}

	entries, err := mgr.ListItems()
	if err != nil {
		return err
	}

	var pending []*secrets.ListEntry
	for _, e := range entries {
		if e.Status != secrets.StatusCached {
			pending = append(pending, e)
		}
	}

	if len(pending) == 0 {
		fmt.Println("All secrets are cached")
		return nil
	}

	fmt.Println("Secrets needing refresh:")
	for _, e := range pending {
		reason := "never fetched"
		if e.Status == secrets.StatusNew {
			reason = "new declaration"
		}
		fmt.Printf("  %s (%s)\n", e.Path, reason)
	}

	return nil
}

func newSecretsManager(cmd *cobra.Command) (*secrets.Manager, error) {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	secretsCfg := resolveSecretsWithSources(cfg, profileName, sourcePaths)

	if len(secretsCfg.Providers) == 0 {
		return nil, fmt.Errorf("no secrets configured")
	}

	var enc *encrypt.AgeEncryptor
	var identitySource string
	if cfg.Age != nil {
		enc, err = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
		if err != nil {
			return nil, fmt.Errorf("setting up encryption: %w", err)
		}
		identitySource = cfg.Age.Identity
		if identitySource == "" && cfg.Age.IdentityCommand != "" {
			identitySource = cfg.Age.IdentityCommand
		}
	}

	return secrets.NewManager(secretsCfg, enc, identitySource)
}

func resolveSecrets(cfg *config.Config, profileName string) *config.SecretsConfig {
	result := cfg.Secrets
	if result == nil {
		result = &config.SecretsConfig{Providers: make(map[string]*config.SecretsProvider)}
	}

	if profileName != "" {
		p, ok := cfg.Profiles[profileName]
		if ok && p.Secrets != nil {
			result = config.MergeSecretsConfig(result, p.Secrets)
		}
	}

	return result
}

func resolveSecretsWithSources(cfg *config.Config, profileName string, sourcePaths []string) *config.SecretsConfig {
	result := resolveSecrets(cfg, profileName)

	for _, srcPath := range sourcePaths {
		dirCfg, err := config.LoadDirConfig(srcPath)
		if err != nil || dirCfg == nil || dirCfg.Secrets == nil {
			continue
		}
		result = config.MergeSecretsConfig(result, dirCfg.Secrets)
	}

	return result
}

func matchSecretsPattern(path, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return path == pattern
}

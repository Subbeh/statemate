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
	"github.com/subbeh/statemate/internal/source"
	"github.com/subbeh/statemate/internal/template"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage secrets",
	Long:  "Fetch and inspect secrets referenced in templates",
}

var secretsFetchCmd = &cobra.Command{
	Use:   "fetch [pattern]",
	Short: "Fetch secrets from providers",
	Long:  "Scan templates for secret references, fetch from providers, and update the encrypted cache. Optionally filter by item pattern (e.g., 'github*')",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSecretsFetch,
}

var secretsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets referenced in templates and cache status",
	RunE:  runSecretsList,
}

var secretsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show secrets that need fetching",
	RunE:  runSecretsStatus,
}

func init() {
	rootCmd.AddCommand(secretsCmd)
	secretsCmd.AddCommand(secretsFetchCmd)
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsStatusCmd)
}

func runSecretsFetch(cmd *cobra.Command, args []string) error {
	mgr, items, err := setupSecrets(cmd)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("No secrets referenced in templates")
		return nil
	}

	var pattern string
	if len(args) > 0 {
		pattern = args[0]
	}

	if pattern != "" {
		var filtered []secrets.FetchItem
		for _, item := range items {
			if matchSecretsPattern(item.Item, pattern) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	fmt.Printf("Fetching %d secrets...\n", len(items))

	green := color.New(color.FgGreen).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	mgr.SetProgress(func(key secrets.CacheKey, changed bool) {
		label := fmt.Sprintf("%s/%s/%s", key.Item, key.Type, key.Field)
		if changed {
			fmt.Printf("  %s %s\n", green("✓"), label)
		} else {
			fmt.Printf("  %s %s\n", dim("·"), label)
		}
	})

	result, err := mgr.Fetch(items)
	if err != nil {
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
	mgr, items, err := setupSecrets(cmd)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("No secrets referenced in templates")
		return nil
	}

	cached := mgr.ListCached()

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	maxItem := len("ITEM")
	maxType := len("TYPE")
	maxField := len("FIELD")
	for _, item := range items {
		if len(item.Item) > maxItem {
			maxItem = len(item.Item)
		}
		if len(item.Type) > maxType {
			maxType = len(item.Type)
		}
		if len(item.Field) > maxField {
			maxField = len(item.Field)
		}
	}

	fmt.Printf("%-*s  %-*s  %-*s  %-16s  %s\n", maxItem, "ITEM", maxType, "TYPE", maxField, "FIELD", "LAST FETCHED", "STATUS")
	for _, item := range items {
		fetched := "-"
		var status string
		if cached != nil {
			if cv, ok := cached[item.Key.String()]; ok {
				fetched = cv.FetchedAt.Format("2006-01-02 15:04")
				status = green("cached")
			} else {
				status = yellow("missing")
			}
		} else {
			status = cyan("no cache")
		}

		fmt.Printf("%-*s  %-*s  %-*s  %-16s  %s\n", maxItem, item.Item, maxType, item.Type, maxField, item.Field, fetched, status)
	}

	return nil
}

func runSecretsStatus(cmd *cobra.Command, args []string) error {
	mgr, items, err := setupSecrets(cmd)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("No secrets referenced in templates")
		return nil
	}

	cached := mgr.ListCached()

	var missing []secrets.FetchItem
	for _, item := range items {
		if cached == nil {
			missing = append(missing, item)
			continue
		}
		if _, ok := cached[item.Key.String()]; !ok {
			missing = append(missing, item)
		}
	}

	if len(missing) == 0 {
		fmt.Println("All secrets are cached")
		return nil
	}

	fmt.Println("Secrets needing fetch:")
	for _, item := range missing {
		fmt.Printf("  %s/%s/%s\n", item.Item, item.Type, item.Field)
	}
	fmt.Printf("\nRun 'mate secrets fetch' to fetch %d secrets\n", len(missing))

	return nil
}

func setupSecrets(cmd *cobra.Command) (*secrets.Manager, []secrets.FetchItem, error) {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = profile.Detect(cfg)
	}

	sources := profile.ResolveSources(cfg, profileName)
	sourcePaths := cfg.ResolveSourcePaths(sources)

	var enc *encrypt.AgeEncryptor
	identitySource := ""
	if cfg.Age != nil {
		enc, err = encrypt.NewAgeEncryptor(cfg.Age.Identity, cfg.Age.IdentityCommand, cfg.Age.Recipients)
		if err != nil {
			return nil, nil, fmt.Errorf("setting up encryption: %w", err)
		}
		identitySource = cfg.Age.Identity
	}

	mgr, err := secrets.NewManager(enc, identitySource, cfg.SecretsCache)
	if err != nil {
		return nil, nil, fmt.Errorf("setting up secrets: %w", err)
	}

	// Discover all bitwarden() calls by rendering templates
	templateFiles := discoverTemplateFiles(cfg, sourcePaths)

	tmplCtx, err := template.NewContext(cfg, profileName)
	if err != nil {
		return nil, nil, fmt.Errorf("creating template context: %w", err)
	}

	var decryptFn func([]byte) ([]byte, error)
	if enc != nil && enc.CanDecrypt() {
		decryptFn = enc.Decrypt
	}

	items := secrets.DiscoverByRendering(templateFiles, tmplCtx, decryptFn)

	return mgr, items, nil
}

func discoverTemplateFiles(cfg *config.Config, sourcePaths []string) []string {
	var files []string

	scanner := source.NewScanner(cfg.TargetBase, cfg.SourceDir())
	tree, err := scanner.Scan(sourcePaths)
	if err != nil {
		return files
	}

	for _, entry := range tree.Files() {
		if entry.Attrs.Template {
			files = append(files, entry.SourcePath)
		}
	}

	// Also scan matescripts for template scripts
	scriptsDir := cfg.SourceDir() + "/.matescripts"
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "#template") {
				files = append(files, scriptsDir+"/"+e.Name())
			}
		}
	}

	return files
}

func matchSecretsPattern(item, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(item, prefix)
	}
	return item == pattern
}

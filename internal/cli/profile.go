package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/subbeh/statemate/internal/config"
	"github.com/subbeh/statemate/internal/profile"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Show active profile",
	Long:  "Show which profile will be used and how it was determined",
	RunE:  runProfile,
}

func init() {
	rootCmd.AddCommand(profileCmd)
}

func runProfile(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profileFlag, _ := cmd.Flags().GetString("profile")

	var profileName string
	var source string

	if profileFlag != "" {
		profileName = profileFlag
		source = "--profile flag"
	} else if envProfile := os.Getenv("STATEMATE_PROFILE"); envProfile != "" {
		profileName = envProfile
		source = "STATEMATE_PROFILE environment variable"
	} else if cfg.Profile != "" {
		profileName = cfg.Profile
		source = "config file (profile field)"
	} else {
		profileName = profile.Detect(cfg)
		if profileName != "" {
			source = "auto-detected"
		}
	}

	if profileName != "" {
		fmt.Printf("Profile: %s\n", profileName)
		fmt.Printf("Source:  %s\n", source)
		if source == "auto-detected" {
			printDetectionReason(cfg, profileName)
		}
	} else {
		fmt.Println("Profile: (none)")
		fmt.Println("Source:  no profile matched")

		if len(cfg.Profiles) > 0 {
			fmt.Println("\nConfigured profiles:")
			for name := range cfg.Profiles {
				fmt.Printf("  - %s\n", name)
			}
		}
	}

	sources := profile.ResolveSources(cfg, profileName)
	if len(sources) > 0 {
		fmt.Println("\nSources:")
		for _, s := range sources {
			fmt.Printf("  - %s\n", s)
		}
	}

	return nil
}

func printDetectionReason(cfg *config.Config, profileName string) {
	p, ok := cfg.Profiles[profileName]
	if !ok || p.Detection == nil {
		return
	}

	fmt.Println("\nMatched because:")
	d := p.Detection

	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	if d.Hostname != nil {
		fmt.Printf("  hostname: %s matches %v\n", hostname, d.Hostname)
	}
	if d.User != nil {
		fmt.Printf("  user: %s matches %v\n", username, d.User)
	}
	if d.OS != "" {
		fmt.Printf("  os: %s\n", d.OS)
	}
	if d.Arch != "" {
		fmt.Printf("  arch: %s\n", d.Arch)
	}
	if d.Command != "" {
		fmt.Printf("  command: %s (exit 0)\n", d.Command)
	}
}

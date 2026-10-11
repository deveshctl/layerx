package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(completionCmd)
	rootCmd.ValidArgsFunction = completeImageRefs
}

var completionCmd = &cobra.Command{
	Use:   "completion <shell>",
	Short: "Generate a shell completion script",
	Long: `Generate tab completion for commands and flags. Image completion on the
inspect command queries Docker by default, or Podman with --engine podman.

The examples load completion for the current session. Bash requires
bash-completion; Zsh requires completion to be initialized with compinit.
To load completion on startup, add the matching command to your shell profile.`,
	Example: `  # Bash
  source <(layerx completion bash)

  # Zsh
  autoload -Uz compinit && compinit
  source <(layerx completion zsh)

  # Fish
  layerx completion fish | source

  # PowerShell
  layerx completion powershell | Out-String | Invoke-Expression`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	SilenceUsage:          true,
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 || (args[0] != "bash" && args[0] != "zsh" && args[0] != "fish" && args[0] != "powershell") {
			return fmt.Errorf("choose a shell: bash, zsh, fish, or powershell\nExample: layerx completion bash")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return rootCmd.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
		}
		return nil
	},
}

// engineBinaryForCompletion returns the binary name to use for image-ref
// completion based on the active engineFlag value. "auto" and "" both default
// to "docker" (matching the resolver's probe order).
func engineBinaryForCompletion(flag string) string {
	if flag == "podman" {
		return "podman"
	}
	return "docker"
}

func completeImageRefs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	binary := engineBinaryForCompletion(engineFlag)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var refs []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		if toComplete == "" || strings.HasPrefix(line, toComplete) {
			refs = append(refs, line)
		}
	}
	return refs, cobra.ShellCompDirectiveNoFileComp
}

// completePlatform offers the canonical Docker / OCI platform strings the
// vast majority of multi-arch images actually publish. Custom variants
// (e.g. linux/arm/v6) are still accepted by --platform; this list is just a
// useful tab-complete starting point, mirroring what `docker buildx ls`
// surfaces by default.
func completePlatform(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	suggestions := []string{
		"linux/amd64",
		"linux/arm64",
		"linux/arm64/v8",
		"linux/arm/v7",
		"linux/arm/v6",
		"linux/386",
		"linux/ppc64le",
		"linux/s390x",
		"linux/riscv64",
		"windows/amd64",
		"windows/arm64",
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func completeTheme(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	themes := []string{
		"tokyo-night",
		"catppuccin-mocha",
		"kanagawa",
		"gruvbox-dark",
		"rose-pine",
		"dracula",
		"oxocarbon",
		"cyberdream",
	}
	return themes, cobra.ShellCompDirectiveNoFileComp
}

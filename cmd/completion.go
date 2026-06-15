package cmd

import (
	"context"
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
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate an autocompletion script for the specified shell.

The script enables tab completion for subcommands, flags, and image
references (read from "docker images") in your current shell session.`,
	Example: `  # Bash (current session)
  source <(layerx completion bash)

  # Bash (system-wide install)
  layerx completion bash | sudo tee /etc/bash_completion.d/layerx

  # Zsh
  layerx completion zsh > "${fpath[1]}/_layerx"

  # Fish
  layerx completion fish | source
  layerx completion fish > ~/.config/fish/completions/layerx.fish

  # PowerShell
  layerx completion powershell | Out-String | Invoke-Expression`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
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

func completeImageRefs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
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

package cmd

import (
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(completionCmd)
	rootCmd.ValidArgsFunction = completeImageRefs
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate a completion script for the specified shell.

To load completions:

Bash:
  $ source <(layerx completion bash)
  # Or install permanently:
  $ layerx completion bash > /etc/bash_completion.d/layerx

Zsh:
  $ layerx completion zsh > "${fpath[1]}/_layerx"

Fish:
  $ layerx completion fish | source
  # Or install permanently:
  $ layerx completion fish > ~/.config/fish/completions/layerx.fish

PowerShell:
  PS> layerx completion powershell | Out-String | Invoke-Expression
`,
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

	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		if toComplete == "" || strings.HasPrefix(line, toComplete) {
			refs = append(refs, line)
		}
	}
	return refs, cobra.ShellCompDirectiveNoFileComp
}

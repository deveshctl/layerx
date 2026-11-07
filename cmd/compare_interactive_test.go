package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func interactiveCompareCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("interactive", "i", false, "")
	cmd.Flags().String("mode", compareModeCompact, "")
	cmd.Flags().Int("top", compareTopDefault, "")
	cmd.Flags().String("format", "text", "")
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.ParseFlags(args))
	return cmd
}

func TestInteractiveCompareFlagGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"noninteractive pipes", nil, ""},
		{"explicit false", []string{"--interactive=false", "--format=json"}, ""},
		{"short flag pipes", []string{"-i"}, "terminal input and output"},
		{"long flag pipes", []string{"--interactive"}, "terminal input and output"},
		{"text allowed", []string{"-i", "--format=text"}, "terminal input and output"},
		{"json", []string{"-i", "--format=json"}, "--format text"},
		{"markdown", []string{"-i", "--format=markdown"}, "--format text"},
		{"mode even default", []string{"-i", "--mode=compact"}, "--mode"},
		{"top even default", []string{"-i", "--top=10"}, "--top"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInteractiveCompare(interactiveCompareCommand(t, tc.args...))
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestInteractiveCompareValidationBeforeResolver(t *testing.T) {
	previous := selectResolver
	t.Cleanup(func() { selectResolver = previous })
	selectResolver = func(string) (image.Resolver, error) {
		t.Fatal("invalid interactive invocation reached resolver setup")
		return nil, nil
	}
	cmd := interactiveCompareCommand(t, "-i")
	require.ErrorContains(t, runCompareCmdInner(cmd, []string{"old", "new"}), "terminal input and output")
}

func TestCompareRegistersInteractiveShorthand(t *testing.T) {
	flag := compareCmd.Flags().ShorthandLookup("i")
	require.NotNil(t, flag)
	assert.Equal(t, "interactive", flag.Name)
	assert.Equal(t, "false", flag.DefValue)
}

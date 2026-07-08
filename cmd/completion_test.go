package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestCompletionEngineBinary_DefaultIsDocker(t *testing.T) {
	assert.Equal(t, "docker", engineBinaryForCompletion(""))
}

func TestCompletionEngineBinary_DockerExplicit(t *testing.T) {
	assert.Equal(t, "docker", engineBinaryForCompletion("docker"))
}

func TestCompletionEngineBinary_PodmanExplicit(t *testing.T) {
	assert.Equal(t, "podman", engineBinaryForCompletion("podman"))
}

func TestCompletionEngineBinary_AutoFallsBackToDocker(t *testing.T) {
	// "auto" defaults to docker (matches resolver probe order).
	assert.Equal(t, "docker", engineBinaryForCompletion("auto"))
}

// completeImageRefs must return ShellCompDirectiveNoFileComp regardless of
// whether the engine binary is present. It must never panic.
func TestCompleteImageRefs_DirectiveIsNoFileComp(t *testing.T) {
	prev := engineFlag
	engineFlag = "docker"
	t.Cleanup(func() { engineFlag = prev })

	_, directive := completeImageRefs(&cobra.Command{}, []string{}, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

// completeImageRefs must return early (no subprocess) when args is non-empty.
func TestCompleteImageRefs_NoCompletionWhenArgsPresent(t *testing.T) {
	refs, directive := completeImageRefs(&cobra.Command{}, []string{"already-provided"}, "")
	assert.Nil(t, refs)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

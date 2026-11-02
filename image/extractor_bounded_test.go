package image

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindFileInLayerBounded(t *testing.T) {
	layer := buildSimpleLayerTar(t, map[string][]byte{"file": bytes.Repeat([]byte("a"), 1025)})
	_, _, err := findFileInLayerBounded(t.Context(), layer, "file", 1024)
	assert.ErrorContains(t, err, "limit 1024")
	data, found, err := findFileInLayerBounded(t.Context(), layer, "file", 1025)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, data, 1025)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = findFileInLayerBounded(ctx, layer, "file", 1025)
	assert.ErrorIs(t, err, context.Canceled)
}

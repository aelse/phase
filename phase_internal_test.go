package phase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPhaserValueAncestor(t *testing.T) {
	t.Parallel()

	p0, _ := Next(context.Background())
	defer p0.Close()

	ctx := context.WithValue(p0, &t, "this just sets an intermediary context")

	assert.Equal(t, p0, ancestorPhaseCtx(ctx), "Expected to find an ancestor via intermediary context")
}

package phase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPhaserValueAncestor(t *testing.T) {
	t.Parallel()

	p0 := Next(context.Background())
	defer Close(p0)

	ctx := context.WithValue(p0, &t, "this just sets an intermediary context")

	assert.Equal(t, p0, ancestorPhaseCtx(ctx), "Expected to find an ancestor via intermediary context")
}

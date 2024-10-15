package util

import (
	"context"

	"github.com/ncruces/go-sqlite3/internal/alloc"
	"github.com/tetratelabs/wazero/experimental"
)

type moduleKey struct{}
type moduleState struct {
	mmapState
	handleState
}

func NewContext(ctx context.Context) context.Context {
	state := new(moduleState)
	ctx = experimental.WithMemoryAllocator(ctx, experimental.MemoryAllocatorFunc(alloc.NewMemory))
	ctx = experimental.WithCloseNotifier(ctx, state)
	ctx = context.WithValue(ctx, moduleKey{}, state)
	return ctx
}

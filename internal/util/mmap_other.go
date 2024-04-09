//go:build !(linux || darwin) || !(amd64 || arm64) || sqlite3_flock || sqlite3_noshm || sqlite3_nosys

package util

import "context"

type mmapState struct{}

func (s *mmapState) init(ctx context.Context, _ bool) context.Context {
	return ctx
}

func CanMap(ctx context.Context) bool {
	return false
}

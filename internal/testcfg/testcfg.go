package testcfg

import (
	"math/bits"
	"os"
	"path/filepath"

	"github.com/ncruces/go-sqlite3"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

func init() {
	if bits.UintSize < 64 {
		return
	}

	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesThreads).
		WithMemoryCapacityFromMax(true).
		WithMemoryLimitPages(1024)

	if os.Getenv("CI") != "" {
		path := filepath.Join(os.TempDir(), "wazero")
		if err := os.MkdirAll(path, 0777); err == nil {
			if cache, err := wazero.NewCompilationCacheWithDir(path); err == nil {
				sqlite3.RuntimeConfig.WithCompilationCache(cache)
			}
		}
	}
}

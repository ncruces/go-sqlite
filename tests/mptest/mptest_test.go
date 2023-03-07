package mptest

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "unsafe"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	_ "github.com/ncruces/go-sqlite3"
)

//go:embed testdata/mptest.wasm
var binary []byte

//go:embed testdata/*.*test
var scripts embed.FS

//go:linkname vfsNewEnvModuleBuilder github.com/ncruces/go-sqlite3.vfsNewEnvModuleBuilder
func vfsNewEnvModuleBuilder(r wazero.Runtime) wazero.HostModuleBuilder

//go:linkname vfsContext github.com/ncruces/go-sqlite3.vfsContext
func vfsContext(ctx context.Context) (context.Context, io.Closer)

var (
	rt        wazero.Runtime
	module    wazero.CompiledModule
	instances atomic.Uint64
)

func init() {
	ctx := context.TODO()

	rt = wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	env := vfsNewEnvModuleBuilder(rt)
	env.NewFunctionBuilder().WithFunc(system).Export("system")
	_, err := env.Instantiate(ctx)
	if err != nil {
		panic(err)
	}

	module, err = rt.CompileModule(ctx, binary)
	if err != nil {
		panic(err)
	}
}

func config(ctx context.Context) wazero.ModuleConfig {
	name := strconv.FormatUint(instances.Add(1), 10)
	log := ctx.Value(logger{}).(io.Writer)
	fs, err := fs.Sub(scripts, "testdata")
	if err != nil {
		panic(err)
	}

	return wazero.NewModuleConfig().
		WithName(name).WithStdout(log).WithStderr(log).WithFS(fs).
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().
		WithOsyield(runtime.Gosched).
		WithRandSource(rand.Reader)
}

func system(ctx context.Context, mod api.Module, ptr uint32) uint32 {
	buf, _ := mod.Memory().Read(ptr, mod.Memory().Size()-ptr)
	buf = buf[:bytes.IndexByte(buf, 0)]

	args := strings.Split(string(buf), " ")
	for i := range args {
		args[i] = strings.Trim(args[i], `"`)
	}
	args = args[:len(args)-1]

	cfg := config(ctx).WithArgs(args...)
	go func() {
		ctx, vfs := vfsContext(ctx)
		rt.InstantiateModule(ctx, module, cfg)
		vfs.Close()
	}()
	return 0
}

func Test_config01(t *testing.T) {
	ctx, vfs := vfsContext(newContext(t))
	name := filepath.Join(t.TempDir(), "test.db")
	cfg := config(ctx).WithArgs("mptest", name, "config01.test")
	_, err := rt.InstantiateModule(ctx, module, cfg)
	if err != nil {
		t.Error(err)
	}
	vfs.Close()
}

func Test_config02(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping in CI")
	}

	ctx, vfs := vfsContext(newContext(t))
	name := filepath.Join(t.TempDir(), "test.db")
	cfg := config(ctx).WithArgs("mptest", name, "config02.test")
	_, err := rt.InstantiateModule(ctx, module, cfg)
	if err != nil {
		t.Error(err)
	}
	vfs.Close()
}

func Test_crash01(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx, vfs := vfsContext(newContext(t))
	name := filepath.Join(t.TempDir(), "test.db")
	cfg := config(ctx).WithArgs("mptest", name, "crash01.test")
	_, err := rt.InstantiateModule(ctx, module, cfg)
	if err != nil {
		t.Error(err)
	}
	vfs.Close()
}

func Test_multiwrite01(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx, vfs := vfsContext(newContext(t))
	name := filepath.Join(t.TempDir(), "test.db")
	cfg := config(ctx).WithArgs("mptest", name, "multiwrite01.test")
	_, err := rt.InstantiateModule(ctx, module, cfg)
	if err != nil {
		t.Error(err)
	}
	vfs.Close()
}

func newContext(t *testing.T) context.Context {
	return context.WithValue(context.Background(), logger{}, &testWriter{T: t})
}

type logger struct{}

type testWriter struct {
	*testing.T
	buf []byte
	mtx sync.Mutex
}

func (l *testWriter) Write(p []byte) (n int, err error) {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	l.buf = append(l.buf, p...)
	for {
		before, after, found := bytes.Cut(l.buf, []byte("\n"))
		if !found {
			return len(p), nil
		}
		l.Logf("%s", before)
		l.buf = after
	}
}

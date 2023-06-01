package vfs_test

import (
	"testing"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/vfs"
)

type testVFS struct {
	*testing.T
}

func (t testVFS) Open(name string, flags vfs.OpenFlag) (vfs.File, vfs.OpenFlag, error) {
	t.Log("Open", name, flags)
	t.SkipNow()
	return nil, flags, nil
}

func (t testVFS) Delete(name string, syncDir bool) error {
	t.Log("Delete", name, syncDir)
	return nil
}

func (t testVFS) Access(name string, flags vfs.AccessFlag) (bool, error) {
	t.Log("Access", name, flags)
	return true, nil
}

func (t testVFS) FullPathname(name string) (string, error) {
	t.Log("FullPathname", name)
	return name, nil
}

func TestRegister(t *testing.T) {
	vfs.Register("foo", testVFS{t})
	defer vfs.Unregister("foo")

	conn, err := sqlite3.Open("file:file.db?vfs=foo")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	t.Error("want skip")
}

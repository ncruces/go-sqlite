package vfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero/experimental/wazerotest"

	"github.com/ncruces/go-sqlite3/internal/util"
)

func Test_vfsLock(t *testing.T) {
	if !SupportsFileLocking {
		t.Skip("skipping without locks")
	}

	name := filepath.Join(t.TempDir(), "test.db")

	// Create a temporary file.
	file1, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		t.Fatal(err)
	}
	defer file1.Close()

	// Open the temporary file again.
	file2, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file2.Close()

	const (
		pFile1  = 4
		pFile2  = 16
		pOutput = 32
	)
	mod := wazerotest.NewModule(wazerotest.NewMemory(wazerotest.PageSize))
	ctx := util.NewContext(context.TODO())

	vfsFileRegister(ctx, mod, pFile1, &vfsFile{File: file1})
	vfsFileRegister(ctx, mod, pFile2, &vfsFile{File: file2})

	rc := vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}
	rc = vfsFileControl(ctx, mod, pFile2, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("invalid lock state", got)
	}

	rc = vfsLock(ctx, mod, pFile2, LOCK_SHARED)
	if rc != _OK {
		t.Fatal("returned", rc)
	}

	rc = vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}
	rc = vfsFileControl(ctx, mod, pFile2, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_SHARED {
		t.Error("invalid lock state", got)
	}

	rc = vfsLock(ctx, mod, pFile2, LOCK_RESERVED)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	rc = vfsLock(ctx, mod, pFile2, LOCK_SHARED)
	if rc != _OK {
		t.Fatal("returned", rc)
	}

	rc = vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Log("file wasn't locked, locking is incompatible with SQLite")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Error("file wasn't locked")
	}
	rc = vfsFileControl(ctx, mod, pFile2, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_RESERVED {
		t.Error("invalid lock state", got)
	}

	rc = vfsLock(ctx, mod, pFile2, LOCK_EXCLUSIVE)
	if rc != _OK {
		t.Fatal("returned", rc)
	}

	rc = vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Log("file wasn't locked, locking is incompatible with SQLite")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Error("file wasn't locked")
	}
	rc = vfsFileControl(ctx, mod, pFile2, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_EXCLUSIVE {
		t.Error("invalid lock state", got)
	}

	rc = vfsLock(ctx, mod, pFile1, LOCK_SHARED)
	if rc == _OK {
		t.Fatal("returned", rc)
	}

	rc = vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Log("file wasn't locked, locking is incompatible with SQLite")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got == LOCK_NONE {
		t.Error("file wasn't locked")
	}
	rc = vfsFileControl(ctx, mod, pFile1, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("invalid lock state", got)
	}

	rc = vfsUnlock(ctx, mod, pFile2, LOCK_SHARED)
	if rc != _OK {
		t.Fatal("returned", rc)
	}

	rc = vfsCheckReservedLock(ctx, mod, pFile1, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}
	rc = vfsCheckReservedLock(ctx, mod, pFile2, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_NONE {
		t.Error("file was locked")
	}

	rc = vfsLock(ctx, mod, pFile1, LOCK_SHARED)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	rc = vfsFileControl(ctx, mod, pFile1, _FCNTL_LOCKSTATE, pOutput)
	if rc != _OK {
		t.Fatal("returned", rc)
	}
	if got := util.Read32[LockLevel](mod, pOutput); got != LOCK_SHARED {
		t.Error("invalid lock state", got)
	}
}

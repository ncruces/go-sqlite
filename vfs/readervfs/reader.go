package readervfs

import (
	"io"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/vfs"
)

type readerVFS struct{}

// Open implements the [vfs.VFS] interface.
func (readerVFS) Open(name string, flags vfs.OpenFlag) (vfs.File, vfs.OpenFlag, error) {
	if flags&vfs.OPEN_MAIN_DB == 0 {
		return nil, flags, sqlite3.CANTOPEN
	}
	readerMtx.RLock()
	defer readerMtx.RUnlock()
	if ra, ok := readerDBs[name]; ok {
		return readerFile{ra}, flags | vfs.OPEN_READONLY, nil
	}
	return nil, flags, sqlite3.CANTOPEN
}

// Delete implements the [vfs.VFS] interface.
func (readerVFS) Delete(name string, dirSync bool) error {
	return sqlite3.IOERR_DELETE
}

// Access implements the [vfs.VFS] interface.
func (readerVFS) Access(name string, flag vfs.AccessFlag) (bool, error) {
	return false, nil
}

// FullPathname implements the [vfs.VFS] interface.
func (readerVFS) FullPathname(name string) (string, error) {
	return name, nil
}

type readerFile struct{ SizeReaderAt }

func (r readerFile) Close() error {
	if c, ok := r.SizeReaderAt.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (readerFile) WriteAt(b []byte, off int64) (n int, err error) {
	return 0, sqlite3.READONLY
}

func (readerFile) Truncate(size int64) error {
	return sqlite3.READONLY
}

func (readerFile) Sync(flag vfs.SyncFlag) error {
	return nil
}

func (readerFile) Lock(lock vfs.LockLevel) error {
	return nil
}

func (readerFile) Unlock(lock vfs.LockLevel) error {
	return nil
}

func (readerFile) CheckReservedLock() (bool, error) {
	return false, nil
}

func (readerFile) SectorSize() int {
	return 0
}

func (readerFile) DeviceCharacteristics() vfs.DeviceCharacteristic {
	return vfs.IOCAP_IMMUTABLE
}

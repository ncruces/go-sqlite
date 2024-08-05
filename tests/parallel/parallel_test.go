package tests

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/internal/testcfg"
	"github.com/ncruces/go-sqlite3/vfs"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
)

func Test_parallel(t *testing.T) {
	if !vfs.SupportsFileLocking {
		t.Skip("skipping without locks")
	}

	var iter int
	if testing.Short() {
		iter = 1000
	} else {
		iter = 5000
	}

	name := "file:" +
		filepath.ToSlash(filepath.Join(t.TempDir(), "test.db")) +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(truncate)" +
		"&_pragma=synchronous(off)"
	testParallel(t, name, iter)
	testIntegrity(t, name)
}

func Test_wal(t *testing.T) {
	if !vfs.SupportsSharedMemory {
		t.Skip("skipping without shared memory")
	}

	name := "file:" +
		filepath.ToSlash(filepath.Join(t.TempDir(), "test.db")) +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(wal)" +
		"&_pragma=synchronous(off)"
	testParallel(t, name, 1000)
	testIntegrity(t, name)
}

func Test_memdb(t *testing.T) {
	var iter int
	if testing.Short() {
		iter = 1000
	} else {
		iter = 5000
	}

	name := memdb.TestDB(t) +
		"_pragma=busy_timeout(10000)"
	testParallel(t, name, iter)
	testIntegrity(t, name)
}

func Test_adiantum(t *testing.T) {
	if !vfs.SupportsFileLocking {
		t.Skip("skipping without locks")
	}

	var iter int
	if testing.Short() {
		iter = 1000
	} else {
		iter = 5000
	}

	name := "file:" +
		filepath.ToSlash(filepath.Join(t.TempDir(), "test.db")) +
		"?vfs=adiantum" +
		"&_pragma=hexkey(e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(truncate)" +
		"&_pragma=synchronous(off)"
	testParallel(t, name, iter)
	testIntegrity(t, name)
}

func TestMultiProcess(t *testing.T) {
	if !vfs.SupportsFileLocking {
		t.Skip("skipping without locks")
	}
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	file := filepath.Join(t.TempDir(), "test.db")
	t.Setenv("TestMultiProcess_dbfile", file)

	name := "file:" + filepath.ToSlash(file) +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(truncate)" +
		"&_pragma=synchronous(off)"

	cmd := exec.Command(os.Args[0], append(os.Args[1:], "-test.v", "-test.run=TestChildProcess")...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var buf [3]byte
	// Wait for child to start.
	if _, err := io.ReadFull(out, buf[:]); err != nil {
		t.Fatal(err)
	} else if str := string(buf[:]); str != "===" {
		t.Fatal(str)
	}

	testParallel(t, name, 1000)
	if err := cmd.Wait(); err != nil {
		t.Error(err)
	}
	testIntegrity(t, name)
}

func TestChildProcess(t *testing.T) {
	file := os.Getenv("TestMultiProcess_dbfile")
	if file == "" || testing.Short() {
		t.SkipNow()
	}

	name := "file:" + filepath.ToSlash(file) +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(truncate)" +
		"&_pragma=synchronous(off)"

	testParallel(t, name, 1000)
}

func Benchmark_parallel(b *testing.B) {
	if !vfs.SupportsSharedMemory {
		b.Skip("skipping without shared memory")
	}

	sqlite3.Initialize()
	b.ResetTimer()

	name := "file:" +
		filepath.Join(b.TempDir(), "test.db") +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(truncate)" +
		"&_pragma=synchronous(off)"
	testParallel(b, name, b.N)
}

func Benchmark_wal(b *testing.B) {
	if !vfs.SupportsSharedMemory {
		b.Skip("skipping without shared memory")
	}

	sqlite3.Initialize()
	b.ResetTimer()

	name := "file:" +
		filepath.Join(b.TempDir(), "test.db") +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(wal)" +
		"&_pragma=synchronous(off)"
	testParallel(b, name, b.N)
}

func Benchmark_memdb(b *testing.B) {
	sqlite3.Initialize()
	b.ResetTimer()

	name := memdb.TestDB(b) +
		"_pragma=busy_timeout(10000)"
	testParallel(b, name, b.N)
}

func testParallel(t testing.TB, name string, n int) {
	writer := func() error {
		db, err := sqlite3.Open(name)
		if err != nil {
			return err
		}
		defer db.Close()

		err = db.BusyHandler(func(count int) (retry bool) {
			time.Sleep(time.Millisecond)
			return true
		})
		if err != nil {
			return err
		}

		err = db.Exec(`CREATE TABLE IF NOT EXISTS users (id INT, name VARCHAR(10))`)
		if err != nil {
			return err
		}

		err = db.Exec(`INSERT INTO users (id, name) VALUES (0, 'go'), (1, 'zig'), (2, 'whatever')`)
		if err != nil {
			return err
		}

		return db.Close()
	}

	reader := func() error {
		db, err := sqlite3.Open(name)
		if err != nil {
			return err
		}
		defer db.Close()

		stmt, _, err := db.Prepare(`SELECT id, name FROM users`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		row := 0
		for stmt.Step() {
			row++
		}
		if err := stmt.Err(); err != nil {
			return err
		}
		if row%3 != 0 {
			t.Errorf("got %d rows, want multiple of 3", row)
		}

		err = stmt.Close()
		if err != nil {
			return err
		}

		return db.Close()
	}

	err := writer()
	if err != nil {
		t.Fatal(err)
	}

	var group errgroup.Group
	group.SetLimit(6)
	for i := 0; i < n; i++ {
		if i&7 != 7 {
			group.Go(reader)
		} else {
			group.Go(writer)
		}
	}
	err = group.Wait()
	if err != nil {
		t.Error(err)
	}
}

func testIntegrity(t testing.TB, name string) {
	db, err := sqlite3.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	test := `PRAGMA integrity_check`
	if testing.Short() {
		test = `PRAGMA quick_check`
	}

	stmt, _, err := db.Prepare(test)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	for stmt.Step() {
		if row := stmt.ColumnText(0); row != "ok" {
			t.Error(row)
		}
	}
	if err := stmt.Err(); err != nil {
		t.Fatal(err)
	}

	err = stmt.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}
}

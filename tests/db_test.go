package tests

import (
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/sqlite3vfs"
)

func TestDB_memory(t *testing.T) {
	testDB(t, ":memory:")
}

func TestDB_file(t *testing.T) {
	testDB(t, filepath.Join(t.TempDir(), "test.db"))
}

func TestDB_VFS(t *testing.T) {
	sqlite3vfs.Register("memvfs", sqlite3vfs.MemoryVFS{
		"test.db": &sqlite3vfs.MemoryDB{},
	})
	testDB(t, "file:test.db?vfs=memvfs&_pragma=journal_mode(memory)")
}

func testDB(t *testing.T, name string) {
	t.Parallel()

	db, err := sqlite3.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.Exec(`CREATE TABLE IF NOT EXISTS users (id INT, name VARCHAR(10))`)
	if err != nil {
		t.Fatal(err)
	}

	err = db.Exec(`INSERT INTO users (id, name) VALUES (0, 'go'), (1, 'zig'), (2, 'whatever')`)
	if err != nil {
		t.Fatal(err)
	}
	changes := db.Changes()
	if changes != 3 {
		t.Errorf("got %d want 3", changes)
	}

	stmt, _, err := db.Prepare(`SELECT id, name FROM users`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	row := 0
	ids := []int{0, 1, 2}
	names := []string{"go", "zig", "whatever"}
	for ; stmt.Step(); row++ {
		id := stmt.ColumnInt(0)
		name := stmt.ColumnText(1)

		if id != ids[row] {
			t.Errorf("got %d, want %d", id, ids[row])
		}
		if name != names[row] {
			t.Errorf("got %q, want %q", name, names[row])
		}
	}
	if row != 3 {
		t.Errorf("got %d, want %d", row, len(ids))
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

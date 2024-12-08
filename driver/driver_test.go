package driver

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/internal/testcfg"
	"github.com/ncruces/go-sqlite3/internal/util"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
)

func Test_Open_error(t *testing.T) {
	t.Parallel()

	_, err := Open("", nil, nil, nil)
	if err == nil {
		t.Error("want error")
	}
	if !errors.Is(err, sqlite3.MISUSE) {
		t.Errorf("got %v, want sqlite3.MISUSE", err)
	}
}

func Test_Open_dir(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ".")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Conn(context.TODO())
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, sqlite3.CANTOPEN) {
		t.Errorf("got %v, want sqlite3.CANTOPEN", err)
	}
}

func Test_Open_pragma(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t, url.Values{
		"_pragma": {"busy_timeout(1000)"},
	})

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var timeout int
	err = db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout)
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 1000 {
		t.Errorf("got %v, want 1000", timeout)
	}
}

func Test_Open_pragma_invalid(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t, url.Values{
		"_pragma": {"busy_timeout 1000"},
	})

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Conn(context.TODO())
	if err == nil {
		t.Fatal("want error")
	}
	var serr *sqlite3.Error
	if !errors.As(err, &serr) {
		t.Fatalf("got %T, want sqlite3.Error", err)
	}
	if rc := serr.Code(); rc != sqlite3.ERROR {
		t.Errorf("got %d, want sqlite3.ERROR", rc)
	}
	if got := err.Error(); got != `sqlite3: invalid _pragma: sqlite3: SQL logic error: near "1000": syntax error` {
		t.Error("got message:", got)
	}
}

func Test_Open_txLock(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t, url.Values{
		"_txlock": {"exclusive"},
		"_pragma": {"busy_timeout(1000)"},
	})

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx1, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Begin()
	if err == nil {
		t.Error("want error")
	}
	if !errors.Is(err, sqlite3.BUSY) {
		t.Errorf("got %v, want sqlite3.BUSY", err)
	}
	var terr interface{ Temporary() bool }
	if !errors.As(err, &terr) || !terr.Temporary() {
		t.Error("not temporary", err)
	}

	err = tx1.Commit()
	if err != nil {
		t.Fatal(err)
	}
}

func Test_Open_txLock_invalid(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t, url.Values{
		"_txlock": {"xclusive"},
	})

	_, err := sql.Open("sqlite3", tmp+"_txlock=xclusive")
	if err == nil {
		t.Fatal("want error")
	}
	if got := err.Error(); got != `sqlite3: invalid _txlock: xclusive` {
		t.Error("got message:", got)
	}
}

func Test_BeginTx(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t, url.Values{
		"_txlock": {"exclusive"},
		"_pragma": {"busy_timeout(0)"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err.Error() != string(util.IsolationErr) {
		t.Error("want isolationErr")
	}

	tx1, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	tx2, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx1.Exec(`CREATE TABLE test (col)`)
	if err == nil {
		t.Error("want error")
	}
	if !errors.Is(err, sqlite3.READONLY) {
		t.Errorf("got %v, want sqlite3.READONLY", err)
	}

	err = tx2.Commit()
	if err != nil {
		t.Fatal(err)
	}

	err = tx1.Commit()
	if err != nil {
		t.Fatal(err)
	}
}

func Test_Prepare(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t)

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var serr *sqlite3.Error
	_, err = db.Prepare(`SELECT`)
	if err == nil {
		t.Error("want error")
	}
	if !errors.As(err, &serr) {
		t.Fatalf("got %T, want sqlite3.Error", err)
	}
	if rc := serr.Code(); rc != sqlite3.ERROR {
		t.Errorf("got %d, want sqlite3.ERROR", rc)
	}
	if got := err.Error(); got != `sqlite3: SQL logic error: incomplete input` {
		t.Error("got message:", got)
	}

	_, err = db.Prepare(`SELECT 1; `)
	if err.Error() != string(util.TailErr) {
		t.Error("want tailErr")
	}

	_, err = db.Prepare(`SELECT 1; SELECT`)
	if err.Error() != string(util.TailErr) {
		t.Error("want tailErr")
	}

	_, err = db.Prepare(`SELECT 1; SELECT 2`)
	if err.Error() != string(util.TailErr) {
		t.Error("want tailErr")
	}
}

func Test_QueryRow_named(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stmt, err := conn.PrepareContext(ctx, `SELECT ?, ?5, :AAA, @AAA, $AAA`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	date := time.Now()
	row := stmt.QueryRow(true, sql.Named("AAA", math.Pi), nil /*3*/, nil /*4*/, date /*5*/)

	var first bool
	var fifth time.Time
	var colon, at, dollar float32
	err = row.Scan(&first, &fifth, &colon, &at, &dollar)
	if err != nil {
		t.Fatal(err)
	}

	if first != true {
		t.Errorf("want true, got %v", first)
	}
	if colon != math.Pi {
		t.Errorf("want π, got %v", colon)
	}
	if at != math.Pi {
		t.Errorf("want π, got %v", at)
	}
	if dollar != math.Pi {
		t.Errorf("want π, got %v", dollar)
	}
	if !fifth.Equal(date) {
		t.Errorf("want %v, got %v", date, fifth)
	}
}

func Test_QueryRow_blob_null(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t)

	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT NULL    UNION ALL
		SELECT x'cafe' UNION ALL
		SELECT x'babe' UNION ALL
		SELECT NULL
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	want := [][]byte{nil, {0xca, 0xfe}, {0xba, 0xbe}, nil}
	for i := 0; rows.Next(); i++ {
		var buf sql.RawBytes
		err = rows.Scan(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, want[i]) {
			t.Errorf("got %q, want %q", buf, want[i])
		}
	}
}

func Test_time(t *testing.T) {
	t.Parallel()

	for _, fmt := range []string{"auto", "sqlite", "rfc3339", time.ANSIC} {
		t.Run(fmt, func(t *testing.T) {
			tmp := memdb.TestDB(t, url.Values{
				"_timefmt": {fmt},
			})

			db, err := sql.Open("sqlite3", tmp)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			twosday := time.Date(2022, 2, 22, 22, 22, 22, 0, time.UTC)

			_, err = db.Exec(`CREATE TABLE test (at DATETIME)`)
			if err != nil {
				t.Fatal(err)
			}

			_, err = db.Exec(`INSERT INTO test VALUES (?)`, twosday)
			if err != nil {
				t.Fatal(err)
			}

			var got time.Time
			err = db.QueryRow(`SELECT * FROM test`).Scan(&got)
			if err != nil {
				t.Fatal(err)
			}

			if !got.Equal(twosday) {
				t.Errorf("got: %v", got)
			}
		})
	}
}

func TestColumnTypeScanType(t *testing.T) {
	t.Parallel()
	tmp := memdb.TestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := Open(tmp, nil, func(c *sqlite3.Conn) error {
		return c.Exec(`PRAGMA optimize`)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx,
		`CREATE TABLE test_types (
			col_int INTEGER,
			col_text TEXT,
			col_blob BLOB,
			col_real REAL,
			col_bool BOOLEAN,
			col_date DATE,
			col_time TIME,
			col_datetime DATETIME,
			col_timestamp TIMESTAMP
		)`)
	if err != nil {
		t.Fatal(err)
	}

	stmt, err := conn.PrepareContext(context.Background(),
		`SELECT col_int, col_text, col_blob, col_real, col_bool, col_date, col_time, col_datetime, col_timestamp FROM test_types`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	typs, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		index int
		want  reflect.Type
	}{
		{0, reflect.TypeOf(int64(0))},
		{1, reflect.TypeOf("")},
		{2, reflect.TypeOf([]byte(nil))},
		{3, reflect.TypeOf(float64(0))},
		{4, reflect.TypeOf(false)},
		{5, reflect.TypeOf(time.Time{})},
		{6, reflect.TypeOf(time.Time{})},
		{7, reflect.TypeOf(time.Time{})},
		{8, reflect.TypeOf(time.Time{})},
	}

	for _, tt := range tests {
		got := typs[tt.index].ScanType()
		if got != tt.want {
			t.Errorf("ColumnTypeScanType(%d) = %v, want %v", tt.index, got, tt.want)
		}
	}
}

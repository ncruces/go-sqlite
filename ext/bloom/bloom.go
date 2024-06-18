// Package bloom provides a Bloom filter virtual table.
//
// A Bloom filter is a space-efficient probabilistic data structure
// used to test whether an element is a member of a set.
//
// https://github.com/nalgeon/sqlean/issues/27#issuecomment-1002267134
package bloom

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/dchest/siphash"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/internal/util"
)

// Register registers the bloom_filter virtual table:
//
//	CREATE VIRTUAL TABLE foo USING bloom_filter(nElements, falseProb, kHashes)
func Register(db *sqlite3.Conn) {
	sqlite3.CreateModule(db, "bloom_filter", create, connect)
}

type bloom struct {
	db      *sqlite3.Conn
	schema  string
	storage string
	prob    float64
	nfilter int64
	hashes  int
}

func create(db *sqlite3.Conn, _, schema, table string, arg ...string) (_ *bloom, err error) {
	t := bloom{
		db:      db,
		schema:  schema,
		storage: table + "_storage",
	}

	nelem := 100
	if len(arg) > 0 {
		nelem, err = strconv.Atoi(arg[0])
		if err != nil {
			return nil, err
		}
		if nelem <= 0 {
			return nil, errors.New("bloom: number of elements in filter must be positive")
		}
	}

	if len(arg) > 1 {
		t.prob, err = strconv.ParseFloat(arg[1], 64)
		if err != nil {
			return nil, err
		}
		if t.prob <= 0 || t.prob >= 1 {
			return nil, errors.New("bloom: probability must be in the range (0,1)")
		}
	} else {
		t.prob = 0.01
	}

	if len(arg) > 2 {
		t.hashes, err = strconv.Atoi(arg[2])
		if err != nil {
			return nil, err
		}
		if t.hashes <= 0 {
			return nil, errors.New("bloom: number of hash functions must be positive")
		}
	} else {
		t.hashes = int(math.Round(-math.Log2(t.prob)))
	}

	t.nfilter = computeLength(nelem, t.prob)

	err = db.Exec(fmt.Sprintf(
		`CREATE TABLE %s.%s (data BLOB, p REAL, n INTEGER, m INTEGER, k INTEGER)`,
		sqlite3.QuoteIdentifier(t.schema), sqlite3.QuoteIdentifier(t.storage)))
	if err != nil {
		return nil, err
	}

	err = db.Exec(fmt.Sprintf(
		`INSERT INTO %s.%s (rowid, data, p, n, m, k)
		 VALUES (1, zeroblob(%d), %f, %d, %d, %d)`,
		sqlite3.QuoteIdentifier(t.schema), sqlite3.QuoteIdentifier(t.storage),
		t.nfilter, t.prob, nelem, t.nfilter*8, t.hashes))
	if err != nil {
		return nil, err
	}

	err = db.DeclareVTab(
		`CREATE TABLE x(present, word HIDDEN NOT NULL PRIMARY KEY) WITHOUT ROWID`)
	if err != nil {
		t.Destroy()
		return nil, err
	}
	return &t, nil
}

func connect(db *sqlite3.Conn, _, schema, table string, arg ...string) (_ *bloom, err error) {
	t := bloom{
		db:      db,
		schema:  schema,
		storage: table + "_storage",
	}

	err = db.DeclareVTab(
		`CREATE TABLE x(present, word HIDDEN NOT NULL PRIMARY KEY) WITHOUT ROWID`)
	if err != nil {
		return nil, err
	}

	load, _, err := db.Prepare(fmt.Sprintf(
		`SELECT m/8, p, k FROM %s.%s WHERE rowid = 1`,
		sqlite3.QuoteIdentifier(t.schema), sqlite3.QuoteIdentifier(t.storage)))
	if err != nil {
		return nil, err
	}
	defer load.Close()

	if !load.Step() {
		if err = load.Err(); err == nil {
			err = sqlite3.CORRUPT_VTAB
		}
		return nil, err
	}

	t.nfilter = load.ColumnInt64(0)
	t.prob = load.ColumnFloat(1)
	t.hashes = load.ColumnInt(2)
	return &t, nil
}

func (b *bloom) Destroy() error {
	return b.db.Exec(fmt.Sprintf(`DROP TABLE %s.%s`,
		sqlite3.QuoteIdentifier(b.schema),
		sqlite3.QuoteIdentifier(b.storage)))
}

func (b *bloom) Rename(new string) error {
	new += "_storage"
	err := b.db.Exec(fmt.Sprintf(`ALTER TABLE %s.%s RENAME TO %s`,
		sqlite3.QuoteIdentifier(b.schema),
		sqlite3.QuoteIdentifier(b.storage),
		sqlite3.QuoteIdentifier(new),
	))
	if err == nil {
		b.storage = new
	}
	return err
}

func (b *bloom) BestIndex(idx *sqlite3.IndexInfo) error {
	for n, cst := range idx.Constraint {
		if cst.Usable && cst.Column == 1 &&
			cst.Op == sqlite3.INDEX_CONSTRAINT_EQ {
			idx.ConstraintUsage[n].ArgvIndex = 1
		}
	}
	idx.OrderByConsumed = true
	idx.EstimatedRows = 1
	idx.EstimatedCost = float64(b.hashes)
	idx.IdxFlags = sqlite3.INDEX_SCAN_UNIQUE
	return nil
}

func (b *bloom) Update(arg ...sqlite3.Value) (rowid int64, err error) {
	if arg[0].Type() != sqlite3.NULL {
		if len(arg) == 1 {
			return 0, errors.New("bloom: elements cannot be deleted")
		}
		return 0, errors.New("bloom: elements cannot be updated")
	}

	blob := arg[2].RawBlob()

	f, err := b.db.OpenBlob(b.schema, b.storage, "data", 1, true)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	for n := 0; n < b.hashes; n++ {
		hash := calcHash(n, blob)
		hash %= uint64(b.nfilter * 8)
		bitpos := byte(hash % 8)
		bytepos := int64(hash / 8)

		var buf [1]byte
		_, err = f.Seek(bytepos, io.SeekStart)
		if err != nil {
			return 0, err
		}
		_, err = f.Read(buf[:])
		if err != nil {
			return 0, err
		}

		buf[0] |= (1 << bitpos)

		_, err = f.Seek(bytepos, io.SeekStart)
		if err != nil {
			return 0, err
		}
		_, err = f.Write(buf[:])
		if err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func (b *bloom) Open() (sqlite3.VTabCursor, error) {
	return &cursor{bloom: b}, nil
}

type cursor struct {
	*bloom
	eof bool
	arg *sqlite3.Value
}

func (c *cursor) Filter(idxNum int, idxStr string, arg ...sqlite3.Value) error {
	if len(arg) != 1 {
		return nil
	}

	c.eof = false
	c.arg = &arg[0]
	blob := arg[0].RawBlob()

	f, err := c.db.OpenBlob(c.schema, c.storage, "data", 1, false)
	if err != nil {
		return err
	}
	defer f.Close()

	for n := 0; n < c.hashes; n++ {
		hash := calcHash(n, blob)
		hash %= uint64(c.nfilter * 8)
		bitpos := byte(hash % 8)
		bytepos := int64(hash / 8)

		var buf [1]byte
		_, err = f.Seek(bytepos, io.SeekStart)
		if err != nil {
			return err
		}
		_, err = f.Read(buf[:])
		if err != nil {
			return err
		}

		c.eof = (buf[0] & (1 << bitpos)) == 0
		if c.eof {
			break
		}
	}
	return nil
}

func (c *cursor) Column(ctx *sqlite3.Context, n int) error {
	switch n {
	case 0:
		ctx.ResultBool(true)
	case 1:
		ctx.ResultValue(*c.arg)
	default:
		panic(util.AssertErr())
	}
	return nil
}

func (c *cursor) Next() error {
	c.eof = true
	return nil
}

func (c *cursor) EOF() bool {
	return c.eof
}

func (c *cursor) RowID() (int64, error) {
	return 0, nil
}

func calcHash(k int, b []byte) uint64 {
	return siphash.Hash(^uint64(k), uint64(k), b)
}

func computeLength(n int, p float64) int64 {
	bits := math.Ceil(-((float64(n) * math.Log(p)) / (math.Ln2 * math.Ln2)))
	return (int64(bits) + 7) / 8
}

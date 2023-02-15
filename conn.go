package sqlite3

import (
	"context"
	"math"
	"time"
)

// Conn is a database connection handle.
//
// https://www.sqlite.org/c3ref/sqlite3.html
type Conn struct {
	ctx    context.Context
	api    sqliteAPI
	mem    memory
	arena  arena
	handle uint32

	waiter chan struct{}
	done   <-chan struct{}
}

// Open calls [OpenFlags] with [OPEN_READWRITE] and [OPEN_CREATE].
func Open(filename string) (conn *Conn, err error) {
	return OpenFlags(filename, OPEN_READWRITE|OPEN_CREATE)
}

// OpenFlags opens an SQLite database file as specified by the filename argument.
//
// https://www.sqlite.org/c3ref/open.html
func OpenFlags(filename string, flags OpenFlag) (conn *Conn, err error) {
	ctx := context.Background()
	module, err := sqlite3.instantiateModule(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if conn == nil {
			module.Close(ctx)
		}
	}()

	c, err := newConn(ctx, module)
	if err != nil {
		return nil, err
	}
	c.arena = c.newArena(1024)

	defer c.arena.reset()
	connPtr := c.arena.new(ptrlen)
	namePtr := c.arena.string(filename)

	r, err := c.api.open.Call(c.ctx, uint64(namePtr), uint64(connPtr), uint64(flags), 0)
	if err != nil {
		return nil, err
	}

	c.handle = c.mem.readUint32(connPtr)
	if err := c.error(r[0]); err != nil {
		return nil, err
	}
	return c, nil
}

// Close closes the database connection.
//
// If the database connection is associated with unfinalized prepared statements,
// open blob handles, and/or unfinished backup objects,
// Close will leave the database connection open and return [BUSY].
//
// https://www.sqlite.org/c3ref/close.html
func (c *Conn) Close() error {
	if c == nil {
		return nil
	}

	c.SetInterrupt(nil)

	r, err := c.api.close.Call(c.ctx, uint64(c.handle))
	if err != nil {
		return err
	}

	if err := c.error(r[0]); err != nil {
		return err
	}

	c.handle = 0
	return c.mem.mod.Close(c.ctx)
}

// SetInterrupt interrupts a long-running query when done is closed.
//
// Subsequent uses of the connection will return [INTERRUPT]
// until done is reset by another call to SetInterrupt.
//
// Typically, done is provided by [context.Context.Done]:
//
//	ctx, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
//	conn.SetInterrupt(ctx.Done())
//	defer cancel()
//
// https://www.sqlite.org/c3ref/interrupt.html
func (c *Conn) SetInterrupt(done <-chan struct{}) (old <-chan struct{}) {
	// Is a waiter running?
	if c.waiter != nil {
		c.waiter <- struct{}{} // Cancel the waiter.
		<-c.waiter             // Wait for it to finish.
		c.waiter = nil
	}

	old = c.done
	c.done = done
	if done == nil {
		return old
	}

	waiter := make(chan struct{})
	c.waiter = waiter
	go func() {
		select {
		case <-waiter: // Waiter was cancelled.
			// Signal that the waiter has finished.
			waiter <- struct{}{}
			return
		case <-done: // Done was closed.

			// Interrupt every 100ms to prevent a race condition
			// where the interrupt is lost if no statemet is running.
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				// Because it doesn't touch the C stack,
				// sqlite3_interrupt is safe to call from a goroutine.
				_, err := c.api.interrupt.Call(c.ctx, uint64(c.handle))
				if err != nil {
					panic(err)
				}

				// Wait for the next call to SetInterrupt.
				select {
				case <-waiter: // Waiter was cancelled.
					// Signal that the waiter has finished.
					waiter <- struct{}{}
					return

				case <-ticker.C:
					// Interrupt again.
					continue
				}
			}
		}
	}()
	return old
}

// Exec is a convenience function that allows an application to run
// multiple statements of SQL without having to use a lot of code.
//
// https://www.sqlite.org/c3ref/exec.html
func (c *Conn) Exec(sql string) error {
	defer c.arena.reset()
	sqlPtr := c.arena.string(sql)

	if c.interrupted() {
		return c.error(uint64(INTERRUPT))
	}
	r, err := c.api.exec.Call(c.ctx, uint64(c.handle), uint64(sqlPtr), 0, 0, 0)
	if err != nil {
		return err
	}
	return c.error(r[0])
}

// Prepare calls [Conn.PrepareFlags] with no flags.
func (c *Conn) Prepare(sql string) (stmt *Stmt, tail string, err error) {
	return c.PrepareFlags(sql, 0)
}

// PrepareFlags compiles the first SQL statement in sql;
// tail is left pointing to what remains uncompiled.
// If the input text contains no SQL (if the input is an empty string or a comment),
// both stmt and err will be nil.
//
// https://www.sqlite.org/c3ref/prepare.html
func (c *Conn) PrepareFlags(sql string, flags PrepareFlag) (stmt *Stmt, tail string, err error) {
	defer c.arena.reset()
	stmtPtr := c.arena.new(ptrlen)
	tailPtr := c.arena.new(ptrlen)
	sqlPtr := c.arena.string(sql)

	if c.interrupted() {
		return nil, "", c.error(uint64(INTERRUPT))
	}
	r, err := c.api.prepare.Call(c.ctx, uint64(c.handle),
		uint64(sqlPtr), uint64(len(sql)+1), uint64(flags),
		uint64(stmtPtr), uint64(tailPtr))
	if err != nil {
		return nil, "", err
	}

	stmt = &Stmt{c: c}
	stmt.handle = c.mem.readUint32(stmtPtr)
	i := c.mem.readUint32(tailPtr)
	tail = sql[i-sqlPtr:]

	if err := c.error(r[0], sql); err != nil {
		return nil, "", err
	}
	if stmt.handle == 0 {
		return nil, "", nil
	}
	return
}

func (c *Conn) error(rc uint64, sql ...string) error {
	if rc == _OK {
		return nil
	}

	err := Error{code: rc}

	if err.Code() == NOMEM || err.ExtendedCode() == IOERR_NOMEM {
		panic(oomErr)
	}

	var r []uint64

	r, _ = c.api.errstr.Call(c.ctx, rc)
	if r != nil {
		err.str = c.mem.readString(uint32(r[0]), 512)
	}

	r, _ = c.api.errmsg.Call(c.ctx, uint64(c.handle))
	if r != nil {
		err.msg = c.mem.readString(uint32(r[0]), 512)
	}

	if sql != nil {
		r, _ = c.api.erroff.Call(c.ctx, uint64(c.handle))
		if r != nil && r[0] != math.MaxUint32 {
			err.sql = sql[0][r[0]:]
		}
	}

	switch err.msg {
	case err.str, "not an error":
		err.msg = ""
	}
	return &err
}

func (c *Conn) interrupted() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Conn) free(ptr uint32) {
	if ptr == 0 {
		return
	}
	_, err := c.api.free.Call(c.ctx, uint64(ptr))
	if err != nil {
		panic(err)
	}
}

func (c *Conn) new(size uint32) uint32 {
	r, err := c.api.malloc.Call(c.ctx, uint64(size))
	if err != nil {
		panic(err)
	}
	ptr := uint32(r[0])
	if ptr == 0 && size != 0 {
		panic(oomErr)
	}
	return ptr
}

func (c *Conn) newBytes(b []byte) uint32 {
	if b == nil {
		return 0
	}
	ptr := c.new(uint32(len(b)))
	c.mem.writeBytes(ptr, b)
	return ptr
}

func (c *Conn) newString(s string) uint32 {
	ptr := c.new(uint32(len(s) + 1))
	c.mem.writeString(ptr, s)
	return ptr
}

func (c *Conn) newArena(size uint32) arena {
	return arena{
		c:    c,
		size: size,
		base: c.new(size),
	}
}

type arena struct {
	c    *Conn
	base uint32
	next uint32
	size uint32
	ptrs []uint32
}

func (a *arena) reset() {
	for _, ptr := range a.ptrs {
		a.c.free(ptr)
	}
	a.ptrs = nil
	a.next = 0
}

func (a *arena) new(size uint32) uint32 {
	if a.next+size <= a.size {
		ptr := a.base + a.next
		a.next += size
		return ptr
	}
	ptr := a.c.new(size)
	a.ptrs = append(a.ptrs, ptr)
	return ptr
}

func (a *arena) string(s string) uint32 {
	ptr := a.new(uint32(len(s) + 1))
	a.c.mem.writeString(ptr, s)
	return ptr
}

package adiantum

import (
	"crypto/rand"

	"golang.org/x/crypto/argon2"
	"lukechampine.com/adiantum"
	"lukechampine.com/adiantum/hbsh"
)

const pepper = "github.com/ncruces/go-sqlite3/vfs/adiantum"

type adiantumCreator struct{}

func (adiantumCreator) HBSH(key []byte) *hbsh.HBSH {
	if len(key) != 32 {
		return nil
	}
	return adiantum.New(key)
}

func (adiantumCreator) KDF(text string) []byte {
	if text == "" {
		key := make([]byte, 32)
		n, _ := rand.Read(key)
		return key[:n]
	}
	return argon2.IDKey([]byte(text), []byte(pepper), 1, 64*1024, 4, 32)
}

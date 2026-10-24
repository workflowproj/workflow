package local

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Params are argon2id cost parameters. Hashes record the parameters they were made with, so
// raising them later rehashes each password the next time its owner signs in.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams follow the OWASP password storage recommendation for argon2id (19 MiB, two
// passes, one lane): roughly 20-50 ms on a server core. Hashing is limited to a few at a time
// (see hashSlots), which bounds memory under a burst of sign-ins.
var DefaultParams = Params{Memory: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

var hashSlots = make(chan struct{}, max(2, runtime.NumCPU()))

func derive(password string, salt []byte, p Params) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
}

// HashPassword returns an argon2id hash in the PHC string format:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash> (unpadded standard base64).
func HashPassword(password string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := derive(password, salt, p)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.Memory, p.Time, p.Threads, b64(salt), b64(key)), nil
}

type decoded struct {
	params Params
	salt   []byte
	key    []byte
}

func decodeHash(encoded string) (decoded, error) {
	var d decoded
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return d, errors.New("not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return d, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &d.params.Memory, &d.params.Time, &d.params.Threads); err != nil {
		return d, errors.New("bad argon2 parameters")
	}
	var err error
	if d.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return d, err
	}
	if d.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return d, err
	}
	// Refuse parameters that would let a tampered row exhaust the server.
	if d.params.Memory == 0 || d.params.Memory > 1<<20 || d.params.Time == 0 || d.params.Time > 20 || d.params.Threads == 0 ||
		len(d.salt) < 8 || len(d.key) < 16 || len(d.key) > 128 {
		return d, errors.New("argon2 parameters out of range")
	}
	d.params.SaltLen, d.params.KeyLen = uint32(len(d.salt)), uint32(len(d.key))
	return d, nil
}

// VerifyPassword reports whether password matches encoded (in constant time), and whether the
// hash should be replaced because it was made with parameters other than current.
func VerifyPassword(password, encoded string, current Params) (ok, rehash bool) {
	d, err := decodeHash(encoded)
	if err != nil {
		VerifyDummy(password)
		return false, false
	}
	key := derive(password, d.salt, d.params)
	if subtle.ConstantTimeCompare(key, d.key) != 1 {
		return false, false
	}
	return true, d.params != current
}

var dummy struct {
	once sync.Once
	hash string
}

// VerifyDummy spends the time a real verification takes, so an unknown account or one without a
// password answers as slowly as a wrong password.
func VerifyDummy(password string) {
	dummy.once.Do(func() {
		dummy.hash, _ = HashPassword("dummy password for timing", DefaultParams)
	})
	d, err := decodeHash(dummy.hash)
	if err == nil {
		_ = subtle.ConstantTimeCompare(derive(password, d.salt, d.params), d.key)
	}
}

// Package rank generates fractional, lexicographically ordered rank keys (LexoRank style),
// so moving an issue on a board updates a single row. It is a port of the
// fractional-indexing algorithm (https://observablehq.com/@dgreensp/implementing-fractional-indexing).
// Keys compare correctly with plain byte ordering (COLLATE "C").
package rank

import (
	"errors"
	"strings"
)

const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var smallestInteger = "A" + strings.Repeat("0", 26)

var ErrInvalidKey = errors.New("rank: invalid key")

// Between returns a key strictly between a and b. Empty strings mean "no bound".
func Between(a, b string) (string, error) {
	if a != "" {
		if err := validateKey(a); err != nil {
			return "", err
		}
	}
	if b != "" {
		if err := validateKey(b); err != nil {
			return "", err
		}
	}
	if a != "" && b != "" && a >= b {
		return "", errors.New("rank: lower bound must sort before upper bound")
	}
	if a == "" {
		if b == "" {
			return "a0", nil
		}
		ib, err := integerPart(b)
		if err != nil {
			return "", err
		}
		fb := b[len(ib):]
		if ib == smallestInteger {
			m, err := midpoint("", fb)
			return ib + m, err
		}
		if ib < b {
			return ib, nil
		}
		res, ok := decrementInteger(ib)
		if !ok {
			return "", errors.New("rank: cannot decrement any further")
		}
		return res, nil
	}
	ia, err := integerPart(a)
	if err != nil {
		return "", err
	}
	fa := a[len(ia):]
	if b == "" {
		i, ok := incrementInteger(ia)
		if !ok {
			m, err := midpoint(fa, "")
			return ia + m, err
		}
		return i, nil
	}
	ib, err := integerPart(b)
	if err != nil {
		return "", err
	}
	fb := b[len(ib):]
	if ia == ib {
		m, err := midpoint(fa, fb)
		return ia + m, err
	}
	i, ok := incrementInteger(ia)
	if !ok {
		return "", errors.New("rank: cannot increment any further")
	}
	if i < b {
		return i, nil
	}
	m, err := midpoint(fa, "")
	return ia + m, err
}

// After returns a key after a; Before returns a key before b.
func After(a string) (string, error)  { return Between(a, "") }
func Before(b string) (string, error) { return Between("", b) }

// midpoint returns a fraction string strictly between a and b ("" for b = unbounded).
func midpoint(a, b string) (string, error) {
	zero := digits[0]
	if b != "" && a >= b {
		return "", ErrInvalidKey
	}
	if (a != "" && a[len(a)-1] == zero) || (b != "" && b[len(b)-1] == zero) {
		return "", ErrInvalidKey
	}
	if b != "" {
		n := 0
		for {
			ca := zero
			if n < len(a) {
				ca = a[n]
			}
			if n >= len(b) || ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			m, err := midpoint(rest, b[n:])
			return b[:n] + m, err
		}
	}
	digitA := 0
	if a != "" {
		digitA = strings.IndexByte(digits, a[0])
	}
	digitB := len(digits)
	if b != "" {
		digitB = strings.IndexByte(digits, b[0])
	}
	if digitB-digitA > 1 {
		return string(digits[(digitA+digitB+1)/2]), nil
	}
	if b != "" && len(b) > 1 {
		return b[:1], nil
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	m, err := midpoint(rest, "")
	return string(digits[digitA]) + m, err
}

func integerLength(head byte) (int, error) {
	switch {
	case head >= 'a' && head <= 'z':
		return int(head-'a') + 2, nil
	case head >= 'A' && head <= 'Z':
		return int('Z'-head) + 2, nil
	}
	return 0, ErrInvalidKey
}

func integerPart(key string) (string, error) {
	n, err := integerLength(key[0])
	if err != nil {
		return "", err
	}
	if n > len(key) {
		return "", ErrInvalidKey
	}
	return key[:n], nil
}

func validateKey(key string) error {
	if key == smallestInteger {
		return ErrInvalidKey
	}
	for i := 0; i < len(key); i++ {
		if strings.IndexByte(digits, key[i]) < 0 {
			return ErrInvalidKey
		}
	}
	i, err := integerPart(key)
	if err != nil {
		return err
	}
	f := key[len(i):]
	if f != "" && f[len(f)-1] == digits[0] {
		return ErrInvalidKey
	}
	return nil
}

func incrementInteger(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	carry := true
	for i := len(digs) - 1; carry && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) + 1
		if d == len(digits) {
			digs[i] = digits[0]
		} else {
			digs[i] = digits[d]
			carry = false
		}
	}
	if !carry {
		return string(head) + string(digs), true
	}
	if head == 'Z' {
		return "a" + string(digits[0]), true
	}
	if head == 'z' {
		return "", false
	}
	h := head + 1
	if h > 'a' {
		digs = append(digs, digits[0])
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}

func decrementInteger(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	borrow := true
	for i := len(digs) - 1; borrow && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) - 1
		if d == -1 {
			digs[i] = digits[len(digits)-1]
		} else {
			digs[i] = digits[d]
			borrow = false
		}
	}
	if !borrow {
		return string(head) + string(digs), true
	}
	if head == 'a' {
		return "Z" + string(digits[len(digits)-1]), true
	}
	if head == 'A' {
		return "", false
	}
	h := head - 1
	if h < 'Z' {
		digs = append(digs, digits[len(digits)-1])
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}

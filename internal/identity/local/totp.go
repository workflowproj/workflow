package local

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

// TOTP (RFC 6238) with the parameters every authenticator app supports: HMAC-SHA1, 30-second
// steps, 6 digits. A code from the previous or next step is accepted for clock drift.
const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32, as authenticator apps expect.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

func decodeSecret(secret string) ([]byte, error) {
	return b32.DecodeString(strings.ToUpper(strings.ReplaceAll(strings.TrimRight(secret, "="), " ", "")))
}

// TOTPStep is the time step a moment falls in.
func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

func hotp(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// TOTPCode returns the code for the step containing t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, TOTPStep(t)), nil
}

// VerifyTOTP checks a code against the steps around t and returns the step it matched. Callers
// must refuse a step at or before the last one accepted (replay).
func VerifyTOTP(secret, code string, t time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	key, err := decodeSecret(secret)
	if err != nil || len(code) != totpDigits {
		return 0, false
	}
	now := TOTPStep(t)
	var matched int64
	found := 0
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		// Compare every candidate so the time taken does not depend on which one matched.
		if subtle.ConstantTimeCompare([]byte(hotp(key, now+d)), []byte(code)) == 1 && found == 0 {
			matched, found = now+d, 1
		}
	}
	return matched, found == 1
}

// LooksLikeTOTP distinguishes a 6-digit code from a recovery code.
func LooksLikeTOTP(code string) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// OTPAuthURI is what the QR code holds (the Key Uri Format understood by authenticator apps).
func OTPAuthURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// QRCodeDataURI renders text as a QR code: an SVG image as a data: URI, so the page can show it
// with an <img> and nothing about the secret leaves the server except to this browser.
func QRCodeDataURI(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String())), nil
}

// ---------------------------------------------------------------------------
// Recovery codes

const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no 0/o, 1/l/i

// NewRecoveryCodes returns n one-time codes such as "k7dmr-x3qpa" (about 49 bits each).
func NewRecoveryCodes(n int) []string {
	out := make([]string, n)
	buf := make([]byte, 10)
	for i := range out {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		var b strings.Builder
		for j, c := range buf {
			if j == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		out[i] = b.String()
	}
	return out
}

// HashRecoveryCode normalises what someone typed (case, spaces, dashes) and hashes it.
func HashRecoveryCode(code string) string {
	norm := strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return r
	}, code)
	sum := sha256.Sum256([]byte("wfm-recovery:" + norm))
	return hex.EncodeToString(sum[:])
}

package local

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
	"time"

	"rsc.io/qr/coding"
)

var fast = Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestArgon2Hashing(t *testing.T) {
	h, err := HashPassword("correct horse battery staple", DefaultParams)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`).MatchString(h) {
		t.Fatalf("unexpected PHC string %q", h)
	}
	if ok, rehash := VerifyPassword("correct horse battery staple", h, DefaultParams); !ok || rehash {
		t.Fatalf("verify = %v, rehash = %v", ok, rehash)
	}
	if ok, _ := VerifyPassword("correct horse battery stable", h, DefaultParams); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery staple", DefaultParams)
	if h2 == h {
		t.Fatal("salt not random")
	}

	// A hash made with other parameters still verifies, and asks to be upgraded.
	old, _ := HashPassword("an older password hash", fast)
	if ok, rehash := VerifyPassword("an older password hash", old, DefaultParams); !ok || !rehash {
		t.Fatalf("old params: verify = %v, rehash = %v", ok, rehash)
	}
	// Malformed or hostile hashes never verify (and never panic).
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1024,t=1,p=1$c2FsdHNhbHQ$aGFzaA", strings.Replace(h, "m=19456", "m=99999999", 1), strings.Replace(h, "v=19", "v=16", 1)} {
		if ok, _ := VerifyPassword("x", bad, DefaultParams); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDefaultParamsAreSensible(t *testing.T) {
	// OWASP's minimum for argon2id: 19 MiB, 2 iterations, 1 lane.
	if DefaultParams.Memory < 19*1024 || DefaultParams.Time < 2 || DefaultParams.Threads < 1 || DefaultParams.SaltLen < 16 || DefaultParams.KeyLen < 32 {
		t.Fatalf("weak parameters %+v", DefaultParams)
	}
	start := time.Now()
	if _, err := HashPassword("timing check password", DefaultParams); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("hashing took %s", d)
	}
}

func TestPasswordPolicy(t *testing.T) {
	email, name := "ada.lovelace@analytical.example.org", "Ada Lovelace"
	cases := []struct {
		pw      string
		ok      bool
		problem string
	}{
		{"short", false, "at least 12"},
		{strings.Repeat("x", 12) + "y", false, "repeating"},
		{"password1234", false, "common"},
		{"QWERTYUIOP123", false, "common"},
		{"Summer2024!!!!", false, "common"},
		{"P@ssw0rd2024!", false, "common"},
		{"iloveyou1234567", false, "common"},
		{"abcabcabcabcabc", false, "repeating"},
		{"ßßßßßßßßßßßßßß", false, "repeating"},
		{"abcdefghijklmn", false, "sequences"},
		{"987654321098", false, "sequences"},
		{"567890123456", false, "sequences"},
		{"ada.lovelace-rocks", false, "name or email"},
		{"my lovelace passphrase", false, "name or email"},
		{"analytical-engine-77", false, "name or email"},
		{strings.Repeat("abcdefgh1", 15), false, "at most 128"},
		{"correct horse battery staple", true, ""},
		{"Tr0ub4dor&3-zebra", true, ""},
		{"vT7#qL9!mZ2$wR", true, ""},
		{"pâtisserie à côté du métro", true, ""},
	}
	for _, c := range cases {
		st := CheckPassword(c.pw, email, name)
		if st.OK() != c.ok {
			t.Errorf("%q: ok = %v, want %v (%v)", c.pw, st.OK(), c.ok, st.Problems)
			continue
		}
		if !c.ok && !strings.Contains(strings.ToLower(strings.Join(st.Problems, " ")), c.problem) {
			t.Errorf("%q: problems %v, want one about %q", c.pw, st.Problems, c.problem)
		}
		if c.ok != (st.Score > 0) {
			t.Errorf("%q: score %d", c.pw, st.Score)
		}
	}
	// Free-mail domains are not treated as personal words.
	if st := CheckPassword("gmail-is-where-my-mail-lives", "someone@gmail.com", "Someone Else"); !st.OK() {
		t.Errorf("free mail domain flagged: %v", st.Problems)
	}
	// Longer and more varied passwords score higher.
	weak := CheckPassword("plainlowercase", "", "").Score
	strong := CheckPassword("Plain-Lowercase 47 With Spaces!", "", "").Score
	if !(weak < strong && strong == 4) {
		t.Errorf("scores weak=%d strong=%d", weak, strong)
	}
	if got := CheckPassword("x", "", "").Label(); got != "Not allowed" {
		t.Errorf("label = %q", got)
	}
}

func TestCommonListLoaded(t *testing.T) {
	if n := len(common()); n < 3000 {
		t.Fatalf("common password list has %d entries", n)
	}
	if _, ok := common()["qwertyuiopasdfghjkl"]; !ok {
		// Long entries are kept verbatim, lower-cased.
		if _, ok := common()["1qaz2wsx3edc"]; !ok {
			t.Fatal("expected well-known long entries")
		}
	}
}

// RFC 6238 appendix B, SHA-1 vectors (the last six digits of the eight-digit values).
func TestTOTPVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		got, err := TOTPCode(secret, time.Unix(unix, 0))
		if err != nil || got != want {
			t.Errorf("T=%d: %q (%v), want %q", unix, got, err, want)
		}
	}
}

func TestTOTPVerify(t *testing.T) {
	secret := NewTOTPSecret()
	if len(secret) != 32 {
		t.Fatalf("secret %q", secret)
	}
	now := time.Unix(1_700_000_000, 0)
	for _, d := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		code, _ := TOTPCode(secret, now.Add(d))
		step, ok := VerifyTOTP(secret, code, now)
		if !ok || step != TOTPStep(now.Add(d)) {
			t.Errorf("drift %s: ok=%v step=%d", d, ok, step)
		}
	}
	old, _ := TOTPCode(secret, now.Add(-90*time.Second))
	if _, ok := VerifyTOTP(secret, old, now); ok {
		t.Error("code from three steps ago accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := VerifyTOTP(secret, bad, now); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	code, _ := TOTPCode(secret, now)
	if _, ok := VerifyTOTP(strings.ToLower(secret), " "+code[:3]+" "+code[3:], now); !ok {
		t.Error("spaces and lower-case secret should be tolerated")
	}
	if !LooksLikeTOTP("123 456") || LooksLikeTOTP("k7dmr-x3qpa") || LooksLikeTOTP("12345") {
		t.Error("LooksLikeTOTP")
	}
}

func TestOTPAuthURIAndQRCode(t *testing.T) {
	uri := OTPAuthURI("Workflow (wfm.example.com)", "ada@example.com", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/Workflow%20%28wfm.example.com%29:ada@example.com?issuer=Workflow+%28wfm.example.com%29&secret=JBSWY3DPEHPK3PXP"
	if uri != want {
		t.Fatalf("uri = %s", uri)
	}
	img, err := QRCodeDataURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(img, "data:image/svg+xml;base64,"))
	if err != nil || !strings.HasPrefix(img, "data:image/svg+xml;base64,") {
		t.Fatalf("data URI: %v", err)
	}
	svg := string(raw)
	if !strings.HasPrefix(svg, "<svg xmlns=") || !strings.HasSuffix(svg, "</svg>") || strings.Count(svg, "h1v1h-1z") < 200 {
		t.Fatalf("svg looks wrong: %.200s", svg)
	}
}

func TestQRMaskChoice(t *testing.T) {
	// An all-light 21x21 grid: every row and column is one 21-module run (42 lines x 19), every
	// 2x2 block counts (400 x 3) and it is 0% dark (10 x 10).
	blank := &coding.Code{Bitmap: make([]byte, 21*3), Size: 21, Stride: 3}
	if got := qrPenalty(blank); got != 42*19+400*3+100 {
		t.Fatalf("penalty of a blank grid = %d", got)
	}
	text := OTPAuthURI("Workflow (workflow.example-company.com)", "firstname.lastname@example-company.com", NewTOTPSecret())
	best, err := qrEncode(text)
	if err != nil {
		t.Fatal(err)
	}
	v := coding.Version((best.Size - 17) / 4)
	if coding.String(text).Bits(v-1) <= (v-1).DataBytes(coding.M)*8 {
		t.Fatalf("version %d is not the smallest that fits", v)
	}
	for m := coding.Mask(0); m < 8; m++ {
		p, _ := coding.NewPlan(v, coding.M, m)
		c, err := p.Encode(coding.String(text))
		if err != nil {
			t.Fatal(err)
		}
		if qrPenalty(c) < qrPenalty(best) {
			t.Fatalf("mask %d scores %d, below the chosen %d", m, qrPenalty(c), qrPenalty(best))
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes(10)
	seen := map[string]bool{}
	for _, c := range codes {
		if !regexp.MustCompile(`^[a-z2-9]{5}-[a-z2-9]{5}$`).MatchString(c) || strings.ContainsAny(c, "01ilo") {
			t.Errorf("code %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate %q", c)
		}
		seen[c] = true
	}
	c := codes[0]
	if HashRecoveryCode(c) != HashRecoveryCode(" "+strings.ToUpper(strings.ReplaceAll(c, "-", " "))+" ") {
		t.Error("normalisation")
	}
	if HashRecoveryCode(codes[0]) == HashRecoveryCode(codes[1]) {
		t.Error("hash collision")
	}
}

func TestTemporaryPassword(t *testing.T) {
	for range 20 {
		pw := temporaryPassword()
		if !regexp.MustCompile(`^[A-Za-z2-9]{4}(-[A-Za-z2-9]{4}){3}$`).MatchString(pw) || !CheckPassword(pw, "", "").OK() {
			t.Fatalf("temporary password %q", pw)
		}
	}
}

func TestPhantomLocks(t *testing.T) {
	p := phantomLocks{m: map[string]*phantom{}}
	now := time.Now()
	for i := range 3 {
		if p.lockedFor("x@example.com", now) > 0 {
			t.Fatalf("locked after %d failures", i)
		}
		p.fail("x@example.com", now, 3, 15*time.Minute)
	}
	if d := p.lockedFor("x@example.com", now); d != 15*time.Minute {
		t.Fatalf("locked for %s", d)
	}
	if p.lockedFor("x@example.com", now.Add(16*time.Minute)) != 0 {
		t.Fatal("lock did not expire")
	}
}

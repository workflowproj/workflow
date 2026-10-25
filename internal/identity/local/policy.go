package local

import (
	_ "embed"
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Password length limits, in characters. The maximum bounds hashing work, not strength.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 128
)

//go:embed common-passwords.txt
var commonList string

var common = sync.OnceValue(func() map[string]struct{} {
	m := make(map[string]struct{}, 4500)
	for _, line := range strings.Split(commonList, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			m[line] = struct{}{}
		}
	}
	return m
})

// Strength is a password's policy verdict and an estimate of how hard it is to guess.
type Strength struct {
	// Score: 0 not allowed, 1 weak, 2 fair, 3 good, 4 strong.
	Score int `json:"score"`
	// Problems are the policy rules the password breaks, as sentences for people.
	Problems []string `json:"problems"`
}

// OK reports whether the password may be used.
func (s Strength) OK() bool { return len(s.Problems) == 0 }

// Label names the score.
func (s Strength) Label() string {
	return [...]string{"Not allowed", "Weak", "Fair", "Good", "Strong"}[s.Score]
}

// freeMail domains are not "context" a password should avoid (everyone has them).
var freeMail = map[string]bool{"gmail": true, "googlemail": true, "outlook": true, "hotmail": true, "live": true, "yahoo": true, "icloud": true, "proton": true, "protonmail": true, "example": true}

// CheckPassword applies the password policy: 12 to 128 characters, not a very common password
// (or a common word padded with digits and symbols, or l33t-speak for one), not one character
// or a short pattern repeated, and not containing the person's email address or name.
func CheckPassword(password, email, name string) Strength {
	var problems []string
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		problems = append(problems, "Use at least 12 characters.")
	case n > MaxPasswordLength:
		problems = append(problems, "Use at most 128 characters.")
	}
	lower := strings.ToLower(password)
	if n >= MinPasswordLength {
		if isCommon(lower) {
			problems = append(problems, "This is one of the most common passwords; choose something less predictable.")
		} else if repetitive(lower) {
			problems = append(problems, "Avoid repeating a character or a short pattern.")
		} else if sequential(lower) {
			problems = append(problems, "Avoid sequences such as abcd… or 1234….")
		}
	}
	if word := personalWord(lower, email, name); word != "" {
		problems = append(problems, "Don't use your name or email address in your password.")
	}
	s := Strength{Problems: problems}
	if s.OK() {
		s.Score = estimate(password)
	}
	if s.Problems == nil {
		s.Problems = []string{}
	}
	return s
}

var leet = strings.NewReplacer("@", "a", "4", "a", "8", "b", "3", "e", "6", "g", "1", "i", "!", "i", "0", "o", "$", "s", "5", "s", "7", "t", "+", "t", "2", "z")

// isCommon matches the list as is, and the word left after stripping leading and trailing
// digits and symbols (also read as l33t-speak): "P@ssw0rd2024!!" is "password".
func isCommon(lower string) bool {
	list := common()
	if _, ok := list[lower]; ok {
		return true
	}
	trim := func(s string) string {
		return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
	}
	for _, core := range []string{trim(lower), trim(leet.Replace(lower)), leet.Replace(trim(lower))} {
		if utf8.RuneCountInString(core) < 4 {
			continue
		}
		if _, ok := list[core]; ok {
			return true
		}
	}
	return false
}

// repetitive: few distinct characters, or the whole password is a shorter unit repeated.
func repetitive(lower string) bool {
	runes := []rune(lower)
	distinct := map[rune]bool{}
	for _, r := range runes {
		distinct[r] = true
	}
	if len(distinct) < 5 {
		return true
	}
	for unit := 1; unit <= len(runes)/2; unit++ {
		if len(runes)%unit != 0 {
			continue
		}
		same := true
		for i := unit; i < len(runes) && same; i++ {
			same = runes[i] == runes[i%unit]
		}
		if same {
			return true
		}
	}
	return false
}

// sequential: the password is one run of consecutive characters ("abcdefghijkl", and digits
// wrapping round, "567890123456" or "987654321098").
func sequential(lower string) bool {
	runes := []rune(lower)
	diff := func(a, b rune) rune {
		if a >= '0' && a <= '9' && b >= '0' && b <= '9' {
			if d := (b - a + 10) % 10; d == 9 {
				return -1
			} else {
				return d
			}
		}
		return b - a
	}
	step := diff(runes[0], runes[1])
	if step != 1 && step != -1 {
		return false
	}
	for i := 2; i < len(runes); i++ {
		if diff(runes[i-1], runes[i]) != step {
			return false
		}
	}
	return true
}

// personalWord returns the part of the person's email or name the password contains, if any:
// the whole local part, or any word of 4+ letters from the name, the local part or the
// organisation's domain.
func personalWord(lower, email, name string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	var words []string
	if at := strings.LastIndexByte(email, '@'); at > 0 {
		local, domain := email[:at], email[at+1:]
		if len(local) >= 3 {
			if strings.Contains(lower, local) {
				return local
			}
		}
		words = append(words, splitWords(local)...)
		if label, _, _ := strings.Cut(domain, "."); !freeMail[label] {
			words = append(words, splitWords(label)...)
		}
	}
	words = append(words, splitWords(strings.ToLower(name))...)
	for _, w := range words {
		if utf8.RuneCountInString(w) >= 4 && strings.Contains(lower, w) {
			return w
		}
	}
	return ""
}

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// estimate converts a rough entropy estimate to a 1-4 score. Runs of repeated or consecutive
// characters count once, so "aaaa" and "1234" add little.
func estimate(password string) int {
	runes := []rune(password)
	var lower, upper, digit, symbol, other bool
	effective := 0.0
	for i, r := range runes {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		case r < 128:
			symbol = true
		default:
			other = true
		}
		if i > 0 {
			d := r - runes[i-1]
			if d >= -1 && d <= 1 {
				effective += 0.25
				continue
			}
		}
		effective++
	}
	pool := 0
	for _, c := range []struct {
		on   bool
		size int
	}{{lower, 26}, {upper, 26}, {digit, 10}, {symbol, 33}, {other, 100}} {
		if c.on {
			pool += c.size
		}
	}
	bits := effective * math.Log2(float64(max(pool, 2)))
	switch {
	case bits < 45:
		return 1
	case bits < 60:
		return 2
	case bits < 80:
		return 3
	}
	return 4
}

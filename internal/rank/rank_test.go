package rank

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestKnownValues(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "", "a0"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"a0", "a1", "a0V"},
		{"a1", "a2", "a1V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"Zz", "a1", "a0"},
		{"", "Y00", "Xzzz"},
		{"bzz", "", "c000"},
		{"a0", "a0V", "a0G"},
		{"a0", "a0G", "a08"},
		{"b125", "b129", "b127"},
		{"a0", "a1V", "a1"},
		{"Zz", "a01", "a0"},
		{"", "a0V", "a0"},
		{"", "b999", "b99"},
	}
	for _, c := range cases {
		got, err := Between(c.a, c.b)
		if err != nil {
			t.Fatalf("Between(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Between(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestInvalid(t *testing.T) {
	for _, c := range [][2]string{{"a0", "a0"}, {"a1", "a0"}, {"a00", ""}, {"0", ""}, {"a1#", ""}, {smallestInteger, ""}} {
		if _, err := Between(c[0], c[1]); err == nil {
			t.Errorf("Between(%q, %q) should fail", c[0], c[1])
		}
	}
}

// Random inserts must always produce a strictly increasing, duplicate-free sequence.
func TestRandomInsertsStayOrdered(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	keys := []string{}
	for i := 0; i < 5000; i++ {
		pos := r.IntN(len(keys) + 1)
		lo, hi := "", ""
		if pos > 0 {
			lo = keys[pos-1]
		}
		if pos < len(keys) {
			hi = keys[pos]
		}
		k, err := Between(lo, hi)
		if err != nil {
			t.Fatalf("Between(%q, %q): %v", lo, hi, err)
		}
		if (lo != "" && k <= lo) || (hi != "" && k >= hi) {
			t.Fatalf("Between(%q, %q) = %q is out of order", lo, hi, k)
		}
		keys = slices.Insert(keys, pos, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys not sorted")
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
}

// Appending repeatedly (the common "create issue" path) keeps keys short.
func TestAppendKeepsKeysShort(t *testing.T) {
	k := ""
	for i := 0; i < 100000; i++ {
		next, err := After(k)
		if err != nil {
			t.Fatal(err)
		}
		k = next
	}
	if len(k) > 6 {
		t.Fatalf("key after 100k appends is %q (%d chars)", k, len(k))
	}
}

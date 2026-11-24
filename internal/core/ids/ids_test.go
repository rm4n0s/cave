package ids

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New("p_")
		if !Valid("p_", id) {
			t.Fatalf("New gave %q, which Valid refuses", id)
		}
		if seen[id] {
			t.Fatalf("New gave %q twice", id)
		}
		seen[id] = true
	}
	if id := New("c_"); !strings.HasPrefix(id, "c_") || len(id) != 18 {
		t.Errorf("New(c_) = %q", id)
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		prefix, id string
		want       bool
	}{
		{"p_", "p_0123456789abcdef", true},
		{"k_", "k_ffffffffffffffff", true},
		{"p_", "k_0123456789abcdef", false}, // another prefix
		{"p_", "p_0123456789abcde", false},  // too short
		{"p_", "p_0123456789abcdef0", false},
		{"p_", "p_0123456789ABCDEF", false}, // capitals
		{"p_", "p_0123456789abcdeg", false},
		{"p_", "", false},
		{"p_", "p_", false},
		{"p_", "0123456789abcdefp_", false},
	}
	for _, tt := range tests {
		if got := Valid(tt.prefix, tt.id); got != tt.want {
			t.Errorf("Valid(%q, %q) = %v, want %v", tt.prefix, tt.id, got, tt.want)
		}
	}
}

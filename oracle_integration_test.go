//go:build oracle

package gplus

import "testing"

func TestOracle_QuoteColumn(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"name", "name"},
		{"users.name", "users.name"},
		{"users.name AS u_name", "users.name AS u_name"},
		{"count(id)", "count(id)"},
		{"users.*", "users.*"},
		{"", ""},
	}

	for _, c := range cases {
		got := quoteColumn(c.input, "", "")
		if got != c.want {
			t.Errorf("quoteColumn(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

package dashboard

import (
	"bytes"
	"testing"
)

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want int
	}{
		{"empty", []byte{}, 0},
		{"one line no newline", []byte("hello"), 1},
		{"one line with newline", []byte("hello\n"), 1},
		{"two lines", []byte("a\nb\n"), 2},
		{"blank line skipped", []byte("a\n\nb\n"), 2},
		{"trailing content without newline", []byte("a\nb"), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitLines(c.in)
			if len(got) != c.want {
				t.Errorf("splitLines(%q) got %d lines, want %d: %q", c.in, len(got), c.want, got)
			}
		})
	}
}

func TestSplitLinesPreservesContent(t *testing.T) {
	got := splitLines([]byte("alpha\nbeta\ngamma\n"))
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3", len(got))
	}
	want := [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma")}
	for i, w := range want {
		if !bytes.Equal(got[i], w) {
			t.Errorf("line %d = %q, want %q", i, got[i], w)
		}
	}
}

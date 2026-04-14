package memory

import "testing"

func TestStripPrivate(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no tags", "hello world", "hello world"},
		{"simple", "before <private>secret</private> after", "before  after"},
		{"multiline", "a\n<private>line1\nline2</private>\nb", "a\n\nb"},
		{"case-insensitive", "a <PRIVATE>x</Private> b", "a  b"},
		{"multiple blocks", "<private>a</private> mid <private>b</private>", " mid "},
		{"entire content private", "<private>everything</private>", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripPrivate(c.in); got != c.want {
				t.Errorf("StripPrivate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

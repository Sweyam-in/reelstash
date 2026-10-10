package slug

import "testing"

func TestName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "   ", want: ""},
		{name: "already a slug", in: "drama", want: "drama"},
		{name: "mixed case and spaces", in: "  Sci Fi  ", want: "sci-fi"},
		{name: "punctuation collapses", in: "Sci-Fi & Fantasy", want: "sci-fi-fantasy"},
		{name: "digits stay", in: "Top 10", want: "top-10"},
		{name: "leading punctuation", in: "...Hello!!!", want: "hello"},
		{name: "unicode is a separator", in: "Café Noir", want: "caf-noir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Name(tt.in); got != tt.want {
				t.Errorf("Name(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

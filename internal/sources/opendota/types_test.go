package opendota

import "testing"

func TestSanitizeDisplayName(t *testing.T) {
	long := ""
	for range maxDisplayName + 10 {
		long += "a"
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Dendi", "Dendi"},
		{"non latin kept", "thunder雷", "thunder雷"},
		{"spaces kept inside", "h8 me", "h8 me"},
		{"trimmed", "  Pain  ", "Pain"},
		{"escape stripped", "\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"newlines stripped", "two\nlines\r\n", "twolines"},
		{"nul stripped", "na\x00me", "name"},
		{"tab becomes space", "a\tb", "a b"},
		{"truncated", long, long[:maxDisplayName]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDisplayName(tt.in); got != tt.want {
				t.Errorf("sanitizeDisplayName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

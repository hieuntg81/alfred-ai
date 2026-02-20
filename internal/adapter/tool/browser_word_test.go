package tool

import "testing"

func TestContainsWord(t *testing.T) {
	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		// Should match: standalone "fs." at start.
		{"fs.readFileSync('/etc/passwd')", "fs.", true},
		// Should match: after dot (non-alphanumeric).
		{"obj.fs.writeFile('x','y')", "fs.", true},
		// Should match: after open paren.
		{"(fs.unlink('/tmp/x'))", "fs.", true},
		// Should match: after space.
		{"var x = fs.statSync('.')", "fs.", true},
		// Should match: after semicolon.
		{";fs.readFile('x')", "fs.", true},
		// Should match: after newline.
		{"\nfs.existsSync('x')", "fs.", true},
		// Should match: after equals.
		{"x=fs.createReadStream('y')", "fs.", true},

		// Should NOT match: preceded by letter (refs, prefs, buffs, dfs...).
		{"this.refs.current", "fs.", false},
		{"prefs.theme", "fs.", false},
		{"buffs.length", "fs.", false},
		{"dfs.search()", "fs.", false},
		{"leafs.forEach(fn)", "fs.", false},
		{"clefs.sort()", "fs.", false},

		// Should NOT match: preceded by digit.
		{"var x2fs.y = 1", "fs.", false},

		// Should NOT match: preceded by underscore.
		{"_fs.read()", "fs.", false},

		// Edge: empty string.
		{"", "fs.", false},
		// Edge: pattern only.
		{"fs.", "fs.", true},
		// Edge: pattern at very end.
		{"xfs.", "fs.", false},
		{"x.fs.", "fs.", true},
	}
	for _, tt := range tests {
		got := containsWord(tt.s, tt.pattern)
		if got != tt.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
		}
	}
}

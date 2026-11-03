package size

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]int64{"512": 512, "4K": 4096, "4Ki": 4096, " 2M ": 2 << 20, "1G": 1 << 30} {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v, want %d", in, got, err, want)
		}
	}
}

// FuzzParse checks that Parse doesn't panic and that a parsed size
// survives Format and Parse again.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"512", "4K", "4Ki", "16M", "1G"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := Parse(s)
		if err != nil {
			return
		}
		back, err := Parse(Format(n))
		if err != nil || back != n {
			t.Fatalf("Parse(Format(%d)) = %d, %v", n, back, err)
		}
	})
}

package config

import "testing"

func TestParseAPIKeys(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{raw: "", want: nil},
		{raw: "a", want: []string{"a"}},
		{raw: "a,b,c", want: []string{"a", "b", "c"}},
		{raw: " a , b ,,c ", want: []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		keys := parseAPIKeys(c.raw)
		if len(keys) != len(c.want) {
			t.Fatalf("parseAPIKeys(%q) = %v, want %v", c.raw, keys, c.want)
		}
		for _, k := range c.want {
			if !keys[k] {
				t.Errorf("parseAPIKeys(%q) missing key %q", c.raw, k)
			}
		}
	}
}

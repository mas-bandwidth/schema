package gotable

import (
	"slices"
	"testing"
)

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
fixed table Root {
	title string(12)
	score int32
}
`)
	all, err := GenerateLineageSurfaces(u, nil, All)
	if err != nil {
		t.Fatal(err)
	}
	named, err := GenerateLineageSurfaces(u, nil, mustSurfaces(t, "wire,fixed,json,cook,block,view,message,retain"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keys(all), keys(named)) {
		t.Fatalf("default all surfaces differ from the explicit full list:\nall=%v\nnamed=%v", keys(all), keys(named))
	}

	cases := []struct {
		list     string
		have     []string
		lacking  []string
	}{
		{"block", []string{"Block.go"}, []string{"TableJson.go", "Cook.go", "View.go"}},
		{"json", []string{"TableJson.go"}, []string{"Cook.go", "Block.go"}},
		{"cook", []string{"Cook.go"}, []string{"TableJson.go", "Block.go"}},
		{"view", []string{"View.go"}, []string{"TableJson.go", "Cook.go", "Block.go"}},
	}
	for _, tc := range cases {
		s := mustSurfaces(t, tc.list)
		got, err := GenerateLineageSurfaces(u, nil, s)
		if err != nil {
			t.Fatalf("--surface %s: %v", tc.list, err)
		}
		for _, name := range tc.have {
			if !hasFile(got, name) {
				t.Fatalf("--surface %s: expected a %s file, got %v", tc.list, name, keys(got))
			}
		}
		for _, name := range tc.lacking {
			if hasFile(got, name) {
				t.Fatalf("--surface %s: %s should not be emitted, got %v", tc.list, name, keys(got))
			}
		}
	}
}

func mustSurfaces(t *testing.T, list string) Surfaces {
	t.Helper()
	s, err := ParseSurfaces(list)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hasFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if len(name) >= len(suffix) && name[len(name)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func keys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

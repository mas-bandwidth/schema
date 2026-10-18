package tablenames

import (
	"testing"
)

// TestEveryPortBackendBitIsItsOwn checks the backend-bit allocation in
// tablenames.go: each backend this tree defines is exactly the bit the
// allocation gives it, and no two backends share a bit.
func TestEveryPortBackendBitIsItsOwn(t *testing.T) {
	type alloc struct {
		name string
		b    Backend
		bit  int // the shift, 0-based: 1 << bit
	}
	// The allocation in tablenames.go: Cpp 0, Cs 1, Rust 2, Go 3,
	// Java 4, C 5, Elixir 6, Js 7, Dart 8, Lua 9, Luau 10,
	// Squirrel 11. Lua and Luau are on other streams, not defined in
	// this tree, so they are not tested here.
	allocs := []alloc{
		{"Cpp", Cpp, 0},
		{"Cs", Cs, 1},
		{"Rust", Rust, 2},
		{"Go", Go, 3},
		{"Java", Java, 4},
		{"C", C, 5},
		{"Elixir", Elixir, 6},
		{"Js", Js, 7},
		{"Dart", Dart, 8},
		{"Squirrel", Squirrel, 11},
	}
	seen := map[Backend]string{}
	for _, a := range allocs {
		want := Backend(1) << uint(a.bit)
		if a.b != want {
			t.Errorf("%s is 1<<%d, want 1<<%d", a.name, bitShift(a.b), a.bit)
		}
		if prev, ok := seen[a.b]; ok {
			t.Errorf("%s is 1<<%d, same bit as %s", a.name, bitShift(a.b), prev)
		}
		seen[a.b] = a.name
	}
}

func bitShift(b Backend) int {
	for i := 0; i < 32; i++ {
		if b&(1<<uint(i)) != 0 {
			return i
		}
	}
	return -1
}

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
	// Java 4, C 5, Elixir 6, Js 7, Dart 8, Lua 9, Luau 10, Moo 11,
	// Squirrel 12, Python 13, Unity 14, Ruby 15.
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
		{"Lua", Lua, 9},
		{"Luau", Luau, 10},
		{"Moo", Moo, 11},
		{"Squirrel", Squirrel, 12},
		{"Python", Python, 13},
		{"Unity", Unity, 14},
		{"Ruby", Ruby, 15},
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

// TestBackendHasRoomForTheNextPort holds the allocation's promise that a new
// port takes the next bit after the highest one used: with Ruby at 15 that is
// 16, and Backend must be wide enough to hold it (review 2026-11-21,
// TABLETEXT-10: a uint16 Backend had no next bit).
func TestBackendHasRoomForTheNextPort(t *testing.T) {
	highest := bitShift(Ruby)
	for _, b := range []Backend{Cpp, Cs, Rust, Go, Java, C, Elixir, Js, Dart, Lua, Luau, Moo, Squirrel, Python, Unity} {
		highest = max(highest, bitShift(b))
	}
	next := uint(highest + 1)
	if Backend(1)<<next == 0 {
		t.Fatalf("the next port's bit, 1<<%d, does not fit in Backend", next)
	}
}

func bitShift(b Backend) int {
	for i := range 32 {
		if b&(1<<uint(i)) != 0 {
			return i
		}
	}
	return -1
}

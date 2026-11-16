package main

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"

	"blockdemo"
)

func fnv1a64(parts ...[]byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, part := range parts {
		for _, v := range part {
			h ^= uint64(v)
			h *= 0x100000001b3
		}
	}
	return h
}

// The prospective v2 identity includes the checked writer definitions after
// the complete layout. Raw layout-only hashes remain explicit legacy evidence;
// they cannot stand in for the current generated writer key.
func w12Definitions(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(w13RepoRoot(t), "tables", "block")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string][]byte)
	for _, name := range []string{"PaddedRow", "RenderCamera"} {
		st := u.Tables[name]
		if st == nil {
			t.Fatalf("W12: checked writer %s missing", name)
		}
		out[name] = ir.TableFixedDefinitionsDigest(st)
	}
	return out
}

func TestRowW12(t *testing.T) {
	definitions := w12Definitions(t)
	cases := []struct {
		name       string
		layout     []byte
		hash       uint64
		legacyHash uint64
	}{
		{"PaddedRow", blockdemo.PaddedRowFixedLayout, blockdemo.PaddedRowFixedHash, 0x9683695abec5c762},
		{"RenderCamera", blockdemo.RenderCameraFixedLayout, blockdemo.RenderCameraFixedHash, 0x74ade5c3866f68d1},
	}

	for _, c := range cases {
		if len(c.layout) < 4 {
			t.Fatalf("%s: layout too short", c.name)
		}
		count := int(c.layout[0]) | int(c.layout[1])<<8 | int(c.layout[2])<<16 | int(c.layout[3])<<24
		if count <= 0 {
			t.Fatalf("%s: expected positive entry count, got %d", c.name, count)
		}

		if legacy := fnv1a64(c.layout); legacy != c.legacyHash {
			t.Fatalf("%s: frozen legacy layout hash = 0x%016x, want 0x%016x", c.name, legacy, c.legacyHash)
		}
		got := fnv1a64(c.layout, definitions[c.name])
		if got != c.hash {
			t.Fatalf("%s: fnv1a64 with count and definitions: got 0x%016x, want 0x%016x", c.name, got, c.hash)
		}

		t.Logf("PASS %s: layout=%d bytes, count=%d (0x%02x%02x%02x%02x LE), hash=0x%016x",
			c.name, len(c.layout), count, c.layout[0], c.layout[1], c.layout[2], c.layout[3], got)
	}
}

func TestRowW12NegativeControl(t *testing.T) {
	definitions := w12Definitions(t)
	cases := []struct {
		name       string
		layout     []byte
		hash       uint64
		legacyHash uint64
	}{
		{"PaddedRow", blockdemo.PaddedRowFixedLayout, blockdemo.PaddedRowFixedHash, 0x9683695abec5c762},
		{"RenderCamera", blockdemo.RenderCameraFixedLayout, blockdemo.RenderCameraFixedHash, 0x74ade5c3866f68d1},
	}

	for _, c := range cases {
		withCount := fnv1a64(c.layout, definitions[c.name])
		withoutCount := fnv1a64(c.layout[4:], definitions[c.name])

		if withCount != c.hash {
			t.Fatalf("%s: positive control hash 0x%016x differs from generated 0x%016x", c.name, withCount, c.hash)
		}
		if fnv1a64(c.layout[4:]) == c.legacyHash {
			t.Fatalf("%s: legacy negative control FAILED: layout hash without count matched the frozen key", c.name)
		}
		if withCount == withoutCount {
			t.Fatalf("%s: negative control FAILED: hash must change when the 4-byte count is excluded", c.name)
		}
		if withoutCount == c.hash {
			t.Fatalf("%s: negative control FAILED: hash without count matched baked hash", c.name)
		}
		t.Logf("CONTROL %s: hash_with_count=0x%016x != hash_without_count=0x%016x",
			c.name, withCount, withoutCount)
	}
}

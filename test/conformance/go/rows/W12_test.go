package main

import (
	"testing"

	"blockdemo"
)

func fnv1a64(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range b {
		h ^= uint64(v)
		h *= 0x100000001b3
	}
	return h
}

func TestRowW12(t *testing.T) {
	cases := []struct {
		name   string
		layout []byte
		hash   uint64
	}{
		{"PaddedRow", blockdemo.PaddedRowFixedLayout, blockdemo.PaddedRowFixedHash},
		{"RenderCamera", blockdemo.RenderCameraFixedLayout, blockdemo.RenderCameraFixedHash},
	}

	for _, c := range cases {
		if len(c.layout) < 4 {
			t.Fatalf("%s: layout too short", c.name)
		}
		count := int(c.layout[0]) | int(c.layout[1])<<8 | int(c.layout[2])<<16 | int(c.layout[3])<<24
		if count <= 0 {
			t.Fatalf("%s: expected positive entry count, got %d", c.name, count)
		}

		got := fnv1a64(c.layout)
		if got != c.hash {
			t.Fatalf("%s: fnv1a64 with count: got 0x%016x, want 0x%016x", c.name, got, c.hash)
		}

		t.Logf("PASS %s: layout=%d bytes, count=%d (0x%02x%02x%02x%02x LE), hash=0x%016x",
			c.name, len(c.layout), count, c.layout[0], c.layout[1], c.layout[2], c.layout[3], got)
	}
}

func TestRowW12NegativeControl(t *testing.T) {
	cases := []struct {
		name   string
		layout []byte
		hash   uint64
	}{
		{"PaddedRow", blockdemo.PaddedRowFixedLayout, blockdemo.PaddedRowFixedHash},
		{"RenderCamera", blockdemo.RenderCameraFixedLayout, blockdemo.RenderCameraFixedHash},
	}

	for _, c := range cases {
		withCount := fnv1a64(c.layout)
		withoutCount := fnv1a64(c.layout[4:])

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

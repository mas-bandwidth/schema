// Dump expected hash inputs for the in-tree schemas our row tests cite.
//
// Run with `go run working/digesthash.go >/tmp/dump.txt` from the repo root.
// The output is the C constants and byte arrays we hand our row tests — they
// cite what IR says the digest must be, byte for byte, so a C emitter that
// folded the digest wrong would land a hash that no longer matches.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func main() {
	aw := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }

	type pick struct {
		schema  string
		root    string
		keyname string
	}
	todo := []pick{
		{schema: "test/tables/W1.schema", root: "Vessel", keyname: "Vessel"},
		{schema: "test/tables/K1.schema", root: "Root", keyname: "Root"},
		{schema: "test/tables/R1.schema", root: "Cfg", keyname: "Cfg"},
		{schema: "tables/examples/Ranges.schema", root: "RangedSigned", keyname: "RangedSigned"},
		{schema: "tables/examples/Ranges.schema", root: "RangedUnsigned", keyname: "RangedUnsigned"},
		{schema: "tables/examples/Ranges.schema", root: "RangedWidths", keyname: "RangedWidths"},
		{schema: "tables/examples/Tables.schema", root: "LoadoutConfig", keyname: "LoadoutConfig"},
		{schema: "tables/examples/Tables.schema", root: "WeaponConfig", keyname: "WeaponConfig"},
		{schema: "tables/messages/Messages.schema", root: "User", keyname: "User"},
		{schema: "tables/messages/Messages.schema", root: "Script", keyname: "Script"},
	}

	c := compiler.New()

	for _, p := range todo {
		u, err := c.Load([]string{p.schema})
		if err != nil {
			fmt.Fprintf(os.Stderr, "load %s: %v\n", p.schema, err)
			os.Exit(1)
		}
		var st *ir.Struct
		for _, s := range u.Structs {
			if s.Name == p.root {
				st = s
			}
		}
		if st == nil {
			for _, s := range u.Tables {
				if s.Name == p.root {
					st = s
				}
			}
		}
		if st == nil {
			keys := []string{}
			for _, s := range u.Structs {
				keys = append(keys, s.Name)
			}
			for _, s := range u.Tables {
				keys = append(keys, s.Name)
			}
			fmt.Fprintf(os.Stderr, "%s: root %q not found, have %v\n", p.schema, p.root, keys)
			os.Exit(1)
		}
		entries := ir.TableFixedWalkRoot(st)
		layout := ir.TableFixedLayoutBytes(entries)
		digest := ir.TableFixedDefinitionsDigest(st)
		full := ir.TableFixedLayoutHash(layout, st)
		layoutOnly := layoutOnlyHash(layout)
		digestOnly := layoutOnlyHash(digest)
		aw("# %s :: %s", p.schema, p.root)
		aw("key=%s", strings.ToLower(p.keyname))
		aw("entries_count=%d", len(entries))
		aw("layout_size=%d", len(layout))
		aw("digest_size=%d", len(digest))
		aw("full_hash=0x%016x", full)
		aw("layout_only_hash=0x%016x", layoutOnly)
		aw("digest_only_hash=0x%016x", digestOnly)
		aw("layout_hex=%s", hex(layout))
		aw("digest_hex=%s", hex(digest))
		counts := map[byte]int{}
		for _, b := range digest {
			counts[b]++
		}
		if has := counts['R']; has > 0 {
			aw("digest_R_count=%d", has)
		}
		if has := counts['Q']; has > 0 {
			aw("digest_Q_count=%d", has)
		}
		if has := counts['F']; has > 0 {
			aw("digest_F_count=%d", has)
		}
		if has := counts['B']; has > 0 {
			aw("digest_B_count=%d", has)
		}
		if has := counts['X']; has > 0 {
			aw("digest_X_count=%d", has)
		}
		if has := counts['L']; has > 0 {
			aw("digest_L_count=%d", has)
		}
		aw("entries:")
		for i, e := range entries {
			aw("  %2d id=0x%016x kind=%d size=%d children=%d note=%q", i, e.ID, byte(e.Kind), e.Size, e.Children, e.Note)
		}
		aw("")
	}
}

func layoutOnlyHash(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range layout {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func hex(b []byte) string {
	var sb strings.Builder
	for _, x := range b {
		fmt.Fprintf(&sb, "%02x", x)
	}
	return sb.String()
}

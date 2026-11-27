// THE ALIGN-ARRAYS FAMILY: a second case family of verify-bitphase, run by
// `--family align-arrays` through the same 18 drivers. Its schema is
// test/verify-bitphase/align-arrays/BitPhase.schema, which reuses the ten type
// names the drivers dispatch on; its rows are written here, bit by bit from
// SPEC §4.3's wire model (LSB first, align to the next byte with zero bits,
// ceil(bits/8) bytes), never by a leg.
//
// Every shape is an array or a union whose element carries an align, so each
// element after the first starts from what the one before it left. A valid
// row is read and written back by every leg and its written bytes are
// compared with the row's own; a refuse row is a valid row with nonzero align
// padding inside an element, which every reader refuses. The audit's four
// Elixir reds (2026-11-24, F1: the Elixir write loop handed stale scratch bits
// to the next element after an align) are rows here, with the bytes the
// broken writer produced as refuse rows beside them where a reader refuses them.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// AlignArraysFamily names the family on the command line.
const AlignArraysFamily = "align-arrays"

// AlignArraysSchema is the family's schema, from the repository root.
const AlignArraysSchema = "test/verify-bitphase/align-arrays/BitPhase.schema"

// bitWriter builds a wire one bit at a time, LSB first.
type bitWriter struct{ bits []byte }

func newBits() *bitWriter { return &bitWriter{} }

// put writes the low n bits of v.
func (w *bitWriter) put(v uint64, n int) *bitWriter {
	for i := range n {
		w.bits = append(w.bits, byte((v>>i)&1))
	}
	return w
}

// align pads to the next byte with the given padding bits (zero on a lawful
// wire).
func (w *bitWriter) align(pad uint64) *bitWriter {
	return w.put(pad, (8-len(w.bits)%8)%8)
}

// raw writes whole bytes; the stream is byte-aligned wherever it is used.
func (w *bitWriter) raw(bs ...byte) *bitWriter {
	for _, b := range bs {
		w.put(uint64(b), 8)
	}
	return w
}

// text writes a string or bytes field: the length in lenBits, an align with
// the given padding, then the used bytes.
func (w *bitWriter) text(lenBits int, s string, pad uint64) *bitWriter {
	return w.put(uint64(len(s)), lenBits).align(pad).raw([]byte(s)...)
}

func (w *bitWriter) bytes() []byte {
	out := make([]byte, (len(w.bits)+7)/8)
	for i, b := range w.bits {
		out[i/8] |= b << (i % 8)
	}
	return out
}

// alignRow is one row of the family: a manifest row and the bytes it owes.
type alignRow struct {
	manifestCase
	data []byte
}

// alignArraysRows is the family, in manifest order.
func alignArraysRows() []alignRow {
	var rows []alignRow
	add := func(kind, typ, name string, data []byte) {
		file := fmt.Sprintf("aa_%s_%s_%s.bin", strings.ToLower(typ), kind, name)
		rows = append(rows, alignRow{manifestCase{kind, typ, file}, data})
	}
	valid := func(typ, name string, data []byte) { add("valid", typ, name, data) }
	refuse := func(typ, name string, data []byte) { add("refuse", typ, name, data) }

	// EmptyZero { pair [2]Named }: no count; each Named is a 3-bit length,
	// an align and the bytes. "a","" is the audit's row: Elixir wrote 01 61 01.
	pair := func(a, b string, padB uint64) []byte {
		return newBits().text(3, a, 0).text(3, b, padB).bytes()
	}
	valid("EmptyZero", "ab-e-acute", pair("ab", "é", 0))
	valid("EmptyZero", "a-empty", pair("a", "", 0))
	valid("EmptyZero", "empty-x", pair("", "x", 0))
	valid("EmptyZero", "full-full", pair("abcdefg", "hijklmn", 0))
	valid("EmptyZero", "empty-empty", pair("", "", 0))
	refuse("EmptyZero", "a-empty-pad", pair("a", "", 1))
	refuse("EmptyZero", "full-full-pad", pair("abcdefg", "hijklmn", 0x10))

	// Phase0 { list [..3]Blob }: a 2-bit count; Blob is bytes(3) (a 2-bit
	// length), an align, the bytes, and a bool.
	blobs := func(padLast uint64, items ...[2]string) []byte {
		w := newBits().put(uint64(len(items)), 2)
		for i, it := range items {
			pad := uint64(0)
			if i == len(items)-1 {
				pad = padLast
			}
			w.text(2, it[0], pad)
			if it[1] == "1" {
				w.put(1, 1)
			} else {
				w.put(0, 1)
			}
		}
		return w.bytes()
	}
	valid("Phase0", "three", blobs(0, [2]string{"\x00\xff", "1"}, [2]string{"", "0"}, [2]string{"abc", "1"}))
	valid("Phase0", "two-empty", blobs(0, [2]string{"", "1"}, [2]string{"", "1"}))
	valid("Phase0", "one", blobs(0, [2]string{"z", "1"}))
	refuse("Phase0", "three-pad", blobs(0x8, [2]string{"\x00\xff", "1"}, [2]string{"", "0"}, [2]string{"abc", "1"}))

	// Phase1 { list [..2]Tail; oct [..3]Octet }: Tail is string(3) (2-bit
	// length), an align, the bytes, bits(5); Octet is string(3), an align,
	// the bytes and a uint8, so it ends on a byte.
	tailsOcts := func(padOct uint64, tails []string, octs [][2]string) []byte {
		w := newBits().put(uint64(len(tails)), 2)
		for _, s := range tails {
			w.text(2, s, 0)
			w.put(0x1F, 5)
		}
		w.put(uint64(len(octs)), 2)
		for i, o := range octs {
			pad := uint64(0)
			if i == len(octs)-1 {
				pad = padOct
			}
			w.text(2, o[0], pad)
			w.raw(o[1][0])
		}
		return w.bytes()
	}
	valid("Phase1", "two-tails", tailsOcts(0, []string{"ab", "c"}, nil))
	valid("Phase1", "two-empty-tails", tailsOcts(0, []string{"", ""}, nil))
	valid("Phase1", "octets", tailsOcts(0, []string{"ab"}, [][2]string{{"xy", "\xff"}, {"", "\x81"}, {"q", "\x7e"}}))
	valid("Phase1", "octets-empty", tailsOcts(0, nil, [][2]string{{"", "\xff"}, {"", "\xff"}}))
	refuse("Phase1", "octets-pad", tailsOcts(0x20, []string{"ab"}, [][2]string{{"xy", "\xff"}, {"", "\x81"}, {"q", "\x7e"}}))

	// Phase7 { us [..3]U }: a 2-bit count; U's tag is 2 bits (n = 1 Named,
	// t = 2 Tail, v = 3 void).
	{
		w := newBits().put(3, 2)
		w.put(1, 2).text(3, "hi", 0)
		w.put(2, 2).text(2, "a", 0).put(0x15, 5)
		w.put(3, 2)
		valid("Phase7", "named-tail-void", w.bytes())
		w = newBits().put(3, 2)
		w.put(3, 2)
		w.put(1, 2).text(3, "", 0)
		w.put(1, 2).text(3, "q", 0)
		valid("Phase7", "void-named-named", w.bytes())
		w = newBits().put(3, 2)
		w.put(3, 2)
		w.put(1, 2).text(3, "", 0)
		w.put(1, 2).text(3, "q", 0x4)
		refuse("Phase7", "void-named-named-pad", w.bytes())
	}

	// Phase31 { outer [..2]Wrap }, Wrap { inner [..2]Named; z bits(1) }: an
	// align-bearing array inside an array element. The audit's row.
	nested := func(pad uint64) []byte {
		w := newBits().put(2, 2)
		w.put(2, 2).text(3, "ab", 0).text(3, "", 0).put(1, 1)
		w.put(1, 2).text(3, "c", pad).put(1, 1)
		return w.bytes()
	}
	valid("Phase31", "nested", nested(0))
	refuse("Phase31", "nested-pad", nested(0x1))

	// Phase32 { x bits(5); list [..2]Named; y bits(7) }
	{
		w := newBits().put(0x1F, 5).put(2, 2)
		w.text(3, "a", 0).text(3, "bc", 0)
		w.put(0x7F, 7)
		valid("Phase32", "two", w.bytes())
		w = newBits().put(0x11, 5).put(1, 2)
		w.text(3, "", 0)
		w.put(0x55, 7)
		valid("Phase32", "one-empty", w.bytes())
		w = newBits().put(0x1F, 5).put(2, 2)
		w.text(3, "a", 0).text(3, "bc", 0x2)
		w.put(0x7F, 7)
		refuse("Phase32", "two-pad", w.bytes())
	}

	// Phase33 { a [..2]Big }, Big { w uint64; s string(3) }: the audit's row.
	bigs := func(pad uint64) []byte {
		w := newBits().put(2, 2)
		w.put(0xFFFFFFFFFFFFFFFF, 64).text(2, "ab", 0)
		w.put(0x8000000000000001, 64).text(2, "", pad)
		return w.bytes()
	}
	valid("Phase33", "two", bigs(0))
	refuse("Phase33", "two-pad", bigs(0x10))

	// Phase56 { k [3]Shape }: Shape's tag is 2 bits (ring = 1 bits(3),
	// slab = 2 string(2) with a 2-bit length, 0 none).
	{
		w := newBits()
		w.put(2, 2).text(2, "ok", 0)
		w.put(1, 2).put(5, 3)
		w.put(2, 2).text(2, "", 0)
		valid("Phase56", "slab-ring-slab", w.bytes())
		w = newBits()
		w.put(1, 2).put(7, 3)
		w.put(2, 2).text(2, "z", 0)
		w.put(0, 2)
		valid("Phase56", "ring-slab-none", w.bytes())
		w = newBits()
		w.put(1, 2).put(7, 3)
		w.put(2, 2).text(2, "z", 0x8)
		w.put(0, 2)
		refuse("Phase56", "ring-slab-none-pad", w.bytes())
	}

	// Phase63 { list [..3]Named }: the audit's first repro, ["ab", "é"],
	// which Elixir wrote 0a 61 62 0a c3 a9.
	names := func(pad uint64, elems ...string) []byte {
		w := newBits().put(uint64(len(elems)), 2)
		for i, e := range elems {
			p := uint64(0)
			if i == len(elems)-1 {
				p = pad
			}
			w.text(3, e, p)
		}
		return w.bytes()
	}
	valid("Phase63", "ab-e-acute", names(0, "ab", "é"))
	valid("Phase63", "three", names(0, "ab", "c", "d"))
	valid("Phase63", "empty-first", names(0, "", "xyz"))
	refuse("Phase63", "ab-e-acute-pad", names(0x10, "ab", "é"))

	// The bytes the broken Elixir writer produced (stale scratch ORed into
	// the next element's first field) where they carry nonzero align
	// padding, so every reader refuses them. Where the residue landed in a
	// value instead (Phase33's second w, Phase1's octet lengths) the bytes
	// are a lawful wire of another value and only the valid rows catch it.
	refuse("EmptyZero", "elixir-f1", []byte{0x01, 0x61, 0x01})
	refuse("Phase63", "elixir-f1", []byte{0x0a, 0x61, 0x62, 0x0a, 0xc3, 0xa9})
	refuse("Phase31", "elixir-f1", []byte{0x2a, 0x61, 0x62, 0x2a, 0x2b, 0x63, 0x2b})
	return rows
}

// alignArraysRoster is the family's required roster, the rows without bytes.
func alignArraysRoster() []manifestCase {
	rows := alignArraysRows()
	out := make([]manifestCase, len(rows))
	for i, r := range rows {
		out[i] = r.manifestCase
	}
	return out
}

// writeAlignArraysCorpus writes the family's manifest and files into dir.
func writeAlignArraysCorpus(dir string) error {
	return writeAlignRows(dir, alignArraysRows())
}

// writeAlignRows writes a manifest and files of the rows given into dir: the
// whole family, or the rows a registry run asked for (adapter.go).
func writeAlignRows(dir string, rows []alignRow) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var m strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&m, "%s\t%s\t%s\n", r.Kind, r.Type, r.File)
		if err := os.WriteFile(filepath.Join(dir, r.File), r.data, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "manifest.tsv"), []byte(m.String()), 0o644)
}

// runFamily runs a case family over the legs named (comma-separated, or all):
// it writes the family's corpus, holds it to the family's roster, builds the
// C++ leg's verifier from cpp/verify.cpp where cpp is named, and runs every
// leg's driver over it. Each invocation has its own folder under
// <work>/<family>/<langs>, since several make processes may run legs of one
// family at once.
func runFamily(repoRoot, schemaBin, schemaMooBin, workRoot, family, langs string) error {
	if family != AlignArraysFamily {
		return fmt.Errorf("--family %q is not a case family (the families: %s)", family, AlignArraysFamily)
	}
	activeRoster = alignArraysRoster
	familyRun = true
	bitphaseSchema = AlignArraysSchema
	var targets []string
	switch langs {
	case "":
		return fmt.Errorf("--lang is required")
	case "all":
		targets = allLangs
	default:
		for l := range strings.SplitSeq(langs, ",") {
			targets = append(targets, strings.TrimSpace(l))
		}
	}
	runRoot := filepath.Join(workRoot, family, strings.ReplaceAll(langs, ",", "-"))
	corpusDir := filepath.Join(runRoot, "corpus")
	if err := writeAlignArraysCorpus(corpusDir); err != nil {
		return err
	}
	cases, blobs, err := loadManifest(corpusDir)
	if err != nil {
		return err
	}
	oracleBin := filepath.Join(runRoot, "oracle")
	if slices.Contains(targets, "cpp") {
		if err := buildFamilyVerifier(repoRoot, schemaBin, oracleBin); err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(targets))
	for _, lang := range targets {
		wg.Go(func() {
			if err := verifyLang(repoRoot, schemaBin, schemaMooBin, oracleBin, corpusDir, filepath.Join(runRoot, "runs"), lang, cases, blobs); err != nil {
				errs <- fmt.Errorf("%s family: %w", family, err)
				return
			}
			fmt.Printf("BITPHASE_OK %s %d (%s family)\n", lang, len(cases), family)
		})
	}
	wg.Wait()
	close(errs)
	var msgs []string
	for err := range errs {
		msgs = append(msgs, err.Error())
	}
	if len(msgs) > 0 {
		return errors.New(strings.Join(msgs, "\n"))
	}
	return nil
}

// buildFamilyVerifier generates the family's C++ codec and compiles
// cpp/verify.cpp against it.
func buildFamilyVerifier(repoRoot, schemaBin, oracleBin string) error {
	cppGen := filepath.Join(filepath.Dir(oracleBin), "cpp-gen")
	if err := generateBitPhase(repoRoot, schemaBin, "", "cpp", cppGen); err != nil {
		return err
	}
	serDir := resolveExistingDir(repoRoot, "SERIALIZE", "../serialize", "build/serialize")
	src := filepath.Join(repoRoot, "test", "verify-bitphase", "cpp", "verify.cpp")
	if out, err := runChecked(repoRoot, nil, envOr("CXX", "c++"), "-std=c++17", "-O2", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off",
		"-I"+serDir, "-I"+cppGen, src, "-o", oracleBin); err != nil {
		return fmt.Errorf("compile verify.cpp: %w\n%s", err, out)
	}
	return nil
}

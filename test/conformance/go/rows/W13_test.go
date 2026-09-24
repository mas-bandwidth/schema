package main

// go/W13 — "layout+hash == C++ reference" (docs/roadmap.sexp, cell
// interoperability/go, "Shared byte oracle and round-trip conformance").
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md §11 (the byte gate every leg owes):
// "It is the WRITE-side byte gate: the Go leg's layout and the whole
// 80915-byte file against the C++ reference's corpus, byte for byte", and
// make/go.mk:533 spells the first half of that run: "compares this build's
// LAYOUT against the corpus's, byte for byte and before anything else — the
// record's positions, ids, kinds, size and the hash every record carries are
// all in those bytes, so a leg that matches them is speaking the form and not
// a near miss". The same page's porting table, step 2, rules the hash:
// "EVERY HASH THE RUNTIME HOLDS IS A HANDED CONSTANT — the leg computes none
// from layout bytes" (§5.9 #47). So the layout+hash a Go build holds are
// CONSTANTS in its generated code, and the C++ reference's are constants in
// the C++ backend's generated code — the backend that test/tables/
// fixedform_dump.cpp names as "the REFERENCE for this form".
//
// THE REFERENCE HERE. The reference's own corpus (build/fixedform-corpus,
// `make tables-fixedform-corpus`) is a C++ build product this tree cannot
// produce on a bench without the C++ serialize runtime, and the committed
// bench corpus (bench/paired/corpus) is for a schema whose generated Go
// imports the serialize.go runtime. What this test uses instead is the
// reference backend's own EMITTED CONSTANTS for the same schema: the same
// compiler (bin/schema, no external dependencies) generates the C++ and the
// Go from one IR, and each backend lays the layout bytes and the fnv1a64
// hash down in its own emitter (internal/codegen/cpptable/fixedform.go and
// internal/codegen/gotable/fixedform.go) — so the constants agreeing is the
// two implementations agreeing, not one file compared with itself. Because
// the hash is HANDED, the constants are exactly the bytes each runtime
// writes: §3.4's file is "form byte, seven reserved zeros, the LAYOUT HASH at
// 8, body at 16, then the layout behind its u32 length, then the records to
// the end of it", and every record carries the same eight-byte hash at its
// head. The test pins the bytes the Go runtime writes to the reference's
// parsed constants, at those offsets.
//
// The unit is the law's own P1/P3 pair (test/tables/P3.schema): Chain nests
// Link by value under an optional, and the reference dump's p3 row is
// written from Chain (docs/FIXED-FORM-ALGORITHM.md §9: "the OUTER table, the
// fixed table no other fixed table of the unit names by value").

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"tblp3"
)

// w13RepoRoot walks up from the rows package to the directory carrying
// bin/schema and make/go.mk — the tree the preflight built the leg's
// generated code from.
func w13RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("W13: cannot name the working directory: %v", err)
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "bin", "schema")); err == nil && !st.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "make", "go.mk")); err == nil {
				return dir
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("W13: the schema repo root is not above the rows package — bin/schema is not built; run the leg's preflight (make build/conformance-harness build/conformance-go table-base64-go)")
		}
		dir = next
	}
}

// w13RefFixed parses the C++ reference backend's emitted constants for one
// table out of the generated <T>Table.h: the handed hash, the layout bytes
// and the record body size.
func w13RefFixed(t *testing.T, header []byte, table string) (hash uint64, layout []byte, body int64) {
	t.Helper()
	name := regexp.QuoteMeta(table)
	m := regexp.MustCompile(name + `FixedHash = 0x([0-9a-fA-F]+)ull`).FindSubmatch(header)
	if m == nil {
		t.Fatalf("W13: the C++ reference names no %sFixedHash in its generated header", table)
	}
	hash, err := strconv.ParseUint(string(m[1]), 16, 64)
	if err != nil {
		t.Fatalf("W13: the C++ reference's %sFixedHash is not a number: %v", table, err)
	}
	m = regexp.MustCompile(name + `FixedBodyBytes = (\d+);`).FindSubmatch(header)
	if m == nil {
		t.Fatalf("W13: the C++ reference names no %sFixedBodyBytes in its generated header", table)
	}
	body, err = strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		t.Fatalf("W13: the C++ reference's %sFixedBodyBytes is not a number: %v", table, err)
	}
	block := regexp.MustCompile(name + `FixedLayout\[\] = \{\n(?s)(.*?)\n\};`).FindSubmatch(header)
	if block == nil {
		t.Fatalf("W13: the C++ reference names no %sFixedLayout in its generated header", table)
	}
	for _, f := range regexp.MustCompile(`0x([0-9a-fA-F]{1,2})`).FindAllSubmatch(block[1], -1) {
		b, err := strconv.ParseUint(string(f[1]), 16, 8)
		if err != nil {
			t.Fatalf("W13: the C++ reference's %sFixedLayout carries a byte that is not one: %v", table, err)
		}
		layout = append(layout, byte(b))
	}
	if len(layout) == 0 {
		t.Fatalf("W13: the C++ reference's %sFixedLayout is empty", table)
	}
	return hash, layout, body
}

func TestRowW13(t *testing.T) {
	root := w13RepoRoot(t)

	// The C++ reference backend, generated fresh from the same schema the
	// Go leg's tables were generated from. The output lives under the
	// repo's build/ scratch and is removed when the test returns.
	out := filepath.Join(root, "build", fmt.Sprintf("rows-w13-cpp-%d", os.Getpid()))
	if err := os.RemoveAll(out); err != nil {
		t.Fatalf("W13: the previous reference generation cannot be cleared: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	gen := exec.Command(filepath.Join("bin", "schema"), "generate", "--lang", "cpp", "--out", out, filepath.Join("test", "tables", "P3.schema"))
	gen.Dir = root
	if text, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("W13: the C++ reference backend cannot be generated: %v\n%s", err, text)
	}
	header, err := os.ReadFile(filepath.Join(out, "P3Table.h"))
	if err != nil {
		t.Fatalf("W13: the C++ reference's generated header is not readable: %v", err)
	}

	chainHash, chainLayout, chainBody := w13RefFixed(t, header, "Chain")
	linkHash, linkLayout, linkBody := w13RefFixed(t, header, "Link")

	// The form byte is one more constant both sides hold (§3.4's first byte).
	m := regexp.MustCompile(`kTableFixedForm = (\d+);`).FindSubmatch(header)
	if m == nil {
		t.Fatalf("W13: the C++ reference names no kTableFixedForm in its generated header")
	}
	refForm, err := strconv.ParseUint(string(m[1]), 10, 8)
	if err != nil {
		t.Fatalf("W13: the C++ reference's kTableFixedForm is not a number: %v", err)
	}

	// ---- the leg's constants against the reference's, one clause per line.

	if uint64(tblp3.TableFixedForm) != refForm {
		t.Errorf("W13: the form byte moved: go=%d C++ reference=%d", tblp3.TableFixedForm, refForm)
	}
	t.Logf("W13: form byte: go=%d C++ reference=%d", tblp3.TableFixedForm, refForm)

	if tblp3.ChainFixedHash != chainHash {
		t.Errorf("W13: Chain's hash is not the C++ reference's: go=%#x C++ reference=%#x", uint64(tblp3.ChainFixedHash), chainHash)
	}
	t.Logf("W13: Chain hash: go=%#016x C++ reference=%#016x", uint64(tblp3.ChainFixedHash), chainHash)

	if !bytes.Equal(tblp3.ChainFixedLayout, chainLayout) {
		t.Errorf("W13: Chain's layout is not the C++ reference's, byte for byte: go=%d bytes C++ reference=%d bytes", len(tblp3.ChainFixedLayout), len(chainLayout))
	} else {
		t.Logf("W13: Chain layout: go=%d bytes C++ reference=%d bytes, equal byte for byte", len(tblp3.ChainFixedLayout), len(chainLayout))
	}

	if int64(tblp3.ChainFixedBodyBytes) != chainBody {
		t.Errorf("W13: Chain's record body size is not the C++ reference's: go=%d C++ reference=%d", tblp3.ChainFixedBodyBytes, chainBody)
	}
	t.Logf("W13: Chain body bytes: go=%d C++ reference=%d", tblp3.ChainFixedBodyBytes, chainBody)

	// The lock's identity entry: the lineage's last is the reader's own, and
	// it must be the same three facts (§5.2; the go emitter's entry is
	// computed exactly as the reference's, so the sets compared here are the
	// OWN entries on both sides — the C++ fixture-only peer fallback that
	// hands the p1 entry to a lockless P3 generate is a test scaffold, not a
	// fact either runtime holds).
	own := tblp3.ChainFixedKnown[len(tblp3.ChainFixedKnown)-1]
	if own.Hash != chainHash || own.Record != 8+chainBody || !bytes.Equal(own.Layout, chainLayout) {
		t.Errorf("W13: Chain's locked identity entry is not the reference's layout+hash: hash=%#x record=%d layout=%d bytes", own.Hash, own.Record, len(own.Layout))
	}
	t.Logf("W13: Chain identity entry: hash=%#016x record=%d layout=%d bytes", own.Hash, own.Record, len(own.Layout))

	if tblp3.LinkFixedHash != linkHash {
		t.Errorf("W13: Link's hash is not the C++ reference's: go=%#x C++ reference=%#x", uint64(tblp3.LinkFixedHash), linkHash)
	}
	t.Logf("W13: Link hash: go=%#016x C++ reference=%#016x", tblp3.LinkFixedHash, linkHash)

	if !bytes.Equal(tblp3.LinkFixedLayout, linkLayout) {
		t.Errorf("W13: Link's layout is not the C++ reference's, byte for byte: go=%d bytes C++ reference=%d bytes", len(tblp3.LinkFixedLayout), len(linkLayout))
	} else {
		t.Logf("W13: Link layout: go=%d bytes C++ reference=%d bytes, equal byte for byte", len(tblp3.LinkFixedLayout), len(linkLayout))
	}

	if int64(tblp3.LinkFixedBodyBytes) != linkBody {
		t.Errorf("W13: Link's record body size is not the C++ reference's: go=%d C++ reference=%d", tblp3.LinkFixedBodyBytes, linkBody)
	}
	t.Logf("W13: Link body bytes: go=%d C++ reference=%d", tblp3.LinkFixedBodyBytes, linkBody)

	// ---- the bytes the leg's runtime writes, against the reference's
	// constants at §3.4's offsets. This is the half the handed-constant law
	// makes a constant comparison into a wire comparison: a file is the
	// header, the layout and the records, and every one of those carries
	// either the form byte, the layout bytes or the hash.
	//
	// The vector is the reference dump's own p3 row (test/tables/
	// fixedform_dump.cpp p3_file): one record whose optional is present and
	// one whose optional is absent, so the record stride is walked twice and
	// the absent payload's zeros ride under the same hash.
	values := []tblp3.Chain{
		{Name: [16]byte{'p', 'r', 'e', 's', 'e', 'n', 't'}, NameLength: 7, LinkPresent: true,
			Link: tblp3.Link{Value: 88, Tag: [8]byte{'h', 'e', 'r', 'e'}, TagLength: 4}},
		{Name: [16]byte{'a', 'b', 's', 'e', 'n', 't'}, NameLength: 6, LinkPresent: false,
			Link: tblp3.Link{Value: 99, Tag: [8]byte{'s', 't', 'i', 'l', 'l'}, TagLength: 5}},
	}
	file := make([]byte, tblp3.ChainFixedMeasure(int64(len(values))))
	n := tblp3.ChainFixedSave(values, file)
	if n != int64(len(file)) {
		t.Fatalf("W13: the leg's writer refused or short-wrote its own measure: n=%d need=%d", n, len(file))
	}
	t.Logf("W13: the writer filled its own measure exactly: n=%d", n)

	if file[0] != tblp3.TableFixedForm {
		t.Errorf("W13: the file's form byte is not the reference's: got=%d want=%d", file[0], refForm)
	}
	t.Logf("W13: the file's form byte: %d", file[0])

	reserved := true
	for i, b := range file[1:tblp3.TableFixedHashAt] {
		if b != 0 {
			reserved = false
			t.Errorf("W13: the header's reserved byte %d is nonzero: %d", i+1, b)
		}
	}
	if reserved {
		t.Logf("W13: the header's seven reserved bytes are zero")
	}

	if got := binary.LittleEndian.Uint64(file[tblp3.TableFixedHashAt:tblp3.TableFixedHeaderBytes]); got != chainHash {
		t.Errorf("W13: the header's hash is not the C++ reference's: got=%#x want=%#x", got, chainHash)
	} else {
		t.Logf("W13: the header's hash at 8: %#016x, the C++ reference's", chainHash)
	}

	layoutBytes := binary.LittleEndian.Uint32(file[tblp3.TableFixedHeaderBytes : tblp3.TableFixedHeaderBytes+4])
	if uint32(len(chainLayout)) != layoutBytes {
		t.Errorf("W13: the layout's length word is not the C++ reference's: got=%d want=%d", layoutBytes, len(chainLayout))
	} else if !bytes.Equal(file[tblp3.TableFixedHeaderBytes+4:tblp3.TableFixedHeaderBytes+4+int(layoutBytes)], chainLayout) {
		t.Errorf("W13: the file's layout bytes are not the C++ reference's, byte for byte")
	} else {
		t.Logf("W13: the layout behind its u32 length at 16: %d bytes, the C++ reference's", layoutBytes)
	}

	record := 8 + chainBody
	rest := int64(len(file)) - tblp3.TableFixedHeaderBytes - 4 - int64(layoutBytes)
	if rest%record != 0 || rest/record != int64(len(values)) {
		t.Fatalf("W13: the records are not the reference's stride: rest=%d record=%d", rest, record)
	}
	hashed := true
	for k := int64(0); k < rest/record; k++ {
		at := tblp3.TableFixedHeaderBytes + 4 + int64(layoutBytes) + k*record
		if got := binary.LittleEndian.Uint64(file[at : at+8]); got != chainHash {
			hashed = false
			t.Errorf("W13: record %d does not carry the C++ reference's hash at its head: got=%#x want=%#x", k, got, chainHash)
		}
	}
	if hashed {
		t.Logf("W13: all %d records carry the C++ reference's hash at their heads", rest/record)
	}
}

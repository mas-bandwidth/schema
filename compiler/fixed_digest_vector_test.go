package compiler

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE DEFINITIONS DIGEST VECTOR (docs/SPEC-TABLES.md §3.4; docs/SPEC-CONFORMANCE.md
// §8, gap 8). A port that builds its fixed layouts from schema text computes the
// layout bytes, the definitions digest and the record hash itself; this vector is
// the one it checks them against, byte for byte. The test holds the compiler to the
// committed vector, and holds the hash to fnv1a64 over the layout bytes and then
// the digest bytes, computed here without the compiler's hash function.
// DIGEST_VECTOR_WRITE=1 rewrites vector.txt from the compiler.
func TestFixedDefinitionsDigestVector(t *testing.T) {
	t.Parallel()
	const dir = "../testdata/conformance/fixed-digest/"
	u, err := New().Load([]string{dir + "Digest.schema"})
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Probe"]
	if st == nil {
		t.Fatal("no fixed table Probe")
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	hash := ir.TableFixedLayoutHash(layout, st)

	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h = (h ^ uint64(b)) * 0x100000001b3
	}
	if h != hash {
		t.Fatalf("the compiler's hash %#016x is not fnv1a64(layout || digest) %#016x", hash, h)
	}
	// every row tag the spec defines is exercised
	for _, tag := range []string{"C", "R", "D", "Q", "B", "X", "F"} {
		if !digestHasRow(digest, tag[0]) {
			t.Errorf("the vector's digest carries no %s row", tag)
		}
	}

	got := fmt.Sprintf("root Probe\nlayout %x\ndigest %x\nhash %#016x\n", layout, digest, hash)
	path := dir + "vector.txt"
	if os.Getenv("DIGEST_VECTOR_WRITE") == "1" {
		head := "# THE DEFINITIONS DIGEST VECTOR for Digest.schema (docs/SPEC-TABLES.md §3.4).\n" +
			"# layout: the layout bytes; digest: the definitions digest; hash: fnv1a64 over\n" +
			"# the layout bytes and then the digest bytes, the u64 every record carries.\n"
		if err := os.WriteFile(path, []byte(head+got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — DIGEST_VECTOR_WRITE=1 writes it", err)
	}
	var body []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			body = append(body, line)
		}
	}
	if want := strings.Join(body, "\n") + "\n"; want != got {
		t.Fatalf("the compiler's digest vector moved:\n got %s\nwant %s", got, want)
	}
}

// digestHasRow walks the digest's rows (tag u8, path length u32, path,
// payload length u32, payload) after the 26-byte prefix.
func digestHasRow(d []byte, tag byte) bool {
	prefix := []byte("\x00schema-fixed-definitions\x02")
	if !bytes.HasPrefix(d, prefix) {
		return false
	}
	le := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	for p := len(prefix); p+5 <= len(d); {
		if d[p] == tag {
			return true
		}
		p += 1 + 4 + le(d[p+1:])
		if p+4 > len(d) {
			return false
		}
		p += 4 + le(d[p:])
	}
	return false
}


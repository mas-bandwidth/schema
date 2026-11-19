package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestMessageFuzzPackUnpackAndOracle(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	u := newUnits(m)
	irUnit, err := u.get("backenddemo")
	if err != nil {
		t.Fatalf("load backenddemo: %v", err)
	}
	roots, rootMap, err := buildMessageFuzzRoots(u, messageFuzzOptions{})
	if err != nil {
		t.Fatalf("buildMessageFuzzRoots: %v", err)
	}
	ri, ok := rootMap["backenddemo.LoginRequest"]
	if !ok {
		t.Fatalf("missing backenddemo.LoginRequest in roots")
	}
	r := roots[ri]

	// 1. Valid 2-body batch with same-build announcement (announceLen == 0) must be accepted.
	b0 := r.model.New(r.def)
	b1 := r.model.New(r.def)
	batch, err := tablewire.EncodeMessages(r.model, []*tabletext.Instance{b0, b1})
	if err != nil {
		t.Fatalf("EncodeMessages: %v", err)
	}
	frame := packMessageFuzzFrame(nil, batch)
	annOut, batchOut, ok := unpackMessageFuzzFrame(frame)
	if !ok || len(annOut) != 0 || !bytes.Equal(batchOut, batch) {
		t.Fatalf("unpackMessageFuzzFrame mismatch")
	}
	ans, err := r.oracle(frame)
	if err != nil {
		t.Fatalf("oracle error: %v", err)
	}
	if !ans.accepted || !bytes.Equal(ans.encoded, batch) {
		t.Fatalf("expected valid 2-body batch accepted and canonical, got accepted=%v", ans.accepted)
	}

	// 2. Malformed announcement (form 2 instead of form 1) must be refused by oracle.
	bad := packMessageFuzzFrame([]byte{0x02, 0x00}, batch)
	badAns, err := r.oracle(bad)
	if err != nil {
		t.Fatalf("oracle on bad announcement: %v", err)
	}
	if badAns.accepted {
		t.Fatalf("expected malformed announcement to be refused by oracle")
	}

	// 3. 65-entry shifted vocabulary announcement + batch must be accepted and re-encoded in own vocab!
	padded := make([]ir.TableVocabularyEntry, 0, 65)
	for k := range 65 - len(r.entries) {
		padded = append(padded, ir.TableVocabularyEntry{
			Id:   0xdeadbeef00000000 | uint64(k+1),
			Kind: uint8(ir.TableKindU8),
		})
	}
	padded = append(padded, r.entries...)
	ann65 := wrapVocabularyAnnouncement(ir.BuildVersion(irUnit)^0x12345678, ir.TableVocabularyBytes(padded))
	batch65, err := tablewire.EncodeMessagesWithVocabulary(r.model, []*tabletext.Instance{b0, b1}, padded)
	if err != nil {
		t.Fatalf("EncodeMessagesWithVocabulary: %v", err)
	}
	ans65, err := r.oracle(packMessageFuzzFrame(ann65, batch65))
	if err != nil {
		t.Fatalf("oracle on 65-entry peer batch: %v", err)
	}
	if !ans65.accepted || !bytes.Equal(ans65.encoded, batch) {
		t.Fatalf("expected 65-entry peer batch to transcode to canonical own batch: accepted=%v", ans65.accepted)
	}
}

// Negative control unit test: verifies a leg that corrupts saved bytes or
// accepts a refused mutant fails messageFuzz immediately with DIVERGENCE.
func TestMessageFuzzDetectsSabotagedDriver(t *testing.T) {
	chdirRepoRoot(t)
	pySrc := `import struct, sys
stdin, stdout = sys.stdin.buffer, sys.stdout.buffer
hdr = stdin.read(4)
if len(hdr) < 4: sys.exit(0)
count, = struct.unpack("<I", hdr)
for _ in range(count):
    ulen, = struct.unpack("<H", stdin.read(2))
    stdin.read(ulen)
    rlen, = struct.unpack("<H", stdin.read(2))
    stdin.read(rlen)
    stdin.read(2)
    stdout.write(b"\x01")
stdout.flush()
while True:
    m = stdin.read(8)
    if len(m) < 8: break
    idx, sz = struct.unpack("<II", m)
    stdin.read(sz)
    # Sabotaged: always claims accepted=1 with bogus 3-byte batch [2, 0, 1]
    out = bytearray(47)
    out[0] = 1
    struct.pack_into("<q", out, 23, -1)
    struct.pack_into("<q", out, 39, 3)
    stdout.write(bytes(out) + b"\x02\x00\x01")
    stdout.flush()
`
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "sabotaged.py")
	if err := os.WriteFile(scriptPath, []byte(pySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	failedPath := filepath.Join(tmp, "failed.bin")
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	var out bytes.Buffer
	err = messageFuzz(&out, m, messageFuzzOptions{
		driver:     "python3 " + scriptPath,
		failed:     failedPath,
		seed:       1,
		n:          5,
		maxMutants: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "DIVERGENCE") {
		t.Fatalf("expected sabotaged driver to fail with DIVERGENCE, got: %v", err)
	}
	if _, serr := os.Stat(failedPath); serr != nil {
		t.Fatalf("expected failing reproducer written to %s: %v", failedPath, serr)
	}
}

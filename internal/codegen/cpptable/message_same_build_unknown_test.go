package cpptable

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This saved wire is login_full_message, bit #10, seed 24845619678: a field
// from the same announced vocabulary belongs to a different message record.
const cppMessageUnknownWire = "020026000000000000046080030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc24794400a04c00"

func cppMessageUnknownProbe(t *testing.T, undermine bool) ([]byte, error) {
	t.Helper()
	wire, err := hex.DecodeString(cppMessageUnknownWire)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(wire)); got != "9235cab607265060bcb037782e27a0666bcb8377d92170871dc46e772fa36be2" {
		t.Fatalf("saved wire digest: %s", got)
	}
	ann, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadGraphDirUnit(t, filepath.Join("..", "..", "..", "tables", "backend"))
	body := fmt.Sprintf(`const uint8_t ann[] = { %s };
const uint8_t wire[] = { %s };
TableMessageEntry entries[kTableMessageEntriesHere];
TableVocabulary vocabulary(entries, kTableMessageEntriesHere);
TableReport ar;
if (!AnnounceRead(vocabulary, ann, sizeof(ann), &ar) || vocabulary.build_version != BuildVersion) { printf("announcement mismatch\n"); return 1; }
LoginRequest value; int64_t count = 1; TableReport same;
if (LoginRequestLoadMessages(&value, &count, vocabulary, wire, sizeof(wire), &same) || !same.damaged || count != 0) { printf("same-build unknown accepted\n"); return 2; }
vocabulary.build_version ^= 1;
count = 1; TableReport foreign;
if (!LoginRequestLoadMessages(&value, &count, vocabulary, wire, sizeof(wire), &foreign) || foreign.damaged || foreign.refused || foreign.unknown != 1 || count != 1) { printf("foreign-build unknown refused\n"); return 3; }
`, cppBytes(ann), cppBytes(wire))
	edit := func(files map[string][]byte) {
		if !undermine {
			return
		}
		const marker = "inline bool LoginRequestLoadMessageBody("
		const guard = "                if ( vocabulary.build_version == BuildVersion ) { report->damaged = true; return false; }\n"
		found := false
		for name, data := range files {
			src := string(data)
			pos := strings.Index(src, marker)
			if pos < 0 {
				continue
			}
			rel := strings.Index(src[pos:], guard)
			if rel < 0 {
				continue
			}
			at := pos + rel
			files[name] = []byte(src[:at] + src[at+len(guard):])
			found = true
			break
		}
		if !found {
			t.Fatal("the generated LoginRequest membership guard was not found")
		}
	}
	return cppHeldRunEdited(t, u, body, edit)
}

func TestCppMessageSameBuildUnknownField(t *testing.T) {
	t.Parallel()
	out, err := cppMessageUnknownProbe(t, false)
	if err != nil {
		t.Fatalf("message membership: %v\n%s", err, out)
	}
}

func TestCppMessageSameBuildUnknownFieldNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := cppMessageUnknownProbe(t, true)
	if err == nil || !strings.Contains(string(out), "same-build unknown accepted") {
		t.Fatalf("removed guard did not turn the saved-wire check red: %v\n%s", err, out)
	}
}

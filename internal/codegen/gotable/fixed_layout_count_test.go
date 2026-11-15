package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedLayoutCountPreserved tests the closure walk after the byte-length
// check. The test copy bypasses only that precondition because a high-bit
// count would otherwise require more than 36 GB of layout bytes.
func TestFixedLayoutCountPreserved(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain")
	u := unitFrom(t, `package probedemo
fixed table Root { x int32 }
`)
	probe := `package probedemo
import ("encoding/binary"; "testing")
func TestCount(t *testing.T) {
 b := append([]byte(nil), RootFixedLayout...)
 if _, why := tableFixedParseLayout(b); why != "" { t.Fatalf("valid layout: %s", why) }
 n := binary.LittleEndian.Uint32(b)
 binary.LittleEndian.PutUint32(b, n | 0x80000000)
 if _, why := tableFixedParseLayout(b); why == "" { t.Fatal("high-bit count accepted") }
}`
	const old = "count == 0 || count > 0x7fffffff || int64(count)*int64(tableFixedEntryBytes)+int64(tableFixedLayoutHeaderBytes) != int64(len(bytes))"
	run := func(mask bool) (string, error) {
		patched := 0
		out, err := sharedRun(t, u, probe, RuntimeStandalone, func(files map[string][]byte) {
			for name, data := range files {
				s := string(data)
				if strings.Contains(s, old) {
					s = strings.Replace(s, old, "count == 0 || count > 0x7fffffff", 1)
					if mask {
						s = strings.Replace(s, "count == 0 || count > 0x7fffffff", "count == 0", 1)
						s = strings.Replace(s, "Count: int32(count)", "Count: int32(count & 0x7fffffff)", 1)
					}
					files[name] = []byte(s)
					patched++
				}
			}
		})
		if patched != 1 {
			t.Fatalf("length precondition applied to %d files", patched)
		}
		return out, err
	}
	if out, err := run(false); err != nil {
		t.Fatalf("standalone layout probe: %v\n%s", err, out)
	}
	if out, err := run(true); err == nil || !strings.Contains(out, "high-bit count accepted") {
		t.Fatalf("masked count control did not reproduce acceptance: %v\n%s", err, out)
	}
}

package gotable

import (
	"bytes"
	"strings"
	"testing"
)

const blobSpanSchema = `package probe
table Catalog {
 thumb *bytes
 note *string
 extra *bytes
}
`

const blobSpanTest = `package probe
import("testing")
func pattern(i int64) byte { return byte((i*31 + 7) & 0xff) }
func TestBlobSpanHolds(t *testing.T) {
 const big int64 = 66000
 var b CatalogBuilder
 if !b.Init() { t.Fatal("init") }
 defer b.Shutdown()
 root := b.GetRoot()
 thumb := TableBytesEmplace(&b.Main, &root.Thumb, big)
 if thumb == nil { t.Fatal("thumb") }
 for i := int64(0); i < big; i++ { thumb[i] = pattern(i) }
 if TableStringEmplace(&b.Main, &root.Note, []byte("tail")) == nil { t.Fatal("note") }
 extra := TableBytesEmplace(&b.Main, &root.Extra, 24)
 if extra == nil { t.Fatal("extra") }
 for i := range extra { extra[i] = byte(i) }
 live := TableBytesAt(&root.Thumb, &b.Arena)
 if live.Length != big { t.Fatalf("live length %d", live.Length) }
 for i := int64(0); i < big; i++ {
  if live.Data[i] != pattern(i) { t.Fatalf("blob past the slab at %d", i) }
 }
 if !b.Lock() { t.Fatal("lock") }
 locked := TableBytesAt(&b.AsConst().Thumb)
 if locked.Length != big { t.Fatalf("locked length %d", locked.Length) }
 for i := int64(0); i < big; i++ {
  if locked.Data[i] != pattern(i) { t.Fatalf("blob past the slab after lock at %d", i) }
 }
}
`

func TestBlobSpanHoldsAfterLaterAllocations(t *testing.T) {
	runGenerated(t, blobSpanSchema, blobSpanTest)
}

func TestBlobSpanNegativeControl(t *testing.T) {
	out, err := runGeneratedEdited(t, blobSpanSchema, blobSpanTest, func(files map[string][]byte) {
		patched := 0
		for name, data := range files {
			s := string(data)
			repls := [][2]string{
				{"span := bytes > tableArenaSlabBytes", "span := false /* SABOTAGED */"},
				{"span:=bytes>tableArenaSlabBytes", "span:=false /* SABOTAGED */"},
				{"if n > tableArenaSlabBytes {", "if false { /* SABOTAGED: no blob takes a span */"},
				{"if n>tableArenaSlabBytes {", "if false { /* SABOTAGED: no blob takes a span */"},
			}
			n := 0
			for _, pair := range repls {
				next := strings.ReplaceAll(s, pair[0], pair[1])
				if next != s {
					n++
					s = next
				}
			}
			if n == 0 {
				continue
			}
			files[name] = []byte(s)
			patched += n
		}
		if patched == 0 {
			t.Fatal("NEGATIVE CONTROL FAILED: the span sabotage patched nothing")
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a blob bump-allocated in a slab it does not fit left the driver GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("blob past the slab")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the driver went red, but not on the blob\n%s", out)
	}
}

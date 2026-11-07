package fixedlimit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	lines := "file=old_limit_record.bin limit=fixed_record_bound bound=65536 record=65536 root=LimitRecord records=1 verdict=read parse=ok counters=unknown=0,kind_mismatch=0,widened=0,clamped=0,duplicate=0,malformed=0 values=r0.lead=2863311530,r0.text[0]=76\n" +
		"file=hostile_limit_record.bin limit=fixed_record_bound bound=65536 record=65537 root=LimitRecord records=1 verdict=refused reason=layout_malformed parse=layout_record_too_large counters=unknown=0,kind_mismatch=0,widened=0,clamped=0,duplicate=0,malformed=0\n"
	if err := os.WriteFile(filepath.Join(dir, "limits.txt"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	at, err := Read(dir, RecordAt)
	if err != nil {
		t.Fatal(err)
	}
	if at.Refused() || at.Bound != 65536 || at.Parse != "ok" {
		t.Fatalf("the at-limit line read as %+v", at)
	}
	if v, err := at.Int("r0.text[0]"); err != nil || v != 76 {
		t.Fatalf("r0.text[0] = %d (%v), want 76", v, err)
	}
	over, err := Read(dir, RecordOver)
	if err != nil {
		t.Fatal(err)
	}
	if !over.Refused() || over.Reason != "layout_malformed" || over.Parse != "layout_record_too_large" {
		t.Fatalf("the over-limit line read as %+v", over)
	}
	if n, err := over.Counter("malformed"); err != nil || n != 0 {
		t.Fatalf("malformed = %d (%v), want 0", n, err)
	}
	if _, err := Read(dir, DepthAt); err == nil {
		t.Fatal("a file limits.txt does not name read as a vector")
	}
}

func TestReadRefusesAReasonlessRefusal(t *testing.T) {
	dir := t.TempDir()
	line := "file=x.bin limit=fixed_max_depth bound=64 verdict=refused parse=ok counters=unknown=0\n"
	if err := os.WriteFile(filepath.Join(dir, "limits.txt"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, "x.bin"); err == nil {
		t.Fatal("a refusal with no reason read as a vector")
	}
}

func TestLayout(t *testing.T) {
	file := make([]byte, 20+5+3)
	file[16] = 5
	for i := range 5 {
		file[20+i] = byte(i + 1)
	}
	got, err := Layout(file)
	if err != nil || len(got) != 5 || got[0] != 1 || got[4] != 5 {
		t.Fatalf("Layout = %v (%v), want the five bytes behind the length", got, err)
	}
	file[16] = 9
	if _, err := Layout(file); err == nil {
		t.Fatal("a layout past the file read")
	}
	if _, err := Layout(file[:10]); err == nil {
		t.Fatal("a short file read")
	}
}

func TestDeepPath(t *testing.T) {
	p := DeepPath()
	if !strings.HasPrefix(p, "r0.d.") || !strings.HasSuffix(p, ".d.v") || strings.Count(p, ".d") != 63 {
		t.Fatalf("DeepPath = %q, want r0, 63 .d hops and .v", p)
	}
}

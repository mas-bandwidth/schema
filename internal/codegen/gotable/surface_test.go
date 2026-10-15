package gotable

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const surfaceProbeSchema = `package probe
enum Grade { Gold, Silver }
type Point { x int32 }
union Value {
	n int32
	label string(8)
}
fixed table Root {
	title string(12) = "default"
	grade Grade
	point Point
	value Value
	values ?[..3]int32
}
`

func surfaceProbeUnit(t *testing.T) *ir.Unit {
	t.Helper()
	return unitFrom(t, surfaceProbeSchema)
}

func TestSurfaceParse(t *testing.T) {
	t.Parallel()
	all, err := parseSurfaces("")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, allSurfaces()) {
		t.Fatalf("default list != all surfaces: %+v", all)
	}
	if _, err := parseSurfaces("bogus"); err == nil {
		t.Fatal("unknown surface was accepted")
	}
	jj, err := parseSurfaces("json")
	if err != nil {
		t.Fatal(err)
	}
	if !jj.json || !jj.wire {
		t.Fatalf("json must resolve wire: %+v", jj)
	}
}

func TestGoGenerateSurfaceDefaultsToAllBytes(t *testing.T) {
	t.Parallel()
	u := surfaceProbeUnit(t)
	defaultFiles, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	allFiles, err := GenerateSurfaceList(u, nil, "all")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaultFiles, allFiles) {
		t.Fatal("default generate and --surface all differ")
	}
	emptyFiles, err := GenerateSurfaceList(u, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaultFiles, emptyFiles) {
		t.Fatal("--surface '' must equal the default")
	}
}

func hasSuffixFile(files map[string][]byte, suffix string) bool {
	for name := range files {
		if bytes.HasSuffix([]byte(name), []byte(suffix)) {
			return true
		}
	}
	return false
}

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	u := surfaceProbeUnit(t)
	// file-level surfaces: their own file appears exactly when the surface is
	// named. The table core (ProbeTable.go) is the wire base and always
	// appears. Fixed and message are in-file surfaces, checked by marker
	// functions inside the core file.
	fileSurfaces := map[string]string{
		"json": "TableJson.go",
		"cook": "Cook.go",
		"view": "View.go",
	}
	for list, suffix := range fileSurfaces {
		files, err := GenerateSurfaceList(u, nil, list)
		if err != nil {
			t.Fatalf("%s: %v", list, err)
		}
		if !hasSuffixFile(files, "Table.go") {
			t.Fatalf("%s: wire base ProbeTable.go missing: %v", list, keys(files))
		}
		if !hasSuffixFile(files, suffix) {
			t.Fatalf("%s: expected file suffix %s: %v", list, suffix, keys(files))
		}
		for other, otherSuffix := range fileSurfaces {
			if other != list && hasSuffixFile(files, otherSuffix) {
				t.Fatalf("%s: unexpected file suffix %s", list, otherSuffix)
			}
		}
	}
	// in-file surfaces: fixed and message markers are present only when named.
	fixedMark := "func RootFixedMeasure("
	messageMark := "func RootSaveMessageBody("
	core, err := GenerateSurfaceList(u, nil, "fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(core["ProbeTable.go"], []byte(fixedMark)) {
		t.Fatal("fixed surface missing RootFixedMeasure")
	}
	if bytes.Contains(core["ProbeTable.go"], []byte(messageMark)) {
		t.Fatal("fixed surface unexpectedly contains message surface")
	}
	msg, err := GenerateSurfaceList(u, nil, "message")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(msg["ProbeTable.go"], []byte(messageMark)) {
		t.Fatal("message surface missing RootSaveMessageBody")
	}
	if bytes.Contains(msg["ProbeTable.go"], []byte(fixedMark)) {
		t.Fatal("message surface unexpectedly contains fixed surface")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

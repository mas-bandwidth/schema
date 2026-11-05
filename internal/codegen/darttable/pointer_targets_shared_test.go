package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartPointerTargetsSharedCorpus(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	schema, raw := sharedDartFixture(t, "pointer-targets")
	var rows []struct {
		Name, Writer, Reader string
		Input, Expected      json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 {
		t.Fatalf("pointer target corpus rows=%d, want10", len(rows))
	}
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var probe strings.Builder
	probe.WriteString(dartClosurePrelude)
	for _, row := range rows {
		writer := m.New(u.Tables[row.Writer])
		var wr tabletext.Report
		if !m.Read(writer, row.Input, &wr) || !wr.Silent() {
			t.Fatalf("%s writer %+v", row.Name, wr)
		}
		wire, err := tablewire.Encode(m, writer)
		if err != nil {
			t.Fatal(err)
		}
		fn, writerFn := dartName(row.Reader), dartName(row.Writer)
		fmt.Fprintf(&probe, "{final original=w.%sFromJson(%q);check(hex(w.%sWireSave(original))==%q,%q);}\n", writerFn, string(row.Input), writerFn, hex.EncodeToString(wire), row.Name+" writer pin")
		for _, retaining := range []bool{false, true} {
			value := m.New(u.Tables[row.Reader])
			var r tabletext.Report
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			var ok bool
			if retaining {
				ok, err = tablewire.DecodeRetain(m, value, wire, store, &r)
			} else {
				ok, err = tablewire.Decode(m, value, wire, &r)
			}
			if !ok || err != nil || r.Refused || row.KindMismatch != 0 || r.Unknown != row.Unknown {
				t.Fatalf("%s retaining=%v explicit oracle %+v %v", row.Name, retaining, r, err)
			}
			canonical, err := m.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err = json.Unmarshal(canonical, &got); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(row.Expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s semantic oracle %s != %s", row.Name, canonical, row.Expected)
			}
			saved, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			var sr tabletext.Report
			retainedSave, err := tablewire.EncodeRetain(m, value, store, &sr)
			if err != nil {
				t.Fatal(err)
			}
			load := fmt.Sprintf("w.%sWireLoad(data,r)", fn)
			if retaining {
				load = fmt.Sprintf("w.%sWireLoadRetain(data,store,r)", fn)
			}
			fmt.Fprintf(&probe, `{final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));
for(var repeat=0;repeat<2;repeat++){
 final r=w.TableWireReport();final v=%s;
 check(!r.malformed&&!r.refused&&r.kindMismatch==%d&&r.unknown==%d&&r.blobShared==0&&r.retained==%d&&r.retainLost==%d,%q);
 check(w.%sToJson(v)==%q,%q);
 check(hex(w.%sWireSave(v))==%q,%q);
`, hex.EncodeToString(wire), load, row.KindMismatch, row.Unknown, r.Retained, r.RetainLost, row.Name+" report", fn, string(canonical)+"\n", row.Name+" value", fn, hex.EncodeToString(saved), row.Name+" writer")
			if retaining {
				fmt.Fprintf(&probe, ` final sr=w.TableWireReport();final saved=w.%sWireSaveRetain(v,store,sr);
 check(sr.retainLost==%d&&hex(saved)==%q,%q);
`, fn, sr.RetainLost, hex.EncodeToString(retainedSave), row.Name+" retaining writer")
			}
			probe.WriteString("}}\n")
		}
	}
	probe.WriteString("}\n")
	runFormsProbe(t, files, probe.String())
}

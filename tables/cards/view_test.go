package cards

import (
	"bytes"
	"testing"
)

type wantField struct {
	typ      string
	counted  bool
	pointer  bool
	list     bool
	bound    int32
	hasRange bool
}

var cardVocabulary = map[string]wantField{
	"id":               {"string", true, false, false, 64, false},
	"sha":              {"string", true, false, false, 40, false},
	"tier":             {"Tier", false, false, false, 0, false},
	"repo":             {"string", true, false, false, 96, false},
	"_base":            {"string", true, false, false, 96, false},
	"kind":             {"string", true, false, false, 24, false},
	"priority":         {"Priority", false, false, false, 0, false},
	"stream":           {"string", true, false, false, 32, false},
	"paths":            {"PathText", true, false, true, 0, false},
	"_new":             {"PathText", true, false, true, 0, false},
	"shared":           {"PathText", true, false, true, 0, false},
	"needs":            {"CardID", true, false, true, 0, false},
	"test":             {"string", true, false, false, 192, false},
	"done":             {"string", false, true, false, 0, false},
	"deadline_minutes": {"int32", false, false, false, 0, true},
	"source":           {"string", false, true, false, 0, false},
	"epoch":            {"int32", false, false, false, 0, false},
	"column":           {"Column", false, false, false, 0, false},
	"holder":           {"string", true, false, false, 32, false},
	"attempt":          {"int32", false, false, false, 0, false},
	"head":             {"string", true, false, false, 40, false},
	"branch":           {"string", true, false, false, 128, false},
	"body":             {"string", false, true, false, 0, false},
	"extra":            {"KeyValue", true, false, true, 0, false},
	"consumers":        {"Consumer", true, false, true, 0, false},
}

func TestViewWalksEveryField(t *testing.T) {
	t.Parallel()
	info := CardTableType()
	if len(info.Fields) != len(cardVocabulary) {
		t.Fatalf("Card has %d fields, pinned vocabulary has %d: adding a declaration field without updating the vocabulary must fail",
			len(info.Fields), len(cardVocabulary))
	}
	for _, f := range info.Fields {
		w, ok := cardVocabulary[f.Name]
		if !ok {
			t.Fatalf("field %q is not in the pinned vocabulary", f.Name)
		}
		if f.TypeName != w.typ {
			t.Fatalf("field %s: type %q, want %q", f.Name, f.TypeName, w.typ)
		}
		if f.Counted != w.counted || f.Pointer != w.pointer || f.List != w.list {
			t.Fatalf("field %s shape differs: counted=%v/%v pointer=%v/%v list=%v/%v",
				f.Name, f.Counted, w.counted, f.Pointer, w.pointer, f.List, w.list)
		}
		if !f.List && f.ArrayBound != w.bound {
			t.Fatalf("field %s bound = %d, want %d", f.Name, f.ArrayBound, w.bound)
		}
		if f.HasRange != w.hasRange {
			t.Fatalf("field %s range flag = %v, want %v", f.Name, f.HasRange, w.hasRange)
		}
	}
	uv := UnitView()
	if uv.Package != "cards" {
		t.Fatalf("registry package = %q, want cards", uv.Package)
	}
	if uv.NumTables != int32(len(uv.Tables)) {
		t.Fatalf("registry NumTables = %d, want %d", uv.NumTables, len(uv.Tables))
	}
	var cardType *TableTypeInfo
	for i := range uv.Tables {
		if uv.Tables[i].Name == "Card" {
			cardType = uv.Tables[i].Type
		}
	}
	if cardType != info {
		t.Fatal("unit registry does not name the Card table the walk reads")
	}
	enumMax := map[string]int64{"Tier": int64(TierMax), "Priority": int64(PriorityMax), "Column": int64(ColumnMax)}
	for i := range uv.Enums {
		e := &uv.Enums[i]
		want, ok := enumMax[e.Name]
		if !ok {
			t.Fatalf("registry names an undeclared enum %q", e.Name)
		}
		if e.Max != want {
			t.Fatalf("registry enum %s max = %d, want %d", e.Name, e.Max, want)
		}
	}
}

func TestViewWalksRecordValues(t *testing.T) {
	t.Parallel()
	rec := buildCfg(t, cardCfg{body: "body", paths: []string{"one", "two"}, test: "T", needs: []string{"n"}, holder: "h"})
	info := CardTableType()
	var bodyField, pathsField, holderField *TableFieldInfo
	for i := range info.Fields {
		switch info.Fields[i].Name {
		case "body":
			bodyField = &info.Fields[i]
		case "paths":
			pathsField = &info.Fields[i]
		case "holder":
			holderField = &info.Fields[i]
		}
	}
	if bodyField == nil || pathsField == nil || holderField == nil {
		t.Fatal("vocabulary missing named fields")
	}
	if !bodyField.Pointer {
		t.Fatal("body is not a pointer")
	}
	if got := bodyBytes(rec.root); !bytes.Equal(got, []byte("body")) {
		t.Fatalf("body walked %q", got)
	}
	if !pathsField.List {
		t.Fatal("paths is not a list")
	}
	if rec.root.Paths.Count != 2 {
		t.Fatalf("paths count = %d, want 2", rec.root.Paths.Count)
	}
	if string(rec.root.Holder[:rec.root.HolderLength]) != "h" {
		t.Fatalf("holder walked %q", rec.root.Holder[:rec.root.HolderLength])
	}
	if err := SetPaths(rec, []string{"one", "two", "three"}); err != nil {
		t.Fatalf("SetPaths: %v", err)
	}
	if rec.root.Paths.Count != 3 {
		t.Fatalf("paths count after edit = %d, want 3", rec.root.Paths.Count)
	}
	if got := bodyBytes(rec.root); !bytes.Equal(got, []byte("body")) {
		t.Fatalf("body after edit walked %q", got)
	}
	if _, err := TaskHash(rec); err != nil {
		t.Fatalf("TaskHash after view walk: %v", err)
	}
}

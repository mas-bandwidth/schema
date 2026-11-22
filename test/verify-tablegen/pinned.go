package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE PINNED ARRAY-DEFAULT MATRIX (docs/SPEC-TABLES.md §22.2, §3.3(a)).
//
// The random pairs above draw their arrays from `[..N]T` and `[2]Sub`, so no
// drawn schema holds a counted array with a positive minimum or a fixed array
// of a wide element, and "is this array its declared default" was held by no
// row any target ran. These units are that row, hand-written and never drawn:
// one array shape per unit, the same field in a fixed root (the message form
// and the fixed form) and in a variable root (the variable form), and one case
// per value and form. Every case is the engine's bytes: a target reads them,
// writes the value back, and owes the same bytes, so a target that stores a
// default the engine elides, or elides a value the engine stores, fails the
// case that names the shape, the value and the form.
type pinnedShape struct {
	Tag   string // unit tag, `pin<Tag>` is the package
	Decls string // declarations the element type needs
	Type  string // the field's whole type, e.g. `[2..4]int32`
	Rows  []pinnedRow
}

type pinnedRow struct {
	Name string
	Text string // the JSON text of the one field `few`, or "" for a fresh value
}

// PinnedUnit is one compiled pinned unit, in the shape the drivers take.
type PinnedUnit struct {
	Pkg      string `json:"pkg"`
	FileBase string `json:"file_base"`
	Dir      string `json:"dir"`
}

const (
	// pinnedVersion is the case layout below: 2 since fix_json became
	// opJSONMsg (the fixed root's native message writer).
	pinnedVersion  = 2
	pinnedFileBase = "Pin"
	pinnedFixRoot  = "FixRoot"
	pinnedVarRoot  = "VarRoot"
	pinnedBlkRoot  = "BlkRoot"
)

// counted is the four counts of a `[2..4]T`: the minimum at its defaults (the
// birth value, which elides), the minimum with one live element, one past the
// minimum at its defaults (which rides: the count is not the birth count), and
// the maximum.
func countedRows(d, x, y string) []pinnedRow {
	return []pinnedRow{
		{"fresh", ""},
		{"min_default", "[" + d + "," + d + "]"},
		{"min_one", "[" + d + "," + x + "]"},
		{"min_first", "[" + y + "," + d + "]"},
		{"min_plus1_default", "[" + d + "," + d + "," + d + "]"},
		{"max", "[" + x + "," + d + "," + y + "," + x + "]"},
		{"max_default", "[" + d + "," + d + "," + d + "," + d + "]"},
	}
}

// fixedRows is a `[2]T`: every element at its default (which elides where the
// element is not a table), then each slot live alone, then both.
func fixedRows(d, x, y string) []pinnedRow {
	return []pinnedRow{
		{"fresh", ""},
		{"default", "[" + d + "," + d + "]"},
		{"second", "[" + d + "," + x + "]"},
		{"first", "[" + y + "," + d + "]"},
		{"both", "[" + x + "," + y + "]"},
	}
}

const pinnedRec = "type Rec {\n    n int32 = 55\n    on bool\n}\n"
const pinnedWide = "type Wide {\n    id uint128\n    mask bits(64)\n}\n"
const pinnedEnum = "enum Tone {\n    Low\n    Mid\n    High\n}\n"

// pinnedShapes is the matrix's rows. A wide element's two live values are one
// with only its HIGH half set and one with only its low half set, because a
// default compare that reads one half answers "default" for the other.
func pinnedShapes() []pinnedShape {
	type elem struct{ tag, decls, typ, d, x, y string }
	elems := []elem{
		{"i32", "", "int32", "0", "7", "-3"},
		{"rec", pinnedRec, "Rec", "{}", `{"n":56}`, `{"on":true}`},
		{"f32", "", "float32", "0.0", "1.5", "-2.25"},
		{"f64", "", "float64", "0.0", "1.5", "-2.25"},
		{"bool", "", "bool", "false", "true", "true"},
		{"enum", pinnedEnum, "Tone", `"Low"`, `"High"`, `"Mid"`},
		{"i64", "", "int64", `"0"`, `"4294967296"`, `"-1"`},
		{"u64", "", "uint64", `"0"`, `"9223372036854775808"`, `"1"`},
		{"i128", "", "int128 | min = -34028236692093846346337460743176821145, max = 34028236692093846346337460743176821145", `"0"`, `"18446744073709551616"`, `"-1"`},
		{"u128", "", "uint128", `"0"`, `"18446744073709551616"`, `"1"`},
		{"wrec", pinnedWide, "Wide", "{}", `{"id":"18446744073709551616"}`, `{"mask":"9223372036854775808"}`},
	}
	var out []pinnedShape
	for _, e := range elems {
		out = append(out,
			pinnedShape{Tag: "c" + e.tag, Decls: e.decls, Type: "[2..4]" + e.typ, Rows: countedRows(e.d, e.x, e.y)},
			pinnedShape{Tag: "f" + e.tag, Decls: e.decls, Type: "[2]" + e.typ, Rows: fixedRows(e.d, e.x, e.y)},
		)
	}
	return out
}

func pinnedSchemaText(pkg string, s pinnedShape) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if s.Decls != "" {
		b.WriteString(s.Decls)
		b.WriteString("\n")
	}
	b.WriteString("fixed table " + pinnedBlkRoot + " {\n    a bool = true\n}\n\n")
	fmt.Fprintf(&b, "fixed table %s {\n    few %s\n}\n\n", pinnedFixRoot, s.Type)
	fmt.Fprintf(&b, "table %s {\n    few %s\n}\n", pinnedVarRoot, s.Type)
	return b.String()
}

// BuildPinned writes and compiles the pinned units under schemasDir and
// returns them with their cases, every expected byte the engine's own.
func BuildPinned(schemasDir string) ([]PinnedUnit, []CorpusCase, error) {
	var units []PinnedUnit
	var cases []CorpusCase
	for _, s := range pinnedShapes() {
		pkg := "pin" + s.Tag
		dir := filepath.Join(schemasDir, pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
		path := filepath.Join(dir, pinnedFileBase+".schema")
		text := pinnedSchemaText(pkg, s)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return nil, nil, err
		}
		c := compiler.New()
		c.FormatInPlace = false
		u, err := c.Load([]string{path})
		if err != nil {
			return nil, nil, fmt.Errorf("pinned %s: %w\n%s", pkg, err, text)
		}
		m := tabletext.NewModel(u)
		units = append(units, PinnedUnit{Pkg: pkg, FileBase: pinnedFileBase, Dir: dir})
		for _, row := range s.Rows {
			id := "pin:" + s.Tag + ":" + row.Name
			place := func(root string) (*tabletext.Instance, error) {
				inst := m.New(m.Lookup(root))
				if row.Text == "" {
					return inst, nil
				}
				var rep tabletext.Report
				src := []byte(`{"few":` + row.Text + "}\n")
				if ok := m.Read(inst, src, &rep); !ok || rep.Refused {
					return nil, fmt.Errorf("%s %s: the text did not place: %+v\n%s", id, root, rep, src)
				}
				return inst, nil
			}
			vinst, err := place(pinnedVarRoot)
			if err != nil {
				return nil, nil, err
			}
			w1, j1, err := verifyOracleVariableAndJSON(m, vinst)
			if err != nil {
				return nil, nil, fmt.Errorf("%s var: %w", id, err)
			}
			finst, err := place(pinnedFixRoot)
			if err != nil {
				return nil, nil, err
			}
			_, w3, j3, err := verifyOracleFixedRoot(m, finst)
			if err != nil {
				return nil, nil, fmt.Errorf("%s fix: %w", id, err)
			}
			w2, err := tablewire.EncodeMessages(m, []*tabletext.Instance{finst})
			if err != nil {
				return nil, nil, fmt.Errorf("%s msg: %w", id, err)
			}
			// the engine reads its own message back, or the row pins nothing
			var voc tablewire.Vocabulary
			var arep, mrep tabletext.Report
			if err := voc.AnnounceRead(tablewire.Announce(u), &arep); err != nil {
				return nil, nil, fmt.Errorf("%s announce: %w", id, err)
			}
			back := []*tabletext.Instance{m.New(m.Lookup(pinnedFixRoot))}
			if n, ok, err := tablewire.DecodeMessages(m, back, w2, &voc, &mrep); err != nil || !ok || n != 1 || mrep.Refused {
				return nil, nil, fmt.Errorf("%s msg: the engine refuses its own bytes: n=%d ok=%v rep=%+v err=%w", id, n, ok, mrep, err)
			}
			cases = append(cases,
				CorpusCase{ID: id + ":var_wire", Pkg: pkg, Root: pinnedVarRoot, Op: opWire, Input: w1, Want1: w1},
				CorpusCase{ID: id + ":var_json", Pkg: pkg, Root: pinnedVarRoot, Op: opJSON, Input: j1, Want1: j1, Want2: w1},
				CorpusCase{ID: id + ":msg", Pkg: pkg, Root: pinnedFixRoot, Op: opMsg, Input: w2, Want1: w2},
				CorpusCase{ID: id + ":fixed", Pkg: pkg, Root: pinnedFixRoot, Op: opFixed, Input: w3, Want1: w3},
				CorpusCase{ID: id + ":fix_json", Pkg: pkg, Root: pinnedFixRoot, Op: opJSONMsg, Input: j3, Want1: j3, Want2: w2},
			)
		}
	}
	return units, cases, nil
}

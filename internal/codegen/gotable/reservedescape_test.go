package gotable

// THE GO ESCAPE (SPEC.md §4.6, "The Go escape"): under --lang go a Go word is
// not refused as a field name, a union arm, an enum variant or a table name.
// Fields, arms and variants need no escape — Go emits them exported or
// prefixed — and a table so named is emitted as `<name>_`, every derived Go
// name on the escaped spelling, while its wire name, ids, JSON keys,
// projections and descriptor names stay the schema name.

import (
	"go/scanner"
	"go/token"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// goUnitFrom checks src as every command does: against every supported
// language's reserved words, the Go escape carrying Go's own (SPEC §4.6).
func goUnitFrom(t *testing.T, src string) (*ir.Unit, []error) {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	return check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
}

func goUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	u, errs := goUnitFrom(t, src)
	if len(errs) > 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

// reservedRoundTrip is a table whose name, fields and enum variants are Go
// words that no other supported language reserves (SPEC §4.6), beside a packet type with a Go-word field: every value form the
// table carries round-trips under the schema's names.
const reservedRoundTrip = `package probe
enum Kind { go, chan }
flags Caps { go, select }
type Pt { range int32 }
union Shape {
 range int32
 go Pt
}
fixed table func {
 select int32
 iota string(16)
 kind Kind
 defer ?int32
 recover Pt
 rune uint8
 uintptr [..3]int32
 fallthrough bool
 caps Caps
 shape Shape
}
`

const reservedRoundTripTest = `package probe
import ("bytes"; "encoding/json"; "testing")
func fill(v *func_) {
 func_Reset(v)
 v.Select = -7
 v.IotaLength = int32(copy(v.Iota[:], "fallthrough"))
 v.Kind = Kindchan
 v.DeferPresent = true; v.Defer = 3
 v.Recover.Range = 11
 v.Rune = 200
 v.Uintptr[0], v.Uintptr[1] = 4, 5; v.UintptrCount = 2
 v.Fallthrough = true
 v.Caps = Capsgo | Capsselect
 v.Shape.Type = ShapeTypeRange; v.Shape.Range = 9
}
func same(t *testing.T, form string, a, b *func_) {
 t.Helper()
 if a.Select != b.Select || string(a.Iota[:a.IotaLength]) != string(b.Iota[:b.IotaLength]) || a.Kind != b.Kind || a.DeferPresent != b.DeferPresent || a.Defer != b.Defer || a.Recover.Range != b.Recover.Range || a.Rune != b.Rune || a.UintptrCount != b.UintptrCount || a.Uintptr != b.Uintptr || a.Fallthrough != b.Fallthrough || a.Caps != b.Caps || a.Shape.Type != b.Shape.Type || a.Shape.Range != b.Shape.Range {
  t.Fatalf("%s round trip: got %+v want %+v", form, *b, *a)
 }
}
func TestReservedRoundTrip(t *testing.T) {
 var v func_
 fill(&v)
 // binary
 n := func_Measure(&v)
 if n <= 0 { t.Fatalf("measure %d", n) }
 wire := make([]byte, n)
 if func_Save(&v, wire) != n { t.Fatal("save") }
 var got func_
 var r TableReport
 if !func_Load(&got, wire, &r) || r.Err() != nil { t.Fatalf("load %+v", r) }
 same(t, "binary", &v, &got)
 // base64
 text, err := func_SaveText(&v)
 if err != nil { t.Fatalf("SaveText %v", err) }
 back, err := func_LoadText(text, nil)
 if err != nil { t.Fatalf("LoadText %v", err) }
 same(t, "base64", &v, back)
 // JSON, whose keys and variant names are the schema's
 js := make([]byte, func_ToJsonMeasure(&v))
 if func_ToJson(&v, js) != int64(len(js)) { t.Fatal("ToJson") }
 var flat bytes.Buffer
 if err := json.Compact(&flat, js); err != nil { t.Fatalf("JSON %s: %v", js, err) }
 for _, key := range []string{"\"select\":-7", "\"iota\":\"fallthrough\"", "\"kind\":\"chan\"", "\"defer\":", "\"recover\":{\"range\":11}", "\"rune\":", "\"uintptr\":[4,5]", "\"fallthrough\":true", "\"caps\":[\"go\",\"select\"]", "\"shape\":{\"range\":9}"} {
  if !bytes.Contains(flat.Bytes(), []byte(key)) { t.Fatalf("JSON %s lacks %s", js, key) }
 }
 if bytes.Contains(js, []byte("func_")) || bytes.Contains(js, []byte("Select")) { t.Fatalf("JSON carries a Go spelling: %s", js) }
 var fromJs func_
 r = TableReport{}
 if !func_FromJson(&fromJs, js, &r) || r.Err() != nil { t.Fatalf("FromJson %+v", r) }
 same(t, "JSON", &v, &fromJs)
 // the descriptor and the variant publish the schema names
 if func_TableType().Name != "func" { t.Fatalf("descriptor name %q", func_TableType().Name) }
}
`

// reservedShapes puts escaped tables in every position a table name reaches:
// a fixed root, a by-value field, a counted array of them, a pointer to
// itself, a map and a text list on the escaped holder, and a table arm.
const reservedShapes = `package probe
fixed table select {
 a int32
 b [2]int32
}
union Arm {
 s select
 n int32
}
table func {
 x int32
 stamp timestamp
 need int32 | required
 scores map[int32]int32 | was = "old_scores"
 next *func
 sel select
 sels [..2]select
 words []string | was = "old_words"
 data []uint16
 arm Arm
}
table Holder {
 f *func
 g select
}
`

func TestGoEscapesReservedNames(t *testing.T) {
	t.Parallel()

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		if out, err := runGeneratedUnit(t, goUnit(t, reservedRoundTrip), reservedRoundTripTest, nil); err != nil {
			t.Fatalf("generated unit: %v\n%s", err, out)
		}
	})

	t.Run("identity is the schema name", func(t *testing.T) {
		t.Parallel()
		u := goUnit(t, reservedShapes)
		esc := goEscapeTables(u)
		if esc == u {
			t.Fatal("a unit with Go-word tables came back unescaped")
		}
		// the caller's unit is untouched (another target may emit it next)
		if u.Tables["func"] == nil || u.Tables["func"].Name != "func" || u.Tables["select"] == nil {
			t.Fatal("escaping mutated the caller's unit")
		}
		for _, name := range []string{"func", "select"} {
			st := esc.Tables[name+"_"]
			if st == nil {
				t.Fatalf("table %s not escaped to %s_", name, name)
			}
			if st.DeclaredName() != name || st.WireName() != u.Tables[name].WireName() {
				t.Fatalf("table %s: declared %q wire %q", name, st.DeclaredName(), st.WireName())
			}
		}
		if ir.BuildVersion(esc) != ir.BuildVersion(u) {
			t.Fatalf("build version moved under the escape:\n%s\n--\n%s", ir.CookProjection(esc), ir.CookProjection(u))
		}
		if ir.CookProjection(esc) != ir.CookProjection(u) {
			t.Fatal("cook projection moved under the escape")
		}
		if ir.WireProjection(esc) != ir.WireProjection(u) {
			t.Fatal("wire projection moved under the escape")
		}
		for _, st := range ir.TableFixedRoots(u) {
			want := ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
			var got uint64
			for _, e := range ir.TableFixedRoots(esc) {
				if e.DeclaredName() == st.Name {
					got = ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(e)), e)
				}
			}
			if got != want {
				t.Fatalf("fixed layout hash of %s moved: %x want %x", st.Name, got, want)
			}
		}
		for name, st := range u.Tables {
			if !st.IsMapEntry() && !st.TextElem {
				continue
			}
			if got := ir.ProjectionMemberName(esc, name); got != ir.ProjectionMemberName(u, name) {
				t.Fatalf("generated %s projects as %s, want %s", name, got, ir.ProjectionMemberName(u, name))
			}
		}

		// no generated string literal spells the escape as a NAME (this is
		// also what holds the required-field and timestamp tables, which are
		// keyed and reported by the published name): the
		// descriptors, the view, the reports and the messages all publish the
		// schema name. A Go identifier derived from it (func_Row in a layout
		// assertion's message) is the Go spelling, and is allowed.
		escaped := regexp.MustCompile(`\b(func|select)_($|[^A-Za-z0-9_])`)
		files, err := golang.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			var s scanner.Scanner
			fs := token.NewFileSet()
			s.Init(fs.AddFile(n, -1, len(files[n])), files[n], nil, 0)
			for {
				pos, tok, lit := s.Scan()
				if tok == token.EOF {
					break
				}
				if tok != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit)
				if err != nil {
					v = lit
				}
				if escaped.MatchString(v) {
					t.Errorf("%s: string literal %s spells the Go escape", fs.Position(pos), lit)
				}
			}
		}
		// and the escaped unit compiles
		if out, err := runGeneratedUnit(t, u, "package probe\nimport \"testing\"\nfunc TestBuilds(t *testing.T) { var v func_; func_Reset(&v); var s select_; s.A = 1; _ = select_Measure(&s) }\n", nil); err != nil {
			t.Fatalf("generated unit: %v\n%s", err, out)
		}
	})

	t.Run("a declaration spelling the escape is refused by name", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"package probe\ntable func { a int32 }\ntable func_ { b int32 }\n",
			"package probe\ntable func { a int32 }\ntype func_ { b int32 }\n",
			"package probe\ntable func { a int32 }\ntable func_Reset { b int32 }\n",
		} {
			_, errs := goUnitFrom(t, src)
			if len(errs) == 0 {
				t.Fatalf("accepted a declaration colliding with the Go escape:\n%s", src)
			}
			msg := errs[0].Error()
			if !strings.Contains(msg, "Go escape") || !strings.Contains(msg, "func_") {
				t.Fatalf("refusal does not name the escape: %s", msg)
			}
		}
	})

	t.Run("other declaration kinds keep refusing", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"package probe\ntype select { a int32 }\n",
			"package probe\nenum go { a }\n",
			"package probe\nunion chan { a int32 }\ntable T { u chan }\n",
			"package select\ntable T { a int32 }\n",
		} {
			_, errs := goUnitFrom(t, src)
			if len(errs) == 0 || !strings.Contains(errs[0].Error(), "is a reserved word in Go") {
				t.Fatalf("want a Go reserved-word refusal for:\n%s\ngot %v", src, errs)
			}
		}
	})
}

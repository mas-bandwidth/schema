package darttable

// THE DART LEG'S FIXED-TABLE OPTIONAL AND DEFAULT ROWS (docs/roadmap.sexp node
// `fixed-tables`, rows optional-scalars, optional-arrays, scalar-defaults,
// text-bytes-flags-defaults). Each subtest is one roadmap task id and implements
// the page sentence its comment quotes: docs/SPEC-TABLES.md §3.4 (a record is
// the values in declared order; the OPTIONAL WRAPPER `?T` adds one present byte
// and rides the payload whole behind it; a string, bytes or flags default is the
// field's default) and docs/FIXED-FORM-ALGORITHM.md §3/§3.4's prefill ("a read
// is a prefill and a loop"). SPEC-TABLES §4.2 fixes the declared default.
//
// A CAPABILITY TASK OWES BOTH HALVES, and both are here: the WRITE the emitter
// lays down (`<T>FixedWriteBody`) and the READ (`<T>FixedDecode`), each asserted
// for non-default and boundary values, and the DECLARED DEFAULT the reader lands
// for an absent field — the prefill constant (`fixedPrefillBytes`) the load
// copies into the ranges the plan does not land. docs/FIXED-FORM-ALGORITHM.md §3.
// The leg's `make tables-dart-fixed-form` / `tables-dart-versioning` gates run
// the same generated unit on the Dart VM; this test proves the emitter and the
// plan in `go test`, where a gate always runs.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapDartT09Optionals(t *testing.T) {
	t.Parallel()

	rows := []struct {
		id      string
		src     string
		root    string
		want    []string
		layout  func(t *testing.T, entries []fixedLayoutEntry)
		plan    func(t *testing.T, st *ir.Struct, plan []fixedPlanEntry)
		prefill func(t *testing.T, b []byte)
	}{
		{
			// optional-scalars/dart/valid-data: "Optional scalar and enum fields:
			// valid-data write/read acceptance". §3.4's OPTIONAL WRAPPER: the
			// present byte at the field's own offset, the payload whole behind
			// it, present or not.
			id:   "optional-scalars/dart/valid-data",
			root: "OptScalars",
			src: `package probe

enum Grade
{
    Bronze
    Silver
    Gold
}

fixed table OptScalars
{
    score ?int32
    grade ?Grade
}
`,
			want: []string{
				// the WRITE: the present flag, then the payload under it
				"bool scorePresent = false;",
				"bool gradePresent = false;",
				"view.setUint8(at, value.scorePresent ? 1 : 0);",
				"if (value.scorePresent) {",
				"view.setInt32(at + 1, value.score, Endian.little);",
				"view.setUint8(at + 5, value.gradePresent ? 1 : 0);",
				"view.setUint8(at + 6, value.grade);",
				// the READ: the present flag, then the payload, then the enum
				// boundary — an ordinal past the last of three variants is None
				// and counts one clamp (§3.4).
				"value.scorePresent = view.getUint8(at) != 0;",
				"value.score = view.getInt32(at + 1, Endian.little);",
				"value.gradePresent = view.getUint8(at + 5) != 0;",
				"value.grade = view.getUint8(at + 6);",
				"if (value.grade > 3) {",
			},
			layout: func(t *testing.T, entries []fixedLayoutEntry) {
				if n := t09Kinds(entries, fixedKindOptional); n != 2 {
					t.Fatalf("optional wrappers in the layout = %d, want 2 (score, grade)", n)
				}
				for i, e := range entries {
					if e.kind != fixedKindOptional {
						continue
					}
					if e.children != 1 || i+1 >= len(entries) {
						t.Fatalf("optional wrapper %q carries %d children, want 1", e.note, e.children)
					}
					// THE PAYLOAD RIDES WHOLE BEHIND THE PRESENT BYTE (§3.4):
					// the wrapper is one present byte plus its child's size.
					if want := fixedPresentBytes + entries[i+1].size; e.size != want {
						t.Fatalf("optional wrapper %q size = %d, want %d (present + payload)", e.note, e.size, want)
					}
				}
			},
			plan: func(t *testing.T, st *ir.Struct, plan []fixedPlanEntry) {
				// THE IDENTITY PLAN MOVES THE WHOLE WRAPPER RUN — the present
				// byte is data on this build's own bytes — so it is one copy
				// over the whole body and nothing else (fixedPlanField, §3.4).
				var total int64
				for _, e := range plan {
					if e.op != fixedOpCopy {
						t.Fatalf("optional scalars identity plan op = %d, want copy", e.op)
					}
					total += e.size
				}
				if total != fixedTypeBytes(st) {
					t.Fatalf("identity plan moves %d bytes, body = %d", total, fixedTypeBytes(st))
				}
			},
			prefill: func(t *testing.T, b []byte) {
				// a writer that sent neither field leaves both present flags
				// clear and both payloads at their declared zero default.
				t09ZeroBytes(t, b, "an absent optional")
			},
		},

		{
			// optional-arrays/dart/valid-data: "Optional arrays: valid-data
			// write/read acceptance". §3.4's optional wrapper around a counted
			// array: the count sits behind the present byte, the loop walks the
			// LIVE count, and the write holds the count to its declared bound.
			id:   "optional-arrays/dart/valid-data",
			root: "OptArrays",
			src: `package probe

fixed table OptArrays
{
    values ?[..4]int32
}
`,
			want: []string{
				"bool valuesPresent = false;",
				"final Int32List values = Int32List(4);",
				"int valuesCount = 0;",
				// the WRITE: present flag, count held to the bound, live loop
				"view.setUint8(at, value.valuesPresent ? 1 : 0);",
				"if (value.valuesPresent) {",
				"assert(value.valuesCount >= 0);",
				"assert(value.valuesCount <= 4);",
				"view.setInt32(at + 1, value.valuesCount, Endian.little);",
				"for (var i = 0; i < value.valuesCount; i++) {",
				"view.setInt32(at + 5 + i * 4, value.values[i], Endian.little);",
				// the READ: present flag, the live count, then the live loop
				"value.valuesPresent = view.getUint8(at) != 0;",
				"value.valuesCount = view.getInt32(at + 1, Endian.little);",
				"value.values[i] = view.getInt32(at + 5 + i * 4, Endian.little);",
			},
			layout: func(t *testing.T, entries []fixedLayoutEntry) {
				if n := t09Kinds(entries, fixedKindOptional); n != 1 {
					t.Fatalf("optional wrappers in the layout = %d, want 1 (values)", n)
				}
				if n := t09Kinds(entries, ir.TableKindArray); n != 1 {
					t.Fatalf("the array kind under the wrapper = %d, want 1", n)
				}
			},
			plan: func(t *testing.T, st *ir.Struct, plan []fixedPlanEntry) {
				counts := 0
				for _, e := range plan {
					if e.op == fixedOpCount {
						counts++
						if e.size != 4 {
							t.Fatalf("the count entry's bound = %d, want 4", e.size)
						}
					}
				}
				if counts != 1 {
					t.Fatalf("plan count entries = %d, want 1 (values)", counts)
				}
			},
			prefill: func(t *testing.T, b []byte) {
				// an absent optional array reads with a clear present flag and
				// a live count of zero.
				t09ZeroBytes(t, b, "an absent optional array")
			},
		},

		{
			// scalar-defaults/dart/valid-data: "Scalar and enum defaults:
			// valid-data write/read acceptance". §3.4 template; ALGORITHM §3:
			// every declared default is folded into the prefill constant, and
			// the enum default is the variant's position in the layout, from 1.
			id:   "scalar-defaults/dart/valid-data",
			root: "Scalars",
			src: `package probe

enum Level
{
    Low
    Mid
    High
}

fixed table Scalars
{
    count  int32 = 7
    ratio  float32 = 0.5
    level  Level = High
    flag   bool = true
}
`,
			want: []string{
				// the WRITE: the declared defaults are the constructed values,
				// and init restores them in place
				"int count = 7;",
				"double ratio = 0.5;",
				"int level = Level.high;",
				"bool flag = true;",
				"void initScalars(Scalars value) {",
				"value.count = 7;",
				"value.ratio = 0.5;",
				"value.level = Level.high;",
				"value.flag = true;",
				// the READ: the enum boundary past the last of three variants
				"value.count = view.getInt32(at, Endian.little);",
				"value.ratio = view.getFloat32(at + 4, Endian.little);",
				"value.level = view.getUint8(at + 8);",
				"if (value.level > 3) {",
				"value.flag = view.getUint8(at + 9) != 0;",
			},
			prefill: func(t *testing.T, b []byte) {
				if len(b) != 10 {
					t.Fatalf("prefill = %d bytes, want the 10-byte body", len(b))
				}
				// count int32 = 7, little-endian
				t09Bytes(t, b, 0, 7, 0, 0, 0)
				// ratio float32 = 0.5 -> 0x3F000000, little-endian
				t09Bytes(t, b, 4, 0x00, 0x00, 0x00, 0x3f)
				// level = High, the THIRD variant, ordinal 3 (§3.4)
				t09Bytes(t, b, 8, 3)
				// flag = true
				t09Bytes(t, b, 9, 1)
			},
		},

		{
			// text-bytes-flags-defaults/dart/valid-data: "String, byte-buffer and
			// flags defaults: valid-data write/read acceptance". §3.4; SPEC §4.2.
			// ALGORITHM §3 prefill: the string's and bytes' declared defaults are
			// both the LENGTH and the bytes; a flags default is its mask.
			id:   "text-bytes-flags-defaults/dart/valid-data",
			root: "TextDefaults",
			src: `package probe

flags Caps { Jump, Crouch, Fly }

fixed table TextDefaults
{
    name string(8) = "new"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Fly }
}
`,
			want: []string{
				// the WRITE: the declared default is the constructed value
				`// the declared default (SPEC §4.2): "new"`,
				"final Uint8List name = Uint8List(8)",
				"..[0] = 0x6e",
				"..[1] = 0x65",
				"..[2] = 0x77;",
				"int nameLength = 3;",
				`// the declared default (SPEC §4.2): "ab"`,
				"..[0] = 0x61",
				"..[1] = 0x62;",
				"int tagLength = 2;",
				"int caps = 5;",
				// the READ: the live length, clamped to the declared capacity,
				// and the bytes copied out of the image
				"value.nameLength = view.getInt32(at, Endian.little);",
				"} else if (value.nameLength > 8) {",
				"value.name.setRange(0, 8, image, at + 4);",
				"value.tagLength = view.getInt32(at + 12, Endian.little);",
				"} else if (value.tagLength > 4) {",
				"value.tag.setRange(0, 4, image, at + 16);",
				"value.caps = view.getUint64(at + 20, Endian.little);",
			},
			prefill: func(t *testing.T, b []byte) {
				if len(b) != 28 {
					t.Fatalf("prefill = %d bytes, want the 28-byte body", len(b))
				}
				// name: the length 3, then "new", then the declared slack zero
				t09Bytes(t, b, 0, 3, 0, 0, 0)
				t09Bytes(t, b, 4, 'n', 'e', 'w')
				t09Bytes(t, b, 7, 0, 0, 0, 0, 0)
				// tag: the length 2, then "ab", then the declared slack zero
				t09Bytes(t, b, 12, 2, 0, 0, 0)
				t09Bytes(t, b, 16, 'a', 'b')
				t09Bytes(t, b, 18, 0, 0)
				// caps: Jump | Fly = 1 | 4 = 5, a u64
				t09Bytes(t, b, 20, 5, 0, 0, 0, 0, 0, 0, 0)
			},
		},
	}

	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, row.src)
			st := findTable(t, u, row.root)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("%s: generate: %v", row.id, err)
			}
			var src strings.Builder
			for _, data := range files {
				src.Write(data)
			}
			all := src.String()
			for _, w := range row.want {
				if !strings.Contains(all, w) {
					t.Fatalf("%s: the generated Dart does not carry %q", row.id, w)
				}
			}
			if row.layout != nil {
				row.layout(t, fixedWalkRoot(st).entries)
			}
			if row.plan != nil {
				row.plan(t, st, fixedIdentityPlan(st))
			}
			if row.prefill != nil {
				b := fixedPrefillBytes(st)
				if int64(len(b)) != fixedTypeBytes(st) {
					t.Fatalf("%s: prefill = %d bytes, body = %d", row.id, len(b), fixedTypeBytes(st))
				}
				row.prefill(t, b)
			}
		})
	}
}

// t09Kinds counts the layout entries of one kind.
func t09Kinds(entries []fixedLayoutEntry, kind int) int {
	n := 0
	for _, e := range entries {
		if e.kind == kind {
			n++
		}
	}
	return n
}

// t09Bytes asserts the bytes at an offset.
func t09Bytes(t *testing.T, b []byte, off int, want ...byte) {
	t.Helper()
	if off+len(want) > len(b) {
		t.Fatalf("prefill is %d bytes, shorter than the assertion at %d+%d", len(b), off, len(want))
	}
	for i, w := range want {
		if b[off+i] != w {
			t.Fatalf("prefill[%d] = 0x%02x, want 0x%02x", off+i, b[off+i], w)
		}
	}
}

// t09ZeroBytes asserts every byte is zero.
func t09ZeroBytes(t *testing.T, b []byte, what string) {
	t.Helper()
	for i, v := range b {
		if v != 0 {
			t.Fatalf("%s: prefill[%d] = 0x%02x, want the default 0x00", what, i, v)
		}
	}
}

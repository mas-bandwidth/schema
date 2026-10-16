package gotable

// THE COMPRESSED FLOAT'S SPECIAL VALUES across all four forms
// (docs/SPEC-TABLES.md §3.3, §3.4, §4, §16.2; SPEC.md §4.3).
//
// In every form (message, fixed, variable cell, text/JSON), a compressed float
// (`float32 | min = ..., max = ..., resolution = ...`):
//   * never lands NaN silently: NaN clamps to min (counted) on wire load /
//     quantizes to min on message write, and is refused by name on JSON write;
//   * never keeps -0's sign bit: -0 lands as +0 (the quantized value of 0 on
//     that range: index 0 on [0, 1], index 100 on [-1, 1] at 0.01);
//   * clamps +Inf and finite > max to max (counted on load), and -Inf and
//     finite < min to min (counted on load), and refuses +-Inf by name on JSON
//     write.

import (
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type cfloatRangeSpec struct {
	name     string
	min, max float32
	res      float32
	bits     int64
	count    uint32
	zeroIdx  uint32
	minBits  uint32
	maxBits  uint32
}

var cfloatRanges = []cfloatRangeSpec{
	{
		name:    "unit_0_1",
		min:     0,
		max:     1,
		res:     0.01,
		bits:    7,
		count:   100,
		zeroIdx: 0,
		minBits: 0x00000000, // +0.0
		maxBits: 0x3F800000, // +1.0
	},
	{
		name:    "signed_m1_1",
		min:     -1,
		max:     1,
		res:     0.01,
		bits:    8,
		count:   200,
		zeroIdx: 100,
		minBits: 0xBF800000, // -1.0
		maxBits: 0x3F800000, // +1.0
	},
}

const cfloatProbeSchema = `package probe

fixed table FixedUnit {
	v float32 | min = 0, max = 1, resolution = 0.01
}

fixed table FixedSigned {
	v float32 | min = -1, max = 1, resolution = 0.01
}

fixed table VarUnit {
	v [2]float32 | min = 0, max = 1, resolution = 0.01
}

fixed table VarSigned {
	v [2]float32 | min = -1, max = 1, resolution = 0.01
}
`

func TestCompressedFloatSpecialValuesGo(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, cfloatProbeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableSrc := string(files["ProbeTable.go"])
	jsonSrc := string(files["ProbeTableJson.go"])

	for _, rng := range cfloatRanges {
		rng := rng
		t.Run(rng.name, func(t *testing.T) {
			t.Parallel()

			// ---- 1. MESSAGE FORM ---------------------------------------------
			t.Run("message", func(t *testing.T) {
				t.Parallel()
				s := ir.TableMessageShape{
					Packing: ir.TableMessageQuantized,
					Bits:    rng.bits,
					QMin:    rng.min,
					QMax:    rng.max,
					QRes:    rng.res,
				}
				count, _, ok := ir.TableMessageQuantization(s)
				if !ok || count != rng.count {
					t.Fatalf("quantization count = (%d, %v), want (%d, true)", count, ok, rng.count)
				}
				rows := []struct {
					name string
					in   uint32
					idx  uint32
					out  uint32
				}{
					{"plus_zero", 0x00000000, rng.zeroIdx, 0x00000000},
					{"negative_zero", 0x80000000, rng.zeroIdx, 0x00000000},
					{"nan_quiet", 0x7FC00000, 0, rng.minBits},
					{"nan_payload", 0x7FC0DEAD, 0, rng.minBits},
					{"plus_inf", 0x7F800000, rng.count, rng.maxBits},
					{"minus_inf", 0xFF800000, 0, rng.minBits},
					{"below_min", 0xC0A00000, 0, rng.minBits}, // -5.0
					{"above_max", 0x40000000, rng.count, rng.maxBits}, // +2.0
				}
				for _, r := range rows {
					r := r
					t.Run(r.name, func(t *testing.T) {
						t.Parallel()
						v := math.Float32frombits(r.in)
						idx := ir.TableMessageQuantize(s, v)
						if idx != r.idx {
							t.Fatalf("quantize(%s = %08x) = index %d, want %d", r.name, r.in, idx, r.idx)
						}
						if idx > rng.count {
							t.Fatalf("index %d overflows count %d", idx, rng.count)
						}
						dq := ir.TableMessageDequantize(s, idx)
						if got := math.Float32bits(dq); got != r.out {
							t.Fatalf("dequantize(index %d) = %08x (%v), want %08x", idx, got, dq, r.out)
						}
					})
				}

				// Also verify message decode when an older unquantized writer
				// sends raw float32 bits (NaN, -0, +0, +-Inf) into a compressed
				// float reader via tablewire.DecodeMessages.
				rawSchema := fmt.Sprintf("package probe\nfixed table FixedRaw {\n\tv [2]float32\n}\n")
				compSchema := fmt.Sprintf("package probe\nfixed table FixedRaw {\n\tv [2]float32 | min = %g, max = %g, resolution = %g\n}\n", rng.min, rng.max, rng.res)
				mRaw := tabletext.NewModel(unitFrom(t, rawSchema))
				mComp := tabletext.NewModel(unitFrom(t, compSchema))
				rawAnn := tablewire.Announce(mRaw.Unit)
				var vocabReport tabletext.Report
				var vocab tablewire.Vocabulary
				if err := vocab.AnnounceRead(rawAnn, &vocabReport); err != nil || !vocab.Announced() || vocabReport.Malformed {
					t.Fatalf("AnnounceRead failed: err=%v rep=%+v", err, vocabReport)
				}
				for _, r := range []struct {
					name    string
					in      uint32
					want    uint32
					clamped int
				}{
					{"raw_plus_zero", 0x00000000, 0x00000000, 0},
					{"raw_negative_zero", 0x80000000, 0x00000000, 0},
					{"raw_nan_quiet", 0x7FC00000, rng.minBits, 1},
					{"raw_nan_payload", 0x7FC0DEAD, rng.minBits, 1},
					{"raw_plus_inf", 0x7F800000, rng.maxBits, 1},
					{"raw_minus_inf", 0xFF800000, rng.minBits, 1},
				} {
					r := r
					t.Run(r.name, func(t *testing.T) {
						t.Parallel()
						inst := mRaw.New(mRaw.Lookup("FixedRaw"))
						inst.Fields[0].Elems[0].F = widenBitsForTest(r.in)
						inst.Fields[0].Elems[1].F = 0.5 // ensure fixed array rides
						batch, err := tablewire.EncodeMessages(mRaw, []*tabletext.Instance{inst})
						if err != nil {
							t.Fatalf("%s EncodeMessages: %v", r.name, err)
						}
						var rep tabletext.Report
						outInst := mComp.New(mComp.Lookup("FixedRaw"))
						n, ok, err := tablewire.DecodeMessages(mComp, []*tabletext.Instance{outInst}, batch, &vocab, &rep)
						if err != nil || !ok || n != 1 {
							t.Fatalf("%s DecodeMessages failed: n=%d ok=%v err=%v rep=%+v", r.name, n, ok, err, rep)
						}
						gotBits := math.Float32bits(float32(outInst.Fields[0].Elems[0].F))
						if gotBits != r.want || rep.Clamped != r.clamped {
							t.Fatalf("%s raw->compressed message: got bits=%08x clamped=%d, want bits=%08x clamped=%d",
								r.name, gotBits, rep.Clamped, r.want, r.clamped)
						}
					})
				}
			})

			// ---- 2. FIXED FORM -----------------------------------------------
			t.Run("fixed", func(t *testing.T) {
				t.Parallel()
				fixedTableName := "FixedUnit"
				if rng.min < 0 {
					fixedTableName = "FixedSigned"
				}
				clampFn := funcSource(tableSrc, "func "+fixedTableName+"FixedClampBody(")
				if clampFn == "" {
					t.Fatalf("missing %sFixedClampBody in emitted Go table source", fixedTableName)
				}
				t.Run("codegen", func(t *testing.T) {
					lo := formatFloat32(float64(rng.min))
					if !strings.Contains(clampFn, fmt.Sprintf("if !(value.V >= %s)", lo)) || !strings.Contains(clampFn, "else if value.V == 0 {") {
						t.Fatalf("emitted %sFixedClampBody for %s does not clamp NaN / strip -0 sign bit:\n%s", fixedTableName, rng.name, clampFn)
					}
				})
				for _, r := range []struct {
					name    string
					in      uint32
					want    uint32
					clamped int32
				}{
					{"plus_zero", 0x00000000, 0x00000000, 0},
					{"negative_zero", 0x80000000, 0x00000000, 0},
					{"nan_quiet", 0x7FC00000, rng.minBits, 1},
					{"nan_payload", 0x7FC0DEAD, rng.minBits, 1},
					{"plus_inf", 0x7F800000, rng.maxBits, 1},
					{"minus_inf", 0xFF800000, rng.minBits, 1},
					{"below_min", 0xC0A00000, rng.minBits, 1},
					{"above_max", 0x40000000, rng.maxBits, 1},
				} {
					r := r
					t.Run(r.name, func(t *testing.T) {
						t.Parallel()
						gotBits, clamped := evalEmittedFixedClampBody(t, clampFn, r.in)
						if gotBits != r.want || clamped != r.clamped {
							t.Fatalf("fixed %s: got bits=%08x (%v) clamped=%d, want bits=%08x clamped=%d",
								r.name, gotBits, math.Float32frombits(gotBits), clamped, r.want, r.clamped)
						}
					})
				}
			})

			// ---- 3. VARIABLE CELL --------------------------------------------
			t.Run("variable", func(t *testing.T) {
				t.Parallel()
				t.Run("codegen", func(t *testing.T) {
					lo := formatFloat32(float64(rng.min))
					if !strings.Contains(tableSrc, fmt.Sprintf("if !(v >= %s)", lo)) || !strings.Contains(tableSrc, "else if v == 0 {") {
						t.Fatalf("emitted variable reader for %s does not clamp NaN / strip -0 sign bit", rng.name)
					}
				})

				m := tabletext.NewModel(u)
				tableName := "VarUnit"
				if rng.min < 0 {
					tableName = "VarSigned"
				}
				st := m.Lookup(tableName)
				rows := []struct {
					name    string
					in      uint32
					want    uint32
					clamped int
				}{
					{"plus_zero", 0x00000000, 0x00000000, 0},
					{"negative_zero", 0x80000000, 0x00000000, 0},
					{"nan_quiet", 0x7FC00000, rng.minBits, 1},
					{"nan_payload", 0x7FC0DEAD, rng.minBits, 1},
					{"plus_inf", 0x7F800000, rng.maxBits, 1},
					{"minus_inf", 0xFF800000, rng.minBits, 1},
					{"below_min", 0xC0A00000, rng.minBits, 1},
					{"above_max", 0x40000000, rng.maxBits, 1},
				}
				for _, r := range rows {
					r := r
					t.Run(r.name, func(t *testing.T) {
						t.Parallel()
						inst := m.New(st)
						inst.Fields[0].Elems[0].F = widenBitsForTest(r.in)
						inst.Fields[0].Elems[1].F = 0.5 // ensure array rides on the wire
						wire, err := tablewire.Encode(m, inst)
						if err != nil {
							t.Fatalf("Encode(%s): %v", r.name, err)
						}
						decoded := m.New(st)
						var rep tabletext.Report
						ok, err := tablewire.Decode(m, decoded, wire, &rep)
						if err != nil || !ok || rep.Malformed {
							t.Fatalf("Decode(%s) failed: err=%v rep=%+v", r.name, err, rep)
						}
						gotBits := math.Float32bits(float32(decoded.Fields[0].Elems[0].F))
						if gotBits != r.want || rep.Clamped != r.clamped {
							t.Fatalf("variable %s: got bits=%08x (%v) clamped=%d, want bits=%08x clamped=%d",
								r.name, gotBits, math.Float32frombits(gotBits), rep.Clamped, r.want, r.clamped)
						}
					})
				}
			})

			// ---- 4. TEXT / JSON ----------------------------------------------
			t.Run("text_json", func(t *testing.T) {
				t.Parallel()
				t.Run("codegen", func(t *testing.T) {
					if !strings.Contains(jsonSrc, "if f.HasRange && v == 0 {\n\t\t\tv = 0\n\t\t}") {
						t.Fatal("emitted tableJsonWriteScalar does not strip -0 sign bit for ranged floats")
					}
					if !strings.Contains(jsonSrc, "else if value == 0 {\n\t\t\t\tvalue = 0\n\t\t\t}") {
						t.Fatal("emitted tableJsonReadScalar does not strip -0 sign bit for ranged floats")
					}
				})

				m := tabletext.NewModel(u)
				tableName := "FixedUnit"
				if rng.min < 0 {
					tableName = "FixedSigned"
				}
				st := m.Lookup(tableName)

				// Non-finite values (NaN, +Inf, -Inf) are refused by name on JSON write.
				for _, nf := range []struct {
					name string
					bits uint32
				}{
					{"write_refuse_nan_quiet", 0x7FC00000},
					{"write_refuse_nan_payload", 0x7FC0DEAD},
					{"write_refuse_plus_inf", 0x7F800000},
					{"write_refuse_minus_inf", 0xFF800000},
				} {
					nf := nf
					t.Run(nf.name, func(t *testing.T) {
						inst := m.New(st)
						inst.Fields[0].Cell.F = widenBitsForTest(nf.bits)
						if _, err := m.Write(inst); err == nil {
							t.Fatalf("JSON Write(%s = %08x) succeeded; want refusal", nf.name, nf.bits)
						}
					})
				}

				// -0 writes as "0" (no "-0") and reads back as +0 (0x00000000).
				t.Run("write_negative_zero", func(t *testing.T) {
					negZeroInst := m.New(st)
					negZeroInst.Fields[0].Cell.F = math.Copysign(0, -1)
					written, err := m.Write(negZeroInst)
					if err != nil {
						t.Fatalf("JSON Write(-0): %v", err)
					}
					if strings.Contains(string(written), "-0") {
						t.Fatalf("JSON Write(-0) emitted negative zero in text: %s", written)
					}
				})

				for _, tc := range []struct {
					name    string
					jsonIn  string
					want    uint32
					clamped int
				}{
					{"read_plus_zero", `{"v": 0}`, 0x00000000, 0},
					{"read_negative_zero", `{"v": -0.0}`, 0x00000000, 0},
					{"read_below_min", `{"v": -5}`, rng.minBits, 1},
					{"read_above_max", `{"v": 2}`, rng.maxBits, 1},
				} {
					tc := tc
					t.Run(tc.name, func(t *testing.T) {
						decoded := m.New(st)
						var rep tabletext.Report
						if !m.Read(decoded, []byte(tc.jsonIn), &rep) || rep.Malformed || rep.KindMismatch != 0 {
							t.Fatalf("JSON Read(%s): rep=%+v", tc.name, rep)
						}
						gotBits := math.Float32bits(float32(decoded.Fields[0].Cell.F))
						// Also check the raw float64 sign bit in Cell.F so -0 never hides in float64 either.
						if tc.want == 0 && math.Signbit(decoded.Fields[0].Cell.F) {
							t.Fatalf("JSON Read(%s) kept negative sign bit in Cell.F", tc.name)
						}
						if gotBits != tc.want || rep.Clamped != tc.clamped {
							t.Fatalf("JSON Read(%s): got bits=%08x clamped=%d, want bits=%08x clamped=%d",
								tc.name, gotBits, rep.Clamped, tc.want, tc.clamped)
						}
					})
				}
			})
		})
	}
}

func widenBitsForTest(bits uint32) float64 {
	if bits&0x7F800000 == 0x7F800000 && bits&0x007FFFFF != 0 {
		sign := uint64(bits>>31) << 63
		payload := uint64(bits&0x007FFFFF) << (52 - 23)
		return math.Float64frombits(sign | 0x7FF0000000000000 | payload)
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], bits)
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[:])))
}

// evalEmittedFixedClampBody parses the emitted `<Table>FixedClampBody` Go
// function and executes its AST directly on a single float32 field `value.V`
// initialized to `inBits`, returning the resulting `math.Float32bits(value.V)`
// and `*clamped`. This runs in-process without shelling out or skipping behind
// slowtest.Gate.
func evalEmittedFixedClampBody(t *testing.T, fnSrc string, inBits uint32) (uint32, int32) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "clamp.go", "package p\n"+fnSrc, 0)
	if err != nil {
		t.Fatalf("parse FixedClampBody: %v\n%s", err, fnSrc)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatalf("no FuncDecl in:\n%s", fnSrc)
	}
	val := math.Float32frombits(inBits)
	clamped := int32(0)

	var evalF32 func(e ast.Expr) float32
	evalF32 = func(e ast.Expr) float32 {
		switch x := e.(type) {
		case *ast.ParenExpr:
			return evalF32(x.X)
		case *ast.SelectorExpr:
			return val
		case *ast.BasicLit:
			f, err := strconv.ParseFloat(x.Value, 32)
			if err != nil {
				t.Fatalf("parse float literal %q: %v", x.Value, err)
			}
			return float32(f)
		case *ast.UnaryExpr:
			if x.Op == token.SUB {
				return -evalF32(x.X)
			}
		}
		t.Fatalf("unsupported float32 expr %T", e)
		return 0
	}

	var evalCond func(e ast.Expr) bool
	evalCond = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.ParenExpr:
			return evalCond(x.X)
		case *ast.UnaryExpr:
			if x.Op == token.NOT {
				return !evalCond(x.X)
			}
		case *ast.BinaryExpr:
			l, r := evalF32(x.X), evalF32(x.Y)
			switch x.Op {
			case token.LSS:
				return l < r
			case token.GTR:
				return l > r
			case token.LEQ:
				return l <= r
			case token.GEQ:
				return l >= r
			case token.EQL:
				return l == r
			case token.NEQ:
				return l != r
			}
		}
		t.Fatalf("unsupported cond expr %T", e)
		return false
	}

	var execBlock func(stmts []ast.Stmt)
	var execIf func(s *ast.IfStmt)
	execIf = func(s *ast.IfStmt) {
		if evalCond(s.Cond) {
			execBlock(s.Body.List)
			return
		}
		switch el := s.Else.(type) {
		case *ast.IfStmt:
			execIf(el)
		case *ast.BlockStmt:
			execBlock(el.List)
		}
	}
	execBlock = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.IfStmt:
				execIf(s)
			case *ast.AssignStmt:
				if sel, ok := s.Lhs[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "V" {
					val = evalF32(s.Rhs[0])
				}
			case *ast.IncDecStmt:
				if s.Tok == token.INC {
					clamped++
				}
			}
		}
	}
	execBlock(fn.Body.List)
	return math.Float32bits(val), clamped
}


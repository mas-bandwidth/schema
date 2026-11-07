package fuzz_test

// tablebounds_test.go is the per-field bounds pass of the random-schema
// differential harness (card schema-go-fuzz-random-table-differential-bounds-pass).
// For each seed it generates a random valid unit with randomSchema, compiles
// the Go leg via compiler.New().Generate(u, "go", nil), and runs a probe
// through runGeneratedTableUnit under -race. For each scalar field of Root,
// the probe writes the {} document with that one field set to each of:
// min-1, max+1, the implied extremes plus one, 1e400, 2.5, "7" and null,
// and reads it with RootFromJSON (plus RootFromJSONTolerant to verify clamp
// and kind-mismatch storage invariants).

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type boundCase struct {
	name string
	mode string // "must_accept", "must_clamp", "must_kind_mismatch", "must_refuse_named", "must_unknown"
	text string
	// want is the single-field JSON document the read must store for a
	// must_clamp case: a clamp keeps the value at the declared/implied bound.
	want string
}

func isRootScalarField(f *genField) bool {
	switch f.Kind {
	case gInt, gUint, gFloat32, gFloat64, gBool, gEnum, gString, gBytes, gTimestamp:
		return true
	default:
		return false
	}
}

func impliedIntBounds(bits int) (lo, hi *big.Int) {
	// [-2^(bits-1), 2^(bits-1)-1]
	one := big.NewInt(1)
	half := new(big.Int).Lsh(one, uint(bits-1))
	lo = new(big.Int).Neg(half)
	hi = new(big.Int).Sub(half, one)
	return lo, hi
}

func impliedUintBounds(bits int) (lo, hi *big.Int) {
	one := big.NewInt(1)
	top := new(big.Int).Lsh(one, uint(bits))
	lo = big.NewInt(0)
	hi = new(big.Int).Sub(top, one)
	return lo, hi
}

func singleFieldDoc(fieldName, rawVal string) string {
	return fmt.Sprintf("{%q:%s}", fieldName, rawVal)
}

// intClampWant returns the JSON spellings of the lower and upper clamp targets
// for a signed integer field: the declared bounds when a range is declared,
// else the storage-width implied bounds.
func intClampWant(f *genField) (lo, hi string) {
	if f.IMode == mExtreme || f.IMode == mExclDefault {
		return strconv.FormatInt(f.Imin, 10), strconv.FormatInt(f.Imax, 10)
	}
	impLo, impHi := impliedIntBounds(f.Bits)
	return impLo.String(), impHi.String()
}

// uintClampWant is intClampWant for unsigned fields.
func uintClampWant(f *genField) (lo, hi string) {
	if f.IMode == mExtreme || f.IMode == mExclDefault {
		umin, umax, _, _ := uintBounds(f.Bits, f.IMode)
		return strconv.FormatUint(umin, 10), strconv.FormatUint(umax, 10)
	}
	impLo, impHi := impliedUintBounds(f.Bits)
	return impLo.String(), impHi.String()
}

func inRangeRawValue(f *genField) string {
	switch f.Kind {
	case gInt:
		return strconv.FormatInt(f.Imin, 10)
	case gUint:
		min, _, _, _ := uintBounds(f.Bits, f.IMode)
		return strconv.FormatUint(min, 10)
	case gFloat32, gFloat64:
		return "2.5"
	case gBool:
		return "true"
	case gEnum:
		return fmt.Sprintf("%q", f.EnumVal)
	case gString:
		return `"7"`
	case gBytes:
		return `""`
	case gTimestamp:
		return `"1970-01-01T00:00:00Z"`
	default:
		return "0"
	}
}

func buildFieldBoundCases(u *genUnit, sabotage bool) []boundCase {
	var cases []boundCase
	one := big.NewInt(1)

	if sabotage {
		// Find the first scalar field of Root (or synthesize an in-range check on "{}")
		// and tell the probe to expect refusal on an in-range value.
		for _, f := range u.Root.Fields {
			if isRootScalarField(f) {
				cases = append(cases, boundCase{
					name: "sabotage_in_range_" + f.Name,
					mode: "must_clamp",
					text: singleFieldDoc(f.Name, inRangeRawValue(f)),
				})
				// The bound-oracle negative control: a clamp case whose expected
				// post-clamp value is deliberately the adjacent in-range value,
				// so the gate must go red on the want mismatch.
				if cc := sabotageClampWantCase(u); cc != nil {
					cases = append(cases, *cc)
				}
				return cases
			}
		}
		cases = append(cases, boundCase{
			name: "sabotage_empty_root",
			mode: "must_clamp",
			text: "{}",
		})
		return cases
	}

	for _, f := range u.Root.Fields {
		if !isRootScalarField(f) {
			continue
		}
		fn := f.Name
		switch f.Kind {
		case gInt:
			impLo, impHi := impliedIntBounds(f.Bits)
			impLoMinus1 := new(big.Int).Sub(impLo, one).String()
			impHiPlus1 := new(big.Int).Add(impHi, one).String()

			// A declared min-1/max+1 only exists when a range is declared.
			// mNone and mBothZero declare no range (the whole storage width, or
			// a bare default), so their Imin/Imax are not what a clamp guards.
			if f.IMode == mExtreme || f.IMode == mExclDefault {
				minMinus1 := new(big.Int).Sub(big.NewInt(f.Imin), one).String()
				maxPlus1 := new(big.Int).Add(big.NewInt(f.Imax), one).String()
				cases = append(cases,
					boundCase{name: fn + "_min_minus_1", mode: "must_clamp", text: singleFieldDoc(fn, minMinus1), want: singleFieldDoc(fn, strconv.FormatInt(f.Imin, 10))},
					boundCase{name: fn + "_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, maxPlus1), want: singleFieldDoc(fn, strconv.FormatInt(f.Imax, 10))},
				)
			}
			// An implied case clamps to the declared bound when a range is
			// declared (a value past the declared range stops there), else to
			// the storage-width implied bound.
			wantLo, wantHi := intClampWant(f)
			cases = append(cases,
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_clamp", text: singleFieldDoc(fn, impLoMinus1), want: singleFieldDoc(fn, wantLo)},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, impHiPlus1), want: singleFieldDoc(fn, wantHi)},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gUint:
			impLo, impHi := impliedUintBounds(f.Bits)
			impLoMinus1 := new(big.Int).Sub(impLo, one).String()
			impHiPlus1 := new(big.Int).Add(impHi, one).String()

			if f.IMode == mExtreme || f.IMode == mExclDefault {
				umin, umax, _, _ := uintBounds(f.Bits, f.IMode)
				minMinus1 := new(big.Int).Sub(new(big.Int).SetUint64(umin), one).String()
				maxPlus1 := new(big.Int).Add(new(big.Int).SetUint64(umax), one).String()
				cases = append(cases,
					boundCase{name: fn + "_min_minus_1", mode: "must_clamp", text: singleFieldDoc(fn, minMinus1), want: singleFieldDoc(fn, strconv.FormatUint(umin, 10))},
					boundCase{name: fn + "_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, maxPlus1), want: singleFieldDoc(fn, strconv.FormatUint(umax, 10))},
				)
			}
			wantLo, wantHi := uintClampWant(f)
			cases = append(cases,
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_clamp", text: singleFieldDoc(fn, impLoMinus1), want: singleFieldDoc(fn, wantLo)},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, impHiPlus1), want: singleFieldDoc(fn, wantHi)},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gFloat32:
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-3.5e38")},
				boundCase{name: fn + "_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "3.5e38")},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-3.5e38")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "3.5e38")},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_accept", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gFloat64:
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1e309")},
				boundCase{name: fn + "_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e309")},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1e309")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e309")},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_accept", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gBool:
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2")},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2")},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gEnum:
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "3")},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "3")},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_unknown", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gString:
			overStr := fmt.Sprintf("%q", strings.Repeat("a", f.StrN+1))
			wantStr := fmt.Sprintf("%q", strings.Repeat("a", f.StrN))
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, overStr), want: singleFieldDoc(fn, wantStr)},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, overStr), want: singleFieldDoc(fn, wantStr)},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_accept", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gBytes:
			overBytes := fmt.Sprintf("%q", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x62}, f.StrN+1)))
			wantBytes := fmt.Sprintf("%q", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x62}, f.StrN)))
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, overBytes), want: singleFieldDoc(fn, wantBytes)},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "-1")},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_clamp", text: singleFieldDoc(fn, overBytes), want: singleFieldDoc(fn, wantBytes)},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_kind_mismatch", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		case gTimestamp:
			cases = append(cases,
				boundCase{name: fn + "_min_minus_1", mode: "must_refuse_named", text: singleFieldDoc(fn, `"1677-09-21T00:12:43.145224191Z"`)},
				boundCase{name: fn + "_max_plus_1", mode: "must_refuse_named", text: singleFieldDoc(fn, `"2262-04-11T23:47:16.854775808Z"`)},
				boundCase{name: fn + "_implied_min_minus_1", mode: "must_refuse_named", text: singleFieldDoc(fn, `"1677-09-21T00:12:43.145224191Z"`)},
				boundCase{name: fn + "_implied_max_plus_1", mode: "must_refuse_named", text: singleFieldDoc(fn, `"2262-04-11T23:47:16.854775808Z"`)},
				boundCase{name: fn + "_1e400", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "1e400")},
				boundCase{name: fn + "_2_5", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "2.5")},
				boundCase{name: fn + "_str_7", mode: "must_refuse_named", text: singleFieldDoc(fn, `"7"`)},
				boundCase{name: fn + "_null", mode: "must_kind_mismatch", text: singleFieldDoc(fn, "null")},
			)
		}
	}
	return cases
}

// sabotageClampWantCase builds a clamp case with an intentionally wrong
// expected post-clamp value (the adjacent in-range value) for the first Root
// integer scalar field, demonstrating the independent bound oracle: the probe
// must observe a (boundsclampwanted divergence when the stored clamp lands at
// the bound but the case expects the adjacent in-range value.
func sabotageClampWantCase(u *genUnit) *boundCase {
	one := big.NewInt(1)
	for _, f := range u.Root.Fields {
		switch f.Kind {
		case gInt:
			impLo, _ := impliedIntBounds(f.Bits)
			belowMin := new(big.Int).Sub(impLo, one).String()
			wantLo, _ := intClampWant(f)
			wantBound, _ := new(big.Int).SetString(wantLo, 10)
			wantAdj := new(big.Int).Add(wantBound, one).String()
			return &boundCase{
				name: "sabotage_clamp_want_" + f.Name,
				mode: "must_clamp",
				text: singleFieldDoc(f.Name, belowMin),
				want: singleFieldDoc(f.Name, wantAdj),
			}
		case gUint:
			impLo, _ := impliedUintBounds(f.Bits)
			belowMin := new(big.Int).Sub(impLo, one).String()
			wantLo, _ := uintClampWant(f)
			wantBound, _ := new(big.Int).SetString(wantLo, 10)
			wantAdj := new(big.Int).Add(wantBound, one).String()
			return &boundCase{
				name: "sabotage_clamp_want_" + f.Name,
				mode: "must_clamp",
				text: singleFieldDoc(f.Name, belowMin),
				want: singleFieldDoc(f.Name, wantAdj),
			}
		}
	}
	return nil
}

var tableBoundsPassProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
)

func record(div *[]string, format string, args ...any) {
	*div = append(*div, fmt.Sprintf(format, args...))
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func checkAcceptedOrClampedRoundTrip(div *[]string, name string, pv *Root) {
	m, mreason := RootMeasureReason(pv)
	if m < 0 || mreason != nil || RootMeasure(pv) != m {
		record(div, "boundsmeasure %s m=%d reason=%v", name, m, mreason)
		return
	}
	wire := make([]byte, m)
	if RootSave(pv, wire) != m {
		record(div, "boundssave %s", name)
		return
	}
	var lr TableReport
	loaded, lok := probeLoad(wire, &lr)
	if !lok || loaded == nil || lr.Err() != nil {
		record(div, "boundsload %s err=%v report=%+v", name, lr.Err(), lr)
		return
	}
	jm := RootToJSONMeasure(pv)
	if jm <= 0 {
		record(div, "boundstojsonmeasure %s jm=%d", name, jm)
		return
	}
	tj := make([]byte, jm)
	if RootToJSON(pv, tj) != jm {
		record(div, "boundstojson %s", name)
		return
	}
	var r2 TableReport
	v2, ok2 := probeFromJSON(tj, &r2)
	if !ok2 || v2 == nil || r2.Err() != nil {
		record(div, "boundsroundtriprefuse %s err=%v %s", name, r2.Err(), b64(tj))
		return
	}
	tj2 := make([]byte, RootToJSONMeasure(v2))
	if RootToJSON(v2, tj2) != int64(len(tj2)) || !bytes.Equal(tj2, tj) {
		record(div, "boundsroundtripjson %s %s %s", name, b64(tj), b64(tj2))
	}
	w2 := make([]byte, RootMeasure(v2))
	if RootSave(v2, w2) != int64(len(w2)) || !bytes.Equal(w2, wire) {
		record(div, "boundsroundtripwire %s", name)
	}
}

func TestBoundsPassProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	raw, err := os.ReadFile(dir + "/bounds.txt")
	if err != nil {
		t.Fatal(err)
	}
	var div []string

	var defVal Root
	RootReset(&defVal)
	defJSON := make([]byte, RootToJSONMeasure(&defVal))
	RootToJSON(&defVal, defJSON)

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) != 4 {
			t.Fatalf("bad bounds line: %q", line)
		}
		name, mode, payloadB64, wantB64 := parts[0], parts[1], parts[2], parts[3]
		decoded, err := base64.StdEncoding.DecodeString(payloadB64)
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		var wantRaw []byte
		if wantB64 != "-" {
			wantRaw, err = base64.StdEncoding.DecodeString(wantB64)
			if err != nil {
				t.Fatalf("decode want %s: %v", name, err)
			}
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					record(&div, "boundspanic %s %v", name, r)
				}
			}()
			var pr TableReport
			pv, ok := probeFromJSON(decoded, &pr)

			switch mode {
			case "must_accept":
				if !ok || pv == nil || pr.Err() != nil {
					record(&div, "boundsrefused %s ok=%v err=%v report=%+v", name, ok, pr.Err(), pr)
					return
				}
				checkAcceptedOrClampedRoundTrip(&div, name, pv)
			case "must_unknown":
				if !ok || pv == nil || pr.Err() != nil || pr.Unknown <= 0 {
					record(&div, "boundsunknown %s ok=%v err=%v report=%+v", name, ok, pr.Err(), pr)
					return
				}
				checkAcceptedOrClampedRoundTrip(&div, name, pv)
			case "must_clamp":
				if ok || pr.Err() == nil || pr.Clamped <= 0 {
					record(&div, "boundsaccepted %s ok=%v err=%v report=%+v", name, ok, pr.Err(), pr)
					return
				}
				var rv Root
				RootReset(&rv)
				var rr TableReport
				probeFromJSONValue(&rv, decoded, &rr)
				// The strict read refused and reset to defaults, so the CLAMPED
				// value is observed through the tolerant read, which keeps the
				// recovered value (§6.5): it must sit exactly at the declared/
				// implied bound (a clamp keeps the value at the bound).
				if len(wantRaw) > 0 {
					var tr TableReport
					tv, _ := probeFromJSONTolerant(decoded, &tr)
					if tv == nil {
						tv = &defVal
					}
					gotJSON := make([]byte, RootToJSONMeasure(tv))
					RootToJSON(tv, gotJSON)
					var wantR TableReport
					wantV, wok := probeFromJSON(wantRaw, &wantR)
					if !wok || wantV == nil || wantR.Err() != nil {
						record(&div, "boundswantrefused %s err=%v", name, wantR.Err())
						return
					}
					wantJSON := make([]byte, RootToJSONMeasure(wantV))
					RootToJSON(wantV, wantJSON)
					if !bytes.Equal(gotJSON, wantJSON) {
						record(&div, "boundsclampwanted %s got=%s want=%s", name, b64(gotJSON), b64(wantJSON))
						return
					}
				}
				checkAcceptedOrClampedRoundTrip(&div, name, &rv)
			case "must_kind_mismatch":
				if ok || pr.Err() == nil || pr.KindMismatch <= 0 {
					record(&div, "boundsaccepted %s ok=%v err=%v report=%+v", name, ok, pr.Err(), pr)
					return
				}
				var rv Root
				RootReset(&rv)
				var rr TableReport
				probeFromJSONValue(&rv, decoded, &rr)
				// A kind mismatch leaves the field at its declared default.
				gotJSON := make([]byte, RootToJSONMeasure(&rv))
				RootToJSON(&rv, gotJSON)
				if !bytes.Equal(gotJSON, defJSON) {
					record(&div, "boundskindmismatchnotdefault %s got=%s want=%s", name, b64(gotJSON), b64(defJSON))
					return
				}
				checkAcceptedOrClampedRoundTrip(&div, name, &rv)
			case "must_refuse_named":
				if ok || pr.Err() == nil || pr.Reason == "" {
					record(&div, "boundsaccepted %s ok=%v err=%v report=%+v", name, ok, pr.Err(), pr)
					return
				}
			}
		}()
	}

	if len(div) > 0 {
		_ = os.WriteFile(dir+"/divergences.txt", []byte(strings.Join(div, "\n")+"\n"), 0o600)
		t.Fatalf("%d divergence(s)", len(div))
	}
}
`

func runBoundsSeed(t *testing.T, r *rand.Rand, seed int, sabotage bool) (unexpected []string, expected []string) {
	t.Helper()
	src, u := randomSchema(r)
	unit := tableUnit(t, src)
	dir := t.TempDir()
	cases := buildFieldBoundCases(u, sabotage)
	var lines []string
	for _, c := range cases {
		want := "-"
		if c.mode == "must_clamp" && c.want != "" {
			want = base64.StdEncoding.EncodeToString([]byte(c.want))
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s", c.name, c.mode, base64.StdEncoding.EncodeToString([]byte(c.text)), want))
	}
	if err := os.WriteFile(filepath.Join(dir, "bounds.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir}
	out, err := runGeneratedTableUnit(t, unit, tableBoundsPassProbe, env, "-race")
	divBytes, _ := os.ReadFile(filepath.Join(dir, "divergences.txt"))
	for line := range strings.SplitSeq(strings.TrimSpace(string(divBytes)), "\n") {
		if line == "" {
			continue
		}
		owned := false
		for _, c := range expectedClasses {
			if c.match(u, line) {
				expected = append(expected, fmt.Sprintf("seed %d: %s (%s): %s", seed, c.id, c.why, shorten(line)))
				owned = true
				break
			}
		}
		if !owned {
			unexpected = append(unexpected, fmt.Sprintf("seed %d: unexpected divergence: %s", seed, shorten(line)))
		}
	}
	if err != nil && len(divBytes) == 0 {
		unexpected = append(unexpected, fmt.Sprintf("seed %d: probe failed with no divergence report: %v\n%s", seed, err, out))
	}
	return unexpected, expected
}

// TestRandomTableBoundsPass generates random valid table units for seeds
// SCHEMA_TABLE_DIFF_FROM..TO (default 0..4) and, for each scalar field of Root,
// writes the {} value with that one field set to each of: min-1, max+1, the
// implied extremes plus one, 1e400, 2.5, "7" and null, reading each with
// RootFromJSON under recover and -race, plus one sabotaged negative-control run.
func TestRandomTableBoundsPass(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units)")
	from, to := 0, 4
	if v := os.Getenv("SCHEMA_TABLE_DIFF_FROM"); v != "" {
		from, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SCHEMA_TABLE_DIFF_TO"); v != "" {
		to, _ = strconv.Atoi(v)
	}
	for _, c := range expectedClasses {
		t.Logf("expected class owned by %s: %s", c.id, c.why)
	}
	for seed := from; seed <= to; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			unexpected, expected := runBoundsSeed(t, rand.New(rand.NewSource(int64(seed))), seed, false)
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			for _, e := range unexpected {
				t.Errorf("%s", e)
			}
		})
	}
	t.Run("negative_control", func(t *testing.T) {
		t.Parallel()
		unexpected, _ := runBoundsSeed(t, rand.New(rand.NewSource(0)), 0, true)
		if len(unexpected) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: an in-range value expected refused left the gate green")
		}
		if !strings.Contains(strings.Join(unexpected, "\n"), "unexpected divergence") {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on an unexpected divergence: %v", unexpected)
		}
		// The bound oracle's negative control must itself turn red: the
		// sabotaged clamp case expects the adjacent in-range value, so the
		// probe must report a boundsclampwanted divergence.
		if !strings.Contains(strings.Join(unexpected, "\n"), "boundsclampwanted") {
			t.Fatalf("NEGATIVE CONTROL FAILED: the bound oracle was not exercised: %v", unexpected)
		}
	})
}

var _ = math.MaxInt8

package benchgen

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Languages are the bench harness targets, in the order the legs are listed in
// bench/README.md.
func Languages() []string {
	return []string{"c", "cpp", "cs", "dart", "elixir", "go", "java", "js", "rust"}
}

// Emit renders the shape-dependent harness code for one language. Every
// benchmarked shape in the unit — a top-level struct with a wire golden of its
// own — gets the pieces that language's leg consumes:
//
//	pin    the golden's own instance, decoded and re-emitted (every language)
//	vary   the §2.2 LCG mapping over value fields (every language)
//	sink   the §2.7 full-struct fold (the legs with no free memory barrier)
//	check  the decoded-field comparison (the legs that gate variants by field)
//
// The timed-loop drivers, escape barriers, buffer discipline and CSV plumbing
// stay hand-written in bench/<lang> — none of it moves with the shape.
func Emit(u *ir.Unit, lang string, goldens map[string][]byte) (map[string][]byte, error) {
	shapes, err := Shapes(u, goldens)
	if err != nil {
		return nil, err
	}
	if len(shapes) == 0 {
		return nil, fmt.Errorf("no benchmarked shape in package %s: no wire golden matches a top-level type", u.Package)
	}
	r, ok := renderers[lang]
	if !ok {
		return nil, fmt.Errorf("unknown bench target %q (targets: %s)", lang, strings.Join(Languages(), ", "))
	}
	name, body := r(u, shapes)
	return map[string][]byte{name: body}, nil
}

// Shapes finds the benchmarked shapes of a unit and decodes each one's golden.
func Shapes(u *ir.Unit, goldens map[string][]byte) ([]*Shape, error) {
	var out []*Shape
	var names []string
	for name := range u.Structs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Structs[name]
		golden := ir.RustSnake(name)
		data, ok := goldens[golden]
		if !ok {
			continue
		}
		nodes, err := Decode(u, st, data)
		if err != nil {
			return nil, fmt.Errorf("%s vs testdata/wire/%s.bin: %w", name, golden, err)
		}
		Plan(nodes)
		out = append(out, &Shape{Struct: st, Golden: golden, Bytes: int64(len(data)), Nodes: nodes, Unit: u})
	}
	return out, nil
}

type renderFunc func(u *ir.Unit, shapes []*Shape) (name string, body []byte)

var renderers = map[string]renderFunc{}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// buf is the small emitter helper every renderer writes through.
type buf struct {
	b      strings.Builder
	indent int
}

func (w *buf) line(format string, args ...any) {
	for range w.indent {
		w.b.WriteString("    ")
	}
	fmt.Fprintf(&w.b, format, args...)
	w.b.WriteByte('\n')
}

func (w *buf) raw(s string) { w.b.WriteString(s) }
func (w *buf) nl()          { w.b.WriteByte('\n') }
func (w *buf) in()          { w.indent++ }
func (w *buf) out()         { w.indent-- }
func (w *buf) String() string {
	return w.b.String()
}

// hexMask renders a mask the way every leg spells one.
func hexMask(m uint64) string { return "0x" + strconv.FormatUint(m, 16) }

// offset renders a ranged form's minimum as the term that follows the draw.
func offset(min *big.Int) string {
	if min.Sign() < 0 {
		return " - " + new(big.Int).Neg(min).String()
	}
	return " + " + min.String()
}

// lo64 / hi64 split a 128 bit pinned value into the two qwords every language's
// 128 bit storage carries (low half first — the wire order).
func lo64(v *big.Int) uint64 {
	return new(big.Int).And(v, new(big.Int).SetUint64(^uint64(0))).Uint64()
}

func hi64(v *big.Int) uint64 {
	return new(big.Int).Rsh(v, 64).And(new(big.Int).Rsh(v, 64), new(big.Int).SetUint64(^uint64(0))).Uint64()
}

// wide128 normalizes a (possibly negative) 128 bit value into its unsigned
// two's complement image, which is what the lo/hi lanes carry.
func wide128(v *big.Int) *big.Int {
	if v.Sign() >= 0 {
		return v
	}
	mod := new(big.Int).Lsh(big.NewInt(1), 128)
	return new(big.Int).Add(mod, v)
}

// floatLit prints a float that reads back EXACTLY: the pinned value must
// re-encode to the golden's bits, so the shortest round-tripping form is the
// only correct one.
func floatLit(f float64, bits int) string {
	s := strconv.FormatFloat(f, 'g', -1, bits)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// shapeOrder sorts shapes the way the benchmarks run: the diagnostic rows
// first, the canonical shape last, matching the corpus file's own order.
func declOrder(u *ir.Unit, shapes []*Shape) []*Shape {
	pos := map[string]int{}
	i := 0
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				pos[st.Name] = i
				i++
			}
		}
	}
	out := append([]*Shape(nil), shapes...)
	sort.SliceStable(out, func(a, b int) bool { return pos[out[a].Struct.Name] < pos[out[b].Struct.Name] })
	return out
}

// countName / lengthName are the companion storage every backend emits beside a
// counted array, a string and a bytes buffer (SPEC §4.7).
func countName(f *ir.Field) string  { return f.Name + "_count" }
func lengthName(f *ir.Field) string { return f.Name + "_length" }

// varyShift renders the shift term: constant, or the element index riding it
// inside an array loop.
func varyShift(v *VarySpec, idxVar string) string {
	if v.PerElem && idxVar != "" {
		return fmt.Sprintf("((%d + %s) & 31)", v.Shift, idxVar)
	}
	return strconv.Itoa(v.Shift)
}

// header is the banner every emitted harness file carries.
func header(comment, u string, extra ...string) string {
	var w buf
	w.line("%s Code generated by `schema bench` — DO NOT EDIT.", comment)
	w.line("%s", comment)
	w.line("%s The shape-dependent half of the bench harness (issue #191): the pinned", comment)
	w.line("%s instance, the LCG vary mapping, and — where the leg has no free memory", comment)
	w.line("%s barrier — the §2.7 full-struct sink fold and the decoded-field check.", comment)
	w.line("%s", comment)
	w.line("%s The pin is the wire golden itself, decoded: testdata/wire/<shape>.bin is", comment)
	w.line("%s the §1.5 oracle every leg is gated against, so it is the ONE source of", comment)
	w.line("%s the pinned values and no leg transcribes them.", comment)
	w.line("%s", comment)
	w.line("%s The vary mapping is derived from each field's declared wire type: a", comment)
	w.line("%s masked draw of the largest power-of-two sub-range inside the field's", comment)
	w.line("%s bounds, offset by its minimum, from one LCG word (§2.2). Structure —", comment)
	w.line("%s array counts, string and bytes used lengths, the union tag, a branch", comment)
	w.line("%s gate — never varies, so bytes/op is constant (§2.7). Array elements vary", comment)
	w.line("%s at stride 2 through their value fields, the family's representative", comment)
	w.line("%s subset made mechanical.", comment)
	w.line("%s", comment)
	w.line("%s unit: %s", comment, u)
	for _, e := range extra {
		w.line("%s %s", comment, e)
	}
	return w.String()
}

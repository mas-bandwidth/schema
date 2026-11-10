// Package packet is the conformance harness's PACKET carrier: docs/SPEC.md
// §7.2 gates 4 and 5 over the type wire, beside the table corpus the rest of
// test/conformance carries.
//
// Per schema in the seed set (Seeds), the carrier
//
//  1. generates random instances of the root type from the IR, respecting
//     every range, arm and fixed bound, and states each one as CANONICAL JSON;
//  2. hands every participating language driver the same instances, and every
//     writer's bytes must be identical;
//  3. has every reader read every writer's bytes, and each must accept and
//     return the instance it was written from;
//  4. flips every bit of every reference packet, one at a time, and has every
//     reader read every mutant: the readers must agree on accept or refuse,
//     and on the decoded value of an accepted mutant (§5 leaves a refused
//     read's output unspecified, so nothing else is compared).
//
// A driver is a command that speaks the two verbs in driverContract over
// files; the drivers live under testdata/drivers/<lang>, one codec per seed
// root. The instances cross the driver boundary in their FLAT form — the
// integer leaves of the canonical value in declaration order, a union as its
// tag followed by the selected arm's leaves — so that no driver needs a JSON
// parser; the harness owns the mapping between the two forms and compares in
// canonical JSON.
//
// The carrier carries the constructs the seeds use: integers (ranged or
// full-width, up to 64 bits), fixed arrays of them, nested types, unions, and
// align. Any other construct is refused by name rather than generated wrong.
package packet

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Seed is one schema in the carrier's seed set: a directory holding one
// schema unit, and the root type the drivers read and write.
type Seed struct {
	Key  string // the drivers' name for the codec
	Dir  string // relative to this package
	Root string // the root type
}

// Seeds is the seed set: shapes outside examples/. A seed is added here, as a
// schema directory under testdata/, and as one codec in every driver.
var Seeds = []Seed{
	// A union field ahead of a fixed byte array: the union's width depends on
	// the arm it carries, so the array's start is not statically aligned.
	{Key: "union-before-bytes", Dir: "testdata/union-before-bytes", Root: "T"},
}

// The contract every driver answers:
//
//	<driver> write <codec> <in> <out>
//	    in:  one instance per line, its flat form (space-separated decimal integers)
//	    out: one line per instance, the written packet as lowercase hex, or "refused"
//	<driver> read <codec> <in> <out>
//	    in:  one packet per line, lowercase hex
//	    out: one line per packet, "ok" and the decoded flat form, or "refused"
//
// The working directory is the driver's own build directory. A read's verdict
// is the generated Read function's result and nothing else: bytes after the
// last field are the caller's concern (docs/SPEC.md §5, read termination).

// Rng is xorshift64*: deterministic everywhere, no library dependence, the
// same generator test/random_main.cpp uses.
type Rng struct{ state uint64 }

// NewRng seeds the generator; a zero seed is replaced, since xorshift's zero
// state is a fixed point.
func NewRng(seed uint64) *Rng {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	return &Rng{state: seed}
}

func (r *Rng) next() uint64 {
	r.state ^= r.state >> 12
	r.state ^= r.state << 25
	r.state ^= r.state >> 27
	return r.state * 0x2545F4914F6CDD1D
}

// below returns a uniform value in [0, n) for n > 0. Drawing 64 bits past
// n's width keeps the modulo bias negligible.
func (r *Rng) below(n *big.Int) *big.Int {
	v := new(big.Int)
	for i := 0; i < n.BitLen()/64+2; i++ {
		v.Lsh(v, 64)
		v.Or(v, new(big.Int).SetUint64(r.next()))
	}
	return v.Mod(v, n)
}

// Kind is a value node's shape.
type Kind int

const (
	KindInt Kind = iota
	KindArray
	KindStruct
	KindUnion
)

// Value is one node of an instance.
type Value struct {
	Kind    Kind
	Int     *big.Int // KindInt
	Elems   []*Value // KindArray
	Names   []string // KindStruct: field names, declared order
	Fields  []*Value // KindStruct
	Tag     int      // KindUnion: 0 is None, i is the i-th declared arm
	Arm     string   // KindUnion: the selected arm's name, "" for None
	Payload *Value   // KindUnion: the selected arm's payload, nil for None
}

// Shape is the carried projection of a root: what to generate, flatten and
// parse. It is built once per root so an unsupported construct is refused
// before any instance exists.
type Shape struct {
	kind   Kind
	min    *big.Int // KindInt: inclusive bounds
	max    *big.Int
	elem   *Shape   // KindArray
	count  int64    // KindArray
	names  []string // KindStruct
	fields []*Shape
	arms   []string // KindUnion, declared order
	armOf  []*Shape
}

// ShapeOf projects a root type onto the carried constructs.
func ShapeOf(root *ir.Struct) (*Shape, error) {
	return structShape(root, map[string]bool{})
}

func structShape(s *ir.Struct, open map[string]bool) (*Shape, error) {
	if open[s.Name] {
		return nil, fmt.Errorf("packet carrier: type %s is recursive", s.Name)
	}
	open[s.Name] = true
	defer delete(open, s.Name)
	out := &Shape{kind: KindStruct}
	for _, item := range s.Items {
		switch it := item.(type) {
		case *ir.AlignItem, *ir.ConstItem, *ir.ReservedItem:
			// no value: the wire's own, written and checked by the codec
		case *ir.FieldItem:
			fs, err := fieldShape(s, it.F, open)
			if err != nil {
				return nil, err
			}
			out.names = append(out.names, it.F.Name)
			out.fields = append(out.fields, fs)
		default:
			return nil, fmt.Errorf("packet carrier: %s carries a branch, which the carrier does not generate yet", s.Name)
		}
	}
	return out, nil
}

func fieldShape(owner *ir.Struct, f *ir.Field, open map[string]bool) (*Shape, error) {
	where := owner.Name + "." + f.Name
	if f.Type.Optional || f.Type.Pointer {
		return nil, fmt.Errorf("packet carrier: %s is optional or a pointer, which the carrier does not generate yet", where)
	}
	var elem *Shape
	switch f.Type.Kind {
	case ir.TInt:
		if f.Type.Width > 64 || f.HasFloatRange || f.Type.Timestamp {
			return nil, fmt.Errorf("packet carrier: %s is a %d-bit or special integer, which the carrier does not generate yet", where, f.Type.Width)
		}
		lo, hi := intBounds(f.Type.Signed, f.Type.Width)
		if f.HasIntRange {
			if f.RangeOneSided {
				return nil, fmt.Errorf("packet carrier: %s has a one-sided range, which the carrier does not generate yet", where)
			}
			lo, hi = new(big.Int).Set(f.IntMin), new(big.Int).Set(f.IntMax)
		}
		elem = &Shape{kind: KindInt, min: lo, max: hi}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			s, err := structShape(ref, open)
			if err != nil {
				return nil, err
			}
			elem = s
		case *ir.Union:
			u := &Shape{kind: KindUnion}
			for _, v := range ref.Variants {
				if v.Ref == nil {
					return nil, fmt.Errorf("packet carrier: %s arm %s names no type, which the carrier does not generate yet", where, v.Name)
				}
				s, err := structShape(v.Ref, open)
				if err != nil {
					return nil, err
				}
				u.arms = append(u.arms, v.Name)
				u.armOf = append(u.armOf, s)
			}
			elem = u
		default:
			return nil, fmt.Errorf("packet carrier: %s names an enum or flags, which the carrier does not generate yet", where)
		}
	default:
		return nil, fmt.Errorf("packet carrier: %s has a type kind the carrier does not generate yet", where)
	}
	switch f.Array {
	case ir.ArrayNone:
		return elem, nil
	case ir.ArrayFixed:
		return &Shape{kind: KindArray, elem: elem, count: f.ArrayBound}, nil
	}
	return nil, fmt.Errorf("packet carrier: %s is a counted or unbounded array, which the carrier does not generate yet", where)
}

func intBounds(signed bool, width int) (*big.Int, *big.Int) {
	if signed {
		hi := new(big.Int).Lsh(big.NewInt(1), uint(width-1))
		lo := new(big.Int).Neg(hi)
		return lo, hi.Sub(hi, big.NewInt(1))
	}
	hi := new(big.Int).Lsh(big.NewInt(1), uint(width))
	return big.NewInt(0), hi.Sub(hi, big.NewInt(1))
}

// Generate draws one instance. Integers favour their bounds a quarter of the
// time, because the edges are where a width or an offset goes wrong.
func (s *Shape) Generate(r *Rng) *Value {
	switch s.kind {
	case KindInt:
		switch r.next() % 8 {
		case 0:
			return &Value{Kind: KindInt, Int: new(big.Int).Set(s.min)}
		case 1:
			return &Value{Kind: KindInt, Int: new(big.Int).Set(s.max)}
		}
		span := new(big.Int).Sub(s.max, s.min)
		span.Add(span, big.NewInt(1))
		v := r.below(span)
		return &Value{Kind: KindInt, Int: v.Add(v, s.min)}
	case KindArray:
		out := &Value{Kind: KindArray}
		for i := int64(0); i < s.count; i++ {
			out.Elems = append(out.Elems, s.elem.Generate(r))
		}
		return out
	case KindStruct:
		out := &Value{Kind: KindStruct, Names: s.names}
		for _, f := range s.fields {
			out.Fields = append(out.Fields, f.Generate(r))
		}
		return out
	}
	tag := int(r.next() % uint64(len(s.arms)+1))
	out := &Value{Kind: KindUnion, Tag: tag}
	if tag > 0 {
		out.Arm = s.arms[tag-1]
		out.Payload = s.armOf[tag-1].Generate(r)
	}
	return out
}

// Flat is the driver-boundary form: the integer leaves in declaration order.
func (v *Value) Flat() string {
	var b strings.Builder
	v.flat(&b)
	return strings.TrimSpace(b.String())
}

func (v *Value) flat(b *strings.Builder) {
	switch v.Kind {
	case KindInt:
		b.WriteString(v.Int.String())
		b.WriteByte(' ')
	case KindArray:
		for _, e := range v.Elems {
			e.flat(b)
		}
	case KindStruct:
		for _, f := range v.Fields {
			f.flat(b)
		}
	case KindUnion:
		b.WriteString(strconv.Itoa(v.Tag))
		b.WriteByte(' ')
		if v.Payload != nil {
			v.Payload.flat(b)
		}
	}
}

// Parse reads a flat form back into a value of this shape. It checks the
// structure — the token count and each union tag — and not the ranges: a
// value outside its range is the READER's verdict to have refused, and is
// returned as read so the comparison can show it.
func (s *Shape) Parse(flat string) (*Value, error) {
	tokens := strings.Fields(flat)
	v, rest, err := s.parse(tokens)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("packet carrier: %d tokens left over after the value", len(rest))
	}
	return v, nil
}

func (s *Shape) parse(tokens []string) (*Value, []string, error) {
	switch s.kind {
	case KindInt:
		if len(tokens) == 0 {
			return nil, nil, fmt.Errorf("packet carrier: the flat form ends before an integer")
		}
		n, ok := new(big.Int).SetString(tokens[0], 10)
		if !ok {
			return nil, nil, fmt.Errorf("packet carrier: %q is not an integer", tokens[0])
		}
		return &Value{Kind: KindInt, Int: n}, tokens[1:], nil
	case KindArray:
		out := &Value{Kind: KindArray}
		for i := int64(0); i < s.count; i++ {
			e, rest, err := s.elem.parse(tokens)
			if err != nil {
				return nil, nil, err
			}
			out.Elems, tokens = append(out.Elems, e), rest
		}
		return out, tokens, nil
	case KindStruct:
		out := &Value{Kind: KindStruct, Names: s.names}
		for _, f := range s.fields {
			e, rest, err := f.parse(tokens)
			if err != nil {
				return nil, nil, err
			}
			out.Fields, tokens = append(out.Fields, e), rest
		}
		return out, tokens, nil
	}
	if len(tokens) == 0 {
		return nil, nil, fmt.Errorf("packet carrier: the flat form ends before a union tag")
	}
	tag, err := strconv.Atoi(tokens[0])
	if err != nil || tag < 0 || tag > len(s.arms) {
		return nil, nil, fmt.Errorf("packet carrier: %q is not a tag of a %d-arm union", tokens[0], len(s.arms))
	}
	out := &Value{Kind: KindUnion, Tag: tag}
	tokens = tokens[1:]
	if tag > 0 {
		out.Arm = s.arms[tag-1]
		arm, rest, err := s.armOf[tag-1].parse(tokens)
		if err != nil {
			return nil, nil, err
		}
		out.Payload, tokens = arm, rest
	}
	return out, tokens, nil
}

// JSON is the canonical JSON of a value: a type is an object of its fields in
// declared order, an array a list, an integer a number, and a union an object
// of its one selected arm — `{}` when it is None.
func (v *Value) JSON() string {
	var b strings.Builder
	v.json(&b)
	return b.String()
}

func (v *Value) json(b *strings.Builder) {
	switch v.Kind {
	case KindInt:
		b.WriteString(v.Int.String())
	case KindArray:
		b.WriteByte('[')
		for i, e := range v.Elems {
			if i > 0 {
				b.WriteByte(',')
			}
			e.json(b)
		}
		b.WriteByte(']')
	case KindStruct:
		b.WriteByte('{')
		for i, f := range v.Fields {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(v.Names[i]))
			b.WriteByte(':')
			f.json(b)
		}
		b.WriteByte('}')
	case KindUnion:
		b.WriteByte('{')
		if v.Payload != nil {
			b.WriteString(strconv.Quote(v.Arm))
			b.WriteByte(':')
			v.Payload.json(b)
		}
		b.WriteByte('}')
	}
}

// Instances draws n instances of a shape from one seed: the same seed is the
// same instances on every host, which is what makes a red reproducible.
func (s *Shape) Instances(seed uint64, n int) []*Value {
	r := NewRng(seed)
	out := make([]*Value, n)
	for i := range out {
		out[i] = s.Generate(r)
	}
	return out
}

// BitFlips returns every packet one bit away from each of the given packets,
// deduplicated and sorted, so the sweep is a function of the packets alone.
func BitFlips(packets [][]byte) [][]byte {
	seen := map[string]bool{}
	for _, p := range packets {
		for i := 0; i < len(p)*8; i++ {
			m := append([]byte(nil), p...)
			m[i/8] ^= 1 << (i % 8)
			seen[string(m)] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][]byte, len(keys))
	for i, k := range keys {
		out[i] = []byte(k)
	}
	return out
}

// Read is one reader's answer for one packet: refused, or the canonical JSON
// of what it accepted.
type Read struct {
	Refused bool
	JSON    string
}

func (r Read) String() string {
	if r.Refused {
		return "refused"
	}
	return "accepted " + r.JSON
}

// ParseReads turns a driver's read output into one Read per packet.
func (s *Shape) ParseReads(lines []string) ([]Read, error) {
	out := make([]Read, len(lines))
	for i, line := range lines {
		switch {
		case line == "refused":
			out[i] = Read{Refused: true}
		case line == "ok" || strings.HasPrefix(line, "ok "):
			v, err := s.Parse(strings.TrimPrefix(line, "ok"))
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			out[i] = Read{JSON: v.JSON()}
		default:
			return nil, fmt.Errorf("line %d: %q is neither \"refused\" nor \"ok ...\"", i+1, line)
		}
	}
	return out, nil
}

// CompareWrites requires every writer to have written the same bytes for each
// instance. writes maps a language to its hex lines, one per instance.
func CompareWrites(instances []*Value, writes map[string][]string) []string {
	langs := sortedKeys(writes)
	var bad []string
	for i, inst := range instances {
		ref := writes[langs[0]][i]
		agree := true
		for _, l := range langs[1:] {
			if writes[l][i] != ref {
				agree = false
			}
		}
		if !agree {
			var parts []string
			for _, l := range langs {
				parts = append(parts, l+"="+writes[l][i])
			}
			bad = append(bad, fmt.Sprintf("instance %d %s: writers disagree: %s", i, inst.JSON(), strings.Join(parts, " ")))
		}
	}
	return bad
}

// CompareRoundTrips requires every reader to accept every writer's packet for
// each instance and return that instance. reads maps "reader<-writer" to the
// reader's answers over the writer's packets.
func CompareRoundTrips(instances []*Value, reads map[string][]Read) []string {
	var bad []string
	for _, pair := range sortedKeys(reads) {
		for i, inst := range instances {
			got := reads[pair][i]
			if got.Refused || got.JSON != inst.JSON() {
				bad = append(bad, fmt.Sprintf("instance %d %s: %s read back %s", i, inst.JSON(), pair, got))
			}
		}
	}
	return bad
}

// CompareSweep requires every reader to give the same answer for each mutant:
// the same verdict and, on acceptance, the same value. reads maps a language
// to its answers over mutants.
func CompareSweep(mutants [][]byte, reads map[string][]Read) []string {
	langs := sortedKeys(reads)
	var bad []string
	for i, m := range mutants {
		ref := reads[langs[0]][i]
		agree := true
		for _, l := range langs[1:] {
			if reads[l][i] != ref {
				agree = false
			}
		}
		if !agree {
			var parts []string
			for _, l := range langs {
				parts = append(parts, l+": "+reads[l][i].String())
			}
			bad = append(bad, fmt.Sprintf("mutant %x: readers disagree: %s", m, strings.Join(parts, "; ")))
		}
	}
	return bad
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

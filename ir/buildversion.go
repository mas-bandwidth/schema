// The BUILD VERSION (SPEC-TABLES.md §20, schema#297): ONE digest over every
// fact the bytes a build produces depend on — the type wire, every table's
// layout, every table's meaning, and the build's byte order.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else (SPEC.md §3), and the BUILD
// VERSION is what everything cooked or blocked is keyed by. A table edit moves
// the build version and never the protocol id; a type edit moves both.
//
// The BLOCK FORM (§19) stamps this constant into every block's prologue and
// BlockOpen compares it against its own — the block form is same-build by
// construction, both sides generated from one declaration at one build, so one
// number answers "would this binary's blocks differ?" for the whole unit and
// there is no per-table layout digest to keep beside it.
package ir

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// buildVersionToken and cookIdToken are FORM VERSIONS: bump one when its fold
// changes (SPEC-TABLES.md §20.3).
const (
	buildVersionToken = "schema-build-version 1"
	cookIdToken       = "schema-cook-id 1"
)

// byteOrderLittle is the build's byte-order byte (§20.3). The compiler emits
// the constant for a little-endian build; a block written by a big-endian one
// is refused by the MAGIC, which is read bytewise and establishes the order
// before any version is compared (§19.1), so a block never has to ask the
// constant which order produced it.
const byteOrderLittle = 0x01

// BuildVersion is the unit's build version: an fnv1a64 fold over the token,
// the protocol id, and every table's COOK ID with the tables sorted by name
// (SPEC-TABLES.md §20.3).
func BuildVersion(u *Unit) uint64 {
	var b []byte
	b = append(b, buildVersionToken...)
	b = appendBE64(b, u.ProtocolId)
	for _, name := range sortedTableNames(u) {
		b = appendBE64(b, CookId(u, name))
	}
	return fnv1a64Bytes(b)
}

// CookId is one table's cook id: the per-TABLE key half (SPEC-TABLES.md
// §20.3), folded over the token, the unit's protocol id, that table's meaning
// digest, its name, its layout digest and the build's byte order.
func CookId(u *Unit, table string) uint64 {
	var b []byte
	b = append(b, cookIdToken...)
	b = appendBE64(b, u.ProtocolId)
	b = appendBE64(b, MeaningDigest(u, table))
	b = append(b, table...)
	b = append(b, 0x00)
	b = appendBE64(b, LayoutDigest(u, table))
	b = append(b, byteOrderLittle)
	return fnv1a64Bytes(b)
}

// ---- group 2: the layout (SPEC-TABLES.md §20.1) ----

// LayoutProjection is the text one table's LAYOUT digest is taken over: the
// table and every record its closure reaches, each with its size and
// alignment, and each field keyed by its WIRE ID rather than by its source
// name — so a `was` rename moves no line, exactly as it moves no byte.
//
// One line per fact, ASCII, sorted byte-wise and concatenated, as the meaning
// projection is (§20.2) and for the same reason: what an id depends on should
// be printable, readable and diffable.
func LayoutProjection(u *Unit, table string) string {
	var lines []string
	seen := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		st := memberStruct(u, name)
		if st == nil {
			return
		}
		ml := layoutRecord(u, st)
		lines = append(lines, fmt.Sprintf("record %s %d %d", name, ml.Size, ml.Align))
		for _, fl := range ml.Fields {
			f := fl.Field
			lines = append(lines, fmt.Sprintf("field %s.%04x %d %d %d",
				name, TableFieldId(f), TableScalarKind(f), fl.Offset, fl.Size))
		}
		for _, f := range st.Fields {
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Struct:
				walk(ref.Name)
			case *Union:
				for _, v := range ref.Variants {
					walk(v.Type)
				}
			}
		}
	}
	walk(table)
	sort.Strings(lines)
	return joinLines(lines)
}

// LayoutDigest is the low 64 bits of SHA-256 over [LayoutProjection].
func LayoutDigest(u *Unit, table string) uint64 { return sha256Low64(LayoutProjection(u, table)) }

// ---- group 3: the meaning (SPEC-TABLES.md §20.2) ----

// MeaningProjection is the text one table's MEANING digest is taken over: what
// a wire load PUTS in the slots, over the table's transitive closure — every
// specified default, every effective declared range, every compressed float's
// resolution, and every enum's and union's ordinal-to-name mapping.
//
// A field is named by its declaring record's name, a `.`, and its WIRE ID as
// four lowercase hex digits — never by its source name (§20.2).
func MeaningProjection(u *Unit, table string) string {
	var lines []string
	seen := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		st := memberStruct(u, name)
		if st == nil {
			return
		}
		for _, f := range st.Fields {
			id := TableFieldId(f)
			if f.HasDefault {
				lines = append(lines, fmt.Sprintf("default %s.%04x %s", name, id, meaningValue(f)))
			}
			if f.HasIntRange {
				lines = append(lines, fmt.Sprintf("range %s.%04x %s %s", name, id, f.IntMin, f.IntMax))
			} else if f.HasFloatRange {
				lines = append(lines, fmt.Sprintf("range %s.%04x %s %s", name, id,
					canonicalFloat(f.FMin), canonicalFloat(f.FMax)))
				lines = append(lines, fmt.Sprintf("step %s.%04x %s", name, id, canonicalFloat(f.Resolution)))
			} else if f.Type.Kind == TBits {
				// bits(N) declares [0, 2^N - 1] by its WIDTH, and §4 clamps a
				// read to it, so the implied range is a meaning fact like any
				// declared one (§8, §20.1).
				lines = append(lines, fmt.Sprintf("range %s.%04x 0 %d", name, id, (uint64(1)<<uint(f.Type.Width))-1))
			}
			// the vocabularies a slot stores an ORDINAL of
			if f.KeyEnumRef != nil {
				walkEnum(&lines, seen, f.KeyEnumRef)
			}
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Enum:
				walkEnum(&lines, seen, ref)
			case *Struct:
				walk(ref.Name)
			case *Union:
				if !seen["union "+ref.Name] {
					seen["union "+ref.Name] = true
					for i, v := range ref.Variants {
						lines = append(lines, fmt.Sprintf("union %s %d %s", ref.Name, i, v.Name))
					}
				}
				for _, v := range ref.Variants {
					walk(v.Type)
				}
			}
		}
	}
	walk(table)
	sort.Strings(lines)
	return joinLines(lines)
}

// MeaningDigest is the low 64 bits of SHA-256 over [MeaningProjection].
func MeaningDigest(u *Unit, table string) uint64 { return sha256Low64(MeaningProjection(u, table)) }

func walkEnum(lines *[]string, seen map[string]bool, e *Enum) {
	if seen["enum "+e.Name] {
		return
	}
	seen["enum "+e.Name] = true
	for i, v := range e.Variants {
		*lines = append(*lines, fmt.Sprintf("enum %s %d %s", e.Name, i, v))
	}
}

// meaningValue renders a field's specified default as schemafmt-canonical text
// of the EVALUATED value — what a constant now produces, never how it was
// spelled (§20.2).
func meaningValue(f *Field) string {
	switch {
	case f.DefVariant != "":
		return f.DefVariant
	case f.Type.Kind == TBool:
		if f.DefBool {
			return "true"
		}
		return "false"
	case f.DefInt != nil:
		return f.DefInt.String()
	default:
		return canonicalFloat(f.DefFloat)
	}
}

func canonicalFloat(v float64) string {
	s := fmt.Sprintf("%g", v)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// ---- the instruments (SPEC-TABLES.md §20.3) ----

func joinLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// sha256Low64 takes the low 64 bits of SHA-256 — the final eight bytes,
// big-endian, exactly as the protocol id is taken (SPEC.md §3.1). The
// projections are TEXT under SHA-256 because they are many small facts a walk
// could forget, so what they depend on must be printable and a missing fact
// must be a review question.
func sha256Low64(text string) uint64 {
	sum := sha256.Sum256([]byte(text))
	return binary.BigEndian.Uint64(sum[24:])
}

// fnv1a64Bytes is the FOLD: its inputs are already digests, and it must be
// computable as a constant expression in every backend's own language with no
// library (§20.3).
func fnv1a64Bytes(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, c := range b {
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}

func appendBE64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

func sortedTableNames(u *Unit) []string {
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

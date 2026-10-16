// THE WIDE SCALARS AND OPTIONAL FIELDS ON THE VARIABLE FORM
// (docs/SPEC-TABLES.md §2.3, §3, §15), form byte 1, in Dart.
//
// A `fixed(I, F)` / `ufixed(I, F)` field rides under kinds 20–29 as its RAW
// scaled integer at the storage width, and an `int128` / `uint128` under kinds
// 18 / 19 as sixteen bytes, two's complement, little-endian, the low half first
// (§3, internal/tablewire encodeElement). Dart's int is 64 bits, so every wide
// value is held as a BigInt RAW integer — the same raw value the Go oracle's
// cell holds — and a fixed default is the raw value ir.Field.DefInt carries.
//
// An OPTIONAL field `?T` (§2.3) keeps its value in the ordinary member and its
// PRESENCE in a generated `has<Member>` bool beside it. Presence is the
// payload: a present optional ALWAYS rides, even at its default — a counted
// optional array with its live count, zero included, and a fixed one with
// every declared element — and an absent one never does (internal/tablewire
// encodeField, the `f.Type.Optional` arm).
package darttable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// isWide reports whether a field (or one element of an array field) rides
// under one of the wide kinds 18–29 (docs/SPEC-TABLES.md §3, §15).
func isWide(f *ir.Field) bool {
	if f.Type.Kind != ir.TFixed && !(f.Type.Kind == ir.TInt && f.Type.Width == 128) {
		return false
	}
	return ir.TableKindWide(ir.TableScalarKind(singleOf(f)))
}

// isFlags reports whether a field names a `flags` set (§3): its mask rides as
// the unsigned integer of the set's storage width.
func isFlags(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}

// wideKind is the wide field's own kind byte (§3).
func wideKind(f *ir.Field) int { return ir.TableScalarKind(singleOf(f)) }

// bigLiteral spells a raw integer as a Dart BigInt expression.
func bigLiteral(n *big.Int) string {
	if n == nil || n.Sign() == 0 {
		return "BigInt.zero"
	}
	return fmt.Sprintf("BigInt.parse('%s')", n.String())
}

// wideDefault is the raw default of a wide field (§5): DefInt is the RAW value
// for a fixed field and the integer itself for a 128-bit one.
func (g *wireGen) wideDefault(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		return bigLiteral(f.DefInt)
	}
	return "BigInt.zero"
}

// emitWideRuntime emits the two wide-kind codecs every wide field, element and
// arm shares (§3): the raw integer at the kind's width, little-endian two's
// complement, and its inverse, sign-extended for a signed kind.
func (g *wireGen) emitWideRuntime() {
	g.pf(`// _putWide writes a raw wide value (kinds 18–29) at its width, two's
// complement, little-endian (docs/SPEC-TABLES.md §3).
void _putWide(TableWireWriter w, BigInt v, int width) {
  var x = v.toUnsigned(width * 8);
  final mask = BigInt.from(0xff);
  for (var i = 0; i < width; i++) {
    w.putByte((x & mask).toInt());
    x = x >> 8;
  }
}

// _getWide reads a raw wide value at its width, sign-extended for a signed
// kind (docs/SPEC-TABLES.md §3). The caller has checked the bytes are there.
BigInt _getWide(TableWireReader r, int width, bool signed) {
  var x = BigInt.zero;
  for (var i = width - 1; i >= 0; i--) {
    x = (x << 8) | BigInt.from(r.data[r.pos + i]);
  }
  r.pos += width;
  return signed ? x.toSigned(width * 8) : x;
}

`)
}

// emitWideClamp clamps a raw wide value to the field's declared RAW range
// (ir.TableRawRange, internal/tabletext WideClamp), counting each clamp (§4).
func (g *wireGen) emitWideClamp(f *ir.Field, value, p, ind string) {
	lo, hi, ok := ir.TableRawRange(singleOf(f))
	if !ok {
		return
	}
	g.pf("%sif (%s < %s) { %s = %s; %s.report.clamped++; }\n", ind, value, bigLiteral(lo), value, bigLiteral(lo), p)
	g.pf("%sif (%s > %s) { %s = %s; %s.report.clamped++; }\n", ind, value, bigLiteral(hi), value, bigLiteral(hi), p)
}

// emitSaveWide writes a wide field (§3): elided at its default, otherwise the
// header and the raw value at the kind's width.
func (g *wireGen) emitSaveWide(f *ir.Field, expr, ind string, id uint64) {
	kind := wideKind(f)
	g.pf("%sif (%s != %s) {\n", ind, expr, g.wideDefault(f))
	g.pf("%s\tw.putLeb(w.refFor(%s));\n", ind, dartU64Literal(id))
	g.pf("%s\tw.putByte(%d);\n", ind, kind)
	g.pf("%s\t_putWide(w, %s, %d);\n", ind, expr, ir.TableKindWidth(kind))
	g.pf("%s}\n", ind)
}

// emitLoadWide is the tolerant read of a wide field (§4): its own kind reads
// at its width; a 128-bit field also WIDENS every smaller integer kind of its
// own signedness (ir.TableKindWidens); any other kind is a mismatch; the value
// clamps to the declared raw range.
func (g *wireGen) emitLoadWide(f *ir.Field, ind string) {
	name := fieldMember(f)
	decl := wideKind(f)
	signed := ir.TableKindSigned(decl)
	g.pf("%s\t{\n", ind)
	g.pf("%s\t\tBigInt? value;\n", ind)
	g.pf("%s\t\tswitch (kind) {\n", ind)
	g.pf("%s\t\t\tcase %d:\n", ind, decl)
	g.pf("%s\t\t\t\tif (!r.has(%d)) { r.report.malformed = true; return; }\n", ind, ir.TableKindWidth(decl))
	g.pf("%s\t\t\t\tvalue = _getWide(r, %d, %v);\n", ind, ir.TableKindWidth(decl), signed)
	g.pf("%s\t\t\t\tbreak;\n", ind)
	for k := 1; k < 34; k++ {
		if !ir.TableKindWidens(k, decl) {
			continue
		}
		w := ir.TableKindWidth(k)
		g.pf("%s\t\t\tcase %d:\n", ind, k)
		g.pf("%s\t\t\t\tif (!r.has(%d)) { r.report.malformed = true; return; }\n", ind, w)
		g.pf("%s\t\t\t\tvalue = _getWide(r, %d, %v);\n", ind, w, signed)
		g.pf("%s\t\t\t\tr.report.widened++;\n", ind)
		g.pf("%s\t\t\t\tbreak;\n", ind)
	}
	g.pf("%s\t\t\tdefault:\n", ind)
	g.pf("%s\t\t\t\tr.report.kindMismatch++;\n", ind)
	g.pf("%s\t\t\t\tif (!r.skipKind(kind)) { r.report.malformed = true; return; }\n", ind)
	g.pf("%s\t\t\t\tbreak;\n", ind)
	g.pf("%s\t\t}\n", ind)
	g.pf("%s\t\tif (value != null) {\n", ind)
	g.pf("%s\t\t\tvar x = value;\n", ind)
	g.emitWideClamp(f, "x", "r", ind+"\t\t\t")
	g.pf("%s\t\t\tv.%s = x;\n", ind, name)
	g.pf("%s\t\t}\n", ind)
	g.pf("%s\t}\n", ind)
	g.pf("%s\tbreak;\n", ind)
}

// nonOptional is an optional field's VALUE as a field of its own (§2.3): the
// same type, shape, defaults and bounds, no presence.
func nonOptional(f *ir.Field) *ir.Field {
	s := *f
	s.Type.Optional = false
	return &s
}

// hasMember is the generated presence member of an optional field (§2.3).
func hasMember(f *ir.Field) string { return "has" + capitalize(fieldMember(f)) }

// emitSaveOptional writes an optional field (docs/SPEC-TABLES.md §2.3,
// internal/tablewire encodeField): nothing when absent; when present it ALWAYS
// rides — an array with its whole framing (the live count for the counted
// spelling, zero included; every declared element for the fixed one), a table
// or type with its body even when that body is its terminator alone, and a
// scalar, string or enum with its payload even at its default. The field's id
// is interned before anything its payload interns (first-use order, §3).
func (g *wireGen) emitSaveOptional(f *ir.Field, expr, ind string, id uint64) {
	base := nonOptional(f)
	g.pf("%sif (v.%s) {\n", ind, hasMember(f))
	in := ind + "\t"
	g.pf("%sfinal ref = w.refFor(%s);\n", in, dartU64Literal(id))
	switch {
	case base.Array != ir.ArrayNone:
		ek := arrayElemKind(base)
		g.pf("%sfinal s = w.sub();\n", in)
		g.pf("%ss.putByte(%d);\n", in, ek)
		g.pf("%ss.putLeb(%s.length);\n", in, expr)
		g.pf("%sfor (final e in %s) {\n", in, expr)
		g.emitPutElem(base, ek, "e", "s", in+"\t")
		g.pf("%s}\n", in)
		g.pf("%sw.putLeb(ref);\n", in)
		g.pf("%sw.putByte(%d);\n", in, wireKindArray)
		g.pf("%sw.putLeb(s.buf.length);\n", in)
		g.pf("%sw.putBytes(s.buf);\n", in)
	case isByValueTable(base):
		g.pf("%sfinal b = w.sub();\n", in)
		g.pf("%s%sWireSaveBody(b, %s);\n", in, dartName(tableRef(base).Name), expr)
		g.pf("%sw.putLeb(ref);\n", in)
		g.pf("%sw.putByte(%d);\n", in, wireKindTable)
		g.pf("%sw.putLeb(b.buf.length);\n", in)
		g.pf("%sw.putBytes(b.buf);\n", in)
	case base.Type.Kind == ir.TString:
		g.pf("%sfinal b = utf8.encode(%s);\n", in, expr)
		g.pf("%sw.putLeb(ref);\n", in)
		g.pf("%sw.putByte(%d);\n", in, wireKindString)
		g.pf("%sw.putLeb(b.length);\n", in)
		g.pf("%sw.putBytes(b);\n", in)
	case enumRef(base) != nil:
		g.pf("%sw.putLeb(ref);\n", in)
		g.pf("%sw.putByte(%d);\n", in, wireKindEnum)
		g.emitPutPayload(base, expr, "w", in)
	default:
		kind := ir.TableWireScalarKind(base)
		g.pf("%sw.putLeb(ref);\n", in)
		g.pf("%sw.putByte(%d);\n", in, kind)
		g.emitPutElem(base, kind, expr, "w", in)
	}
	g.pf("%s}\n", ind)
}

// emitWStringPayload writes wide text as an arm payload (§3, kind 33): the
// UTF-16 code units, little-endian, with no length of their own — the arm's L
// is the byte length.
func (g *wireGen) emitWStringPayload(expr, p, ind string) {
	g.pf("%sfor (final u in %s.codeUnits) { %s.putU16(u); }\n", ind, expr, p)
}

// emitWStringGetPayload reads wide text out of an arm payload bounded to L
// (§3, kind 33): an odd L or ill-formed UTF-16 is damage inside the arm, and a
// value past the bound clamps at a code-point boundary.
func (g *wireGen) emitWStringGetPayload(f *ir.Field, dest, p, ind string) {
	bad := fmt.Sprintf("{ %s.report.malformed = true; return false; }", p)
	g.pf("%sfinal len = %s.limit - %s.pos;\n", ind, p, p)
	g.pf("%sif (len.isOdd) %s\n", ind, bad)
	g.pf("%sfinal units = <int>[];\n", ind)
	g.pf("%sfor (var i = 0; i < len; i += 2) { units.add(%s.data[%s.pos + i] | (%s.data[%s.pos + i + 1] << 8)); }\n", ind, p, p, p, p)
	g.pf("%s%s.pos = %s.limit;\n", ind, p, p)
	g.pf("%sif (!_wideValid(units)) %s\n", ind, bad)
	g.pf("%svar keep = units.length;\n", ind)
	g.pf("%sif (keep > %d) {\n", ind, f.Type.Size)
	g.pf("%s\tkeep = %d;\n", ind, f.Type.Size)
	g.pf("%s\tif (keep > 0 && units[keep - 1] >= 0xD800 && units[keep - 1] <= 0xDBFF) keep--;\n", ind)
	g.pf("%s\t%s.report.clamped++;\n", ind, p)
	g.pf("%s}\n", ind)
	g.pf("%s%s = String.fromCharCodes(units.sublist(0, keep));\n", ind, dest)
	g.pf("%sreturn true;\n", ind)
}

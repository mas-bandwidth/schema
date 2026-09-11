package darttable

// THE FIXED FORM'S LIBRARIES: <Base>Fixed.dart per unit file that declares a
// fixed root, and the unit's ONE RUNTIME HOME, <Home>Fixed.dart, where the
// shared runtime, every table's storage class and every type's write/decode
// helper land — the same home rule the block and cook halves already follow,
// for the same reason: a Dart library is a FILE, so a shared runtime is
// DEFINED once and IMPORTED everywhere else, and the library that defines it
// wants a name that does not move with file order.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateFixedFiles emits the fixed form for every unit file that declares a
// root the form is carried for, plus the unit's runtime home. A unit with no
// fixed-form root at all gets nothing: a library holding only a refusal nobody
// imports is a file nobody wants.
func generateFixedFiles(u *ir.Unit, wide []string, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	out := map[string][]byte{}
	roots := map[string][]*ir.Struct{}
	any := false
	for _, f := range u.Files {
		rs := fixedRoots(f.Tables)
		roots[f.Base] = rs
		if len(rs) > 0 {
			any = true
		}
	}
	if !any {
		return out, nil
	}
	home := runtimeHome(u)

	// THE CLOSURE OF EVERY ROOT OF THE UNIT, ordered so a nested type's
	// helpers are emitted before the type that uses them. They land in the
	// home and nowhere else: a nested type is often declared in a file of
	// `type`s alone, which gets no fixed surface of its own, and a consumer
	// would then import a helper nothing emitted.
	seen := map[string]bool{}
	var closure []*ir.Struct
	for _, f := range u.Files {
		for _, st := range roots[f.Base] {
			fixedCollectTypes(st, seen, &closure)
		}
	}
	// the TABLES of that closure are the ones whose storage class is this
	// emitter's own; a `type`'s class belongs to internal/codegen/dart
	mine := map[string]bool{}
	for _, st := range closure {
		if st.IsTable {
			mine[st.Name] = true
		}
	}

	homeWritten := false
	for _, f := range u.Files {
		if len(roots[f.Base]) == 0 && f.Base != home {
			continue
		}
		g := newFixedModule(u, f, f.Base, f.Base == home, home, wide, mine, lineage)
		g.emit(closure, roots[f.Base], f.Tables)
		if g.home {
			homeWritten = true
		}
		out[f.Base+"Fixed.dart"] = g.assemble()
	}
	if !homeWritten {
		g := newFixedModule(u, nil, home, true, home, wide, mine, lineage)
		g.emit(closure, nil, nil)
		out[home+"Fixed.dart"] = g.assemble()
	}
	return out, nil
}

type fixedModule struct {
	unit     *ir.Unit
	file     *ir.File
	base     string
	home     bool
	homeBase string
	wide     []string
	mine     map[string]bool                // the classes this emitter writes itself
	lineage  map[string][]FixedLineageEntry // the locked layouts, oldest first (§5.2)
	imports  map[string]map[string]bool
	gen      fixedGen
	body     strings.Builder
}

func newFixedModule(u *ir.Unit, f *ir.File, base string, home bool, homeBase string,
	wide []string, mine map[string]bool, lineage map[string][]FixedLineageEntry) *fixedModule {
	g := &fixedModule{unit: u, file: f, base: base, home: home, homeBase: homeBase,
		wide: wide, mine: mine, lineage: lineage, imports: map[string]map[string]bool{}}
	g.gen.unit = u
	g.gen.need = g.needClass
	return g
}

func (g *fixedModule) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// emitFn writes a function signature the way `dart format` would: one line
// when it fits the eighty-column bound, and the tall form with a trailing
// comma when it does not. The formatter is this leg's gate (tables-dart-clean),
// so the emitter measures rather than guesses.
func (g *fixedModule) emitFn(ret, name string, params []string) {
	one := fmt.Sprintf("%s %s(%s) {", ret, name, strings.Join(params, ", "))
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s %s(\n", ret, name)
	for _, p := range params {
		g.pf("  %s,\n", p)
	}
	g.pf(") {\n")
}

// call writes one call statement the way the Dart formatter would: one line
// when it fits the eighty-column bound, and the tall form with a trailing
// comma when it does not.
func (g *fixedModule) call(ind, lead, fn string, args []string, tail string) {
	one := ind + lead + fn + "(" + strings.Join(args, ", ") + ")" + tail
	if len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s%s%s(\n", ind, lead, fn)
	for _, a := range args {
		g.pf("%s  %s,\n", ind, a)
	}
	g.pf("%s)%s\n", ind, tail)
}

// THE BYTE TABLES ARE FORMATTER-EXEMPT, and the directive says so in the file.
// `dart format` puts one element per line in a list with a trailing comma, so
// a thousand-byte layout would be a thousand lines and no reader could diff it
// against the C++ reference's own hex table. The rows below are twelve bytes
// wide, exactly as the reference emits them.
const fixedFormatOff = "// dart format off\n"
const fixedFormatOn = "// dart format on\n"

func (g *fixedModule) need(base string, symbols ...string) {
	if base == "" || base == g.base+"Fixed" {
		return
	}
	set := g.imports[base]
	if set == nil {
		set = map[string]bool{}
		g.imports[base] = set
	}
	for _, s := range symbols {
		set[s] = true
	}
}

func (g *fixedModule) needHome(symbols ...string) { g.need(g.homeBase+"Fixed", symbols...) }

// needClass is what the generator calls for a class or namespace another
// emitter owns: a `type`'s class, an enum's constant namespace, a union's
// class, or the packet emitter's own Int128/UInt128 pair.
func (g *fixedModule) needClass(name string, extra ...string) {
	if name == "" || g.mine[name] {
		return // a table of this unit's closure, emitted right here
	}
	if name == "Int128" || name == "UInt128" {
		g.need("Int128", name)
		return
	}
	if base := fixedDeclaringFile(g.unit, name); base != "" {
		g.need(base, append([]string{name}, extra...)...)
	}
}

// fixedDeclaringFile answers the library basename that declares a named type,
// enum, union or flags, so its class can be imported rather than emitted
// twice.
func fixedDeclaringFile(u *ir.Unit, name string) string {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch r := d.(type) {
			case *ir.Struct:
				if r.Name == name {
					return f.Base
				}
			case *ir.Union:
				if r.Name == name {
					return f.Base
				}
			case *ir.Enum:
				if r.Name == name {
					return f.Base
				}
			case *ir.Flags:
				if r.Name == name {
					return f.Base
				}
			}
		}
	}
	return ""
}

func (g *fixedModule) emit(closure, roots, tables []*ir.Struct) {
	if g.home {
		g.pf("%s\n", fixedRuntime)
		// A TABLE gets its storage class here; a `type` already has one, from
		// internal/codegen/dart, and is imported rather than emitted twice.
		for _, st := range closure {
			if st.IsTable {
				g.gen.emitStorageClass(st)
			} else {
				g.gen.need(st.Name)
			}
		}
		for _, st := range closure {
			g.gen.emitWriteBody(st)
			g.gen.emitDecodeBody(st)
		}
		g.pf("%s", g.gen.body.String())
	}
	for _, st := range tables {
		if !st.IsTable || st.IsMapEntry() {
			continue
		}
		if reason := fixedRefusal(st); reason != "" {
			// NAMED, NEVER SILENT: a table with no fixed form says which and why.
			g.pf("// table %s has NO FIXED FORM in Dart: %s.\n", st.Name, reason)
			g.pf("// Nothing about form 1 moves, for this table or any other (§3.4).\n\n")
		}
	}
	for _, st := range roots {
		g.emitRoot(st)
	}
}

// emitRoot is one table's whole fixed-form surface.
func (g *fixedModule) emitRoot(st *ir.Struct) {
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := fixedTypeBytes(st)
	prefill := fixedPrefillBytes(st)
	plan := fixedIdentityPlan(st)
	lower := lowerFirst(st.Name)

	// exactly the names the surface below spells, and no more: the analyzer
	// refuses an unused shown name, and `dart analyze` is this leg's gate
	g.needHome("tableFixedForm", "tableFixedRun", "tableFixedFillRun",
		"tableFixedSelect", "tableFixedLineagePlans", "TableFixedKnownLayout",
		"TableFixedLineagePlan", "TableFixedLimits", "TableFixedPlan",
		"TableFixedRefusal", "TableFixedReport")
	g.needHome(st.Name, lower+"FixedWriteBody", lower+"FixedDecode")

	g.pf("// ---- %s, THE FIXED FORM (docs/SPEC-TABLES.md §3.4) ----\n//\n", st.Name)
	g.pf("// A record is an eight-byte hash of the writer's LAYOUT and then the values\n")
	g.pf("// in declared order, every field at its declared storage width, little-\n")
	g.pf("// endian, nothing padded between fields.\n\n")

	g.pf("/// MeasureBody IS A CONSTANT on this form: the body is the same size for\n")
	g.pf("/// every value the type can hold.\n")
	g.pf("const int %sFixedBodyBytes = %d;\n", lower, body)
	g.pf("const int %sFixedRecordBytes = %d; // the hash and the body\n\n", lower, fixedHashBytes+body)

	g.pf("/// fnv1a64 over the layout's bytes exactly as written. A Dart int is a full\n")
	g.pf("/// 64-bit two's complement value, so the hash rides as itself.\n")
	g.pf("const int %sFixedHash = %s;\n\n", lower, fixedDartHex(hash))

	g.pf("/// THE LAYOUT: %d entries, a PRE-ORDER walk of the closure in the writer's\n", len(w.entries))
	g.pf("/// declared order. Every byte is settled by the compiler.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final Uint8List %sFixedLayout = Uint8List.fromList(const <int>[\n", lower)
	g.emitBytes(layout)
	g.pf("]);\n")
	g.pf("%s", fixedFormatOn)
	g.pf("const int %sFixedLayoutBytes = %d;\n\n", lower, len(layout))
	g.pf("/// A FILE'S WHOLE HEAD: §3's sixteen-byte HEADER — the form byte, seven\n")
	g.pf("/// reserved zero bytes and the LAYOUT HASH at 8 — then the layout's u32\n")
	g.pf("/// length and the layout itself. The records begin here.\n")
	g.pf("const int %sFixedHeaderBytes = %d;\n\n", lower, 16+4+len(layout))

	g.pf("/// THE PREFILL: the declared defaults as a constant run of bytes. A field\n")
	g.pf("/// this record does not carry has no plan entry, so it keeps what this put\n")
	g.pf("/// there and the loop never learns it existed. THE LOAD COPIES ONLY THE\n")
	g.pf("/// RANGES THE PLAN DOES NOT LAND — identity's list is empty.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final Uint8List %sFixedPrefill = Uint8List.fromList(const <int>[\n", lower)
	g.emitBytes(prefill)
	g.pf("]);\n")
	g.pf("%s\n", fixedFormatOn)

	cover := fixedIdentityCover(plan)
	g.pf("/// THE TYPE'S VALUE BYTES: every byte of this build's own image that holds\n")
	g.pf("/// a DECLARED VALUE, sorted and merged. THE PREFILL IS THIS SET MINUS WHAT\n")
	g.pf("/// A PLAN LANDS (§3.4), so against the identity plan it is empty and the\n")
	g.pf("/// identity read writes no byte twice; a plan compiled from a stranger's\n")
	g.pf("/// layout subtracts itself from it once, when the plan is compiled, and\n")
	g.pf("/// prefills exactly the rest.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final Int32List %sFixedCover = Int32List.fromList(const <int>[\n", lower)
	for _, r := range cover {
		g.pf("  %d, %d,\n", r.dst, r.size)
	}
	g.pf("]);\n")
	g.pf("%s", fixedFormatOn)
	g.pf("const int %sFixedCoverCount = %d;\n\n", lower, len(cover))

	g.pf("/// MY SIDE of the layout, five lanes per entry: the storage facts a layout\n")
	g.pf("/// entry cannot carry. In C++ these are offsetof rows; the reader's own\n")
	g.pf("/// storage in Dart is the canonical image, so they are its offsets.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final Int32List %sFixedDst = Int32List.fromList(const <int>[\n", lower)
	for i, d := range w.dst {
		g.pf("  %d, %d, %d, %d, %d, // %d %s\n", d.dst, d.stride, d.aux, d.counted, d.arg, i, w.entries[i].note)
	}
	g.pf("]);\n")
	g.pf("%s\n", fixedFormatOn)

	g.pf("/// THE IDENTITY PLAN, coalesced HERE rather than at run time: when a record's\n")
	g.pf("/// hash is this build's own the plan is the one the compiler already wrote.\n")
	g.pf("/// Because the reader's own storage in this language IS the wire's layout,\n")
	g.pf("/// source and destination advance together and the walk collapses to %d\n", len(plan))
	if len(plan) == 1 {
		g.pf("/// entry — the same coalescer the plan compiler runs, reaching its best case\n")
	} else {
		g.pf("/// entries — the same coalescer the plan compiler runs, reaching its best\n")
		g.pf("/// case\n")
	}
	g.pf("/// rather than skipping a step. A COUNT and a TEXT LENGTH keep entries of\n")
	g.pf("/// their own, because those two are the only bytes a record does not merely\n")
	g.pf("/// move: a hostile one is CLAMPED to this reader's bound before it reaches a\n")
	g.pf("/// value a consumer will index with.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final Int32List %sFixedIdentity = Int32List.fromList(const <int>[\n", lower)
	for _, e := range plan {
		note := ""
		if e.note != "" {
			note = " // " + e.note
		}
		g.pf("  %d, %d, %d, %d, %d, %d, %d, %d,%s\n",
			e.op, e.src, e.dst, e.size, e.aux, e.guard, e.arg, e.meta, note)
	}
	g.pf("]);\n")
	g.pf("%s", fixedFormatOn)
	g.pf("const int %sFixedIdentityCount = %d;\n\n", lower, len(plan))

	// ---- THE LINEAGE, OLDEST FIRST, THE CURRENT LAYOUT LAST (§5.2) ----
	//
	// It is the BUILD's data and never the wire's: a file is matched on its
	// header's hash against these entries, and the layout it carries is held to
	// a BYTE COMPARISON against the bytes the lock recorded, never walked.
	entries, floor := g.fixedLineage(st, FixedLineageEntry{
		Wire: hash, Layout: layout, Record: fixedHashBytes + body,
	})
	g.pf("/// THE LINEAGE: every layout of %s this build serves, OLDEST FIRST and\n", st.Name)
	g.pf("/// THE CURRENT ONE LAST (§5.2). A file is matched on its header's hash\n")
	g.pf("/// against these and on nothing else; the layout it carries is COMPARED\n")
	g.pf("/// with the bytes the lock recorded and never parsed. A hash no entry holds\n")
	g.pf("/// is `layout_newer` — ship the reader.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("final List<TableFixedKnownLayout> %sFixedKnown = <TableFixedKnownLayout>[\n", lower)
	for _, e := range entries {
		if e.Retired {
			g.pf("  // RETIRED: %s\n", e.Reason)
		}
		g.pf("  TableFixedKnownLayout(%s, Uint8List.fromList(const <int>[\n", fixedDartHex(e.Wire))
		g.emitBytes(e.Layout)
		g.pf("  ]), %d, %d),\n", len(e.Layout), e.Record)
	}
	g.pf("];\n")
	g.pf("%s\n", fixedFormatOn)
	g.pf("/// THE FLOOR: 1 + the highest RETIRED index, 0 when none is (§5.2). Below\n")
	g.pf("/// it a layout this build once served is retired and the answer is\n")
	g.pf("/// `layout_unsupported` — upgrade the client — rather than `layout_newer`.\n")
	g.pf("const int %sFixedFloor = %d;\n\n", lower, floor)
	g.pf("/// ONE PLAN PER LINEAGE ENTRY, built from THE LOCK'S bytes the first time\n")
	g.pf("/// this list is read and never again (§5.9 #3): nothing on the load path\n")
	g.pf("/// compiles, nothing on it parses a layout a file carried, and the load\n")
	g.pf("/// cannot fail for want of a plan.\n")
	// `dart format` is this leg's formatting authority and the gate compares
	// against what it writes, so the call's shape is the one it would choose:
	// the flat header while it fits inside eighty columns, the wrapped one once
	// the table's name pushes it past.
	ind := "  "
	if len("final List<TableFixedLineagePlan> ")+len(lower)+len("FixedLineagePlans = tableFixedLineagePlans(") <= 80 {
		g.pf("final List<TableFixedLineagePlan> %sFixedLineagePlans = tableFixedLineagePlans(\n", lower)
	} else {
		g.pf("final List<TableFixedLineagePlan> %sFixedLineagePlans =\n", lower)
		g.pf("    tableFixedLineagePlans(\n")
		ind = "      "
	}
	g.pf("%[1]s%[2]sFixedKnown,\n%[1]s%[2]sFixedLayout,\n", ind, lower)
	g.pf("%[1]s%[2]sFixedDst,\n%[1]s%[2]sFixedCover,\n", ind, lower)
	g.pf("%[1]s%[2]sFixedCoverCount,\n%[1]s%[2]sFixedBodyBytes,\n", ind, lower)
	g.pf("%[1]s%[2]sFixedHash,\n", ind, lower)
	if ind == "  " {
		g.pf(");\n\n")
	} else {
		g.pf("    );\n\n")
	}

	// ---- the caller's plan storage ----
	g.pf("/// THE PLAN'S STORAGE IS THE CALLER'S, DECLARED BY CAPACITY, AND THE CODEC\n")
	g.pf("/// NEVER ALLOCATES. One of these per peer; a layout whose plan does not fit\n")
	g.pf("/// is a refusal by name.\n")
	g.pf("TableFixedPlan %sFixedNewPlan({\n", lower)
	g.pf("  int entryCapacity = 4096,\n")
	g.pf("  int remapCapacity = 4096,\n")
	g.pf("}) {\n")

	g.call("  ", "return ", "TableFixedPlan",
		[]string{"entryCapacity", lower + "FixedBodyBytes", "remapCapacity"}, ";")
	g.pf("}\n\n")

	// ---- the file ----
	g.pf("/// A FILE: §3's sixteen-byte HEADER, the layout behind its u32 length, then\n")
	g.pf("/// the records to the end of it. A FILE ALWAYS CARRIES THE LAYOUT — a file is\n")
	g.pf("/// read by somebody who was not there when it was written.\n")
	g.pf("int %sFixedMeasure(int count) {\n", lower)
	sum := fmt.Sprintf("  return %sFixedHeaderBytes + count * %sFixedRecordBytes;", lower, lower)
	if len(sum) <= 80 {
		g.pf("%s\n", sum)
	} else {
		g.pf("  return %sFixedHeaderBytes +\n", lower)
		g.pf("      count * %sFixedRecordBytes;\n", lower)
	}
	g.pf("}\n\n")

	// ---- the write ----
	g.pf("/// THE WRITE IS A TEMPLATE: the hash and zeros laid down first — which is\n")
	g.pf("/// also what zero-fills every byte of declared slack — and then value stores\n")
	g.pf("/// at constant offsets. No measuring pass, no id interning, no trailer and no\n")
	g.pf("/// second walk. Answers the bytes written, or -1; nothing here throws.\n")
	g.emitFn("int", lower+"FixedSave",
		[]string{"List<" + st.Name + "> values", "int count", "Uint8List bytes"})
	g.pf("  final need = %sFixedMeasure(count);\n", lower)
	g.pf("  if (count < 0 || bytes.length < need) {\n    return -1;\n  }\n")
	g.pf("  final view = ByteData.sublistView(bytes);\n")
	g.pf("  // THE HEADER, ONE RULE FOR ALL FIVE FORMS (§3): the form byte, seven\n")
	g.pf("  // RESERVED ZERO bytes, the LAYOUT HASH at 8, and the body at 16.\n")
	g.call("  ", "", "bytes.fillRange",
		[]string{"0", "TableFixedLimits.headerBytes", "0"}, ";")
	g.pf("  bytes[0] = tableFixedForm;\n")
	g.call("  ", "", "view.setUint64",
		[]string{"TableFixedLimits.hashAt", lower + "FixedHash", "Endian.little"}, ";")
	g.call("  ", "", "view.setUint32",
		[]string{"TableFixedLimits.headerBytes", lower + "FixedLayoutBytes", "Endian.little"}, ";")
	g.call("  ", "", "bytes.setRange", []string{
		"TableFixedLimits.layoutAt",
		lower + "FixedHeaderBytes",
		lower + "FixedLayout",
	}, ";")
	g.pf("  var at = %sFixedHeaderBytes;\n", lower)
	g.pf("  for (var k = 0; k < count; k++) {\n")
	g.pf("    view.setUint64(at, %sFixedHash, Endian.little);\n", lower)
	g.pf("    // the template's zeros, which is also every byte of declared slack\n")
	g.call("    ", "", "bytes.fillRange",
		[]string{"at + 8", "at + " + lower + "FixedRecordBytes", "0"}, ";")
	g.call("    ", "", lower+"FixedWriteBody",
		[]string{"bytes", "view", "at + 8", "values[k]"}, ";")
	g.pf("    at += %sFixedRecordBytes;\n", lower)
	g.pf("  }\n  return need;\n}\n\n")

	// ---- the read ----
	g.pf("/// THE READ: a prefill and ONE loop over ONE plan — the identity plan when\n")
	g.pf("/// the layout's hash is this build's own, and a plan compiled once from the\n")
	g.pf("/// writer's layout otherwise. THE SAME LOOP EITHER WAY: there is no strict\n")
	g.pf("/// flag, no fast path and no second reader to keep honest against the first,\n")
	g.pf("/// which is the owner's own ruling — a form whose cost moved when a peer\n")
	g.pf("/// shipped would be a cliff at exactly the moment a deployment cannot afford\n")
	g.pf("/// one.\n")
	g.pf("///\n")
	g.pf("/// THE PREFILL IS EXACTLY THE BYTES THE PLAN DOES NOT LAND. It is a list of\n")
	g.pf("/// ranges the plan compiler works out once, and on the identity plan that\n")
	g.pf("/// list is EMPTY — so this read writes no byte twice, and it is one rule for\n")
	g.pf("/// both plans rather than a flag that asks which one this is. Answers the\n")
	g.pf("/// records read, or -1 with the reason named in the report.\n")
	g.emitFn("int", lower+"FixedLoad", []string{
		"List<" + st.Name + "> values", "int capacity", "Uint8List bytes",
		"int byteLength", "TableFixedPlan plan", "TableFixedReport report",
	})
	g.pf("  report.reset();\n")
	g.pf("  if (byteLength < TableFixedLimits.layoutAt || byteLength > bytes.length) {\n")
	g.pf("    report.malformed = true;\n    return -1;\n  }\n")
	g.pf("  final view = ByteData.sublistView(bytes);\n")
	g.pf("  // THE FORM BYTE IS READ FIRST, AND IT SAYS WHICH DIRECTION (§3, §3.4):\n")
	g.pf("  // the registry is ordered, so a byte this reader does not carry is named\n")
	g.pf("  // by where it sits relative to this form and never by one word for both.\n")
	g.pf("  if (bytes[0] != tableFixedForm) {\n")
	g.pf("    report.refused = switch (bytes[0]) {\n")
	g.pf("      TableFixedLimits.variableForm => TableFixedRefusal.previousForm,\n")
	g.pf("      TableFixedLimits.messageForm => TableFixedRefusal.messageFormAsFile,\n")
	g.pf("      _ => TableFixedRefusal.newerForm,\n")
	g.pf("    };\n")
	g.pf("    return -1;\n  }\n")
	g.call("  ", "final layoutBytes = ", "view.getUint32",
		[]string{"TableFixedLimits.headerBytes", "Endian.little"}, ";")
	g.pf("  if (layoutBytes + TableFixedLimits.layoutAt > byteLength) {\n")
	g.pf("    report.refused = TableFixedRefusal.layoutMalformed;\n    return -1;\n  }\n")
	g.call("  ", "final hash = ", "view.getUint64",
		[]string{"TableFixedLimits.hashAt", "Endian.little"}, ";")
	// STEP 5 and STEP 6: SELECT BY HASH, then the floor. Outside the lineage is
	// layout_newer — ship the reader; below the floor is layout_unsupported —
	// upgrade the client. BOTH report the FILE'S hash (§5.9 #7), and
	// layout_newer reports it AND NOTHING ELSE.
	g.pf("  // THE LINEAGE SELECT (§5.3 steps 5 and 6): the header's hash is taken as\n")
	g.pf("  // GIVEN — the digest is not on the wire, so it cannot be re-derived from a\n")
	g.pf("  // file — and it is matched against the layouts THE LOCK recorded. Nothing\n")
	g.pf("  // parses a stranger's layout, on any path.\n")
	g.pf("  final pick = tableFixedSelect(%sFixedKnown, hash);\n", lower)
	g.pf("  if (pick < 0) {\n")
	g.pf("    report.layoutHash = hash;\n")
	g.pf("    report.refused = TableFixedRefusal.layoutNewer;\n    return -1;\n  }\n")
	g.pf("  if (pick < %sFixedFloor) {\n", lower)
	g.pf("    report.layoutHash = hash;\n")
	g.pf("    report.refused = TableFixedRefusal.layoutUnsupported;\n    return -1;\n  }\n")
	// STEP 7: a known hash is read under THE LOCK'S layout bytes. A difference
	// is ONE name — a lie about a known version — and §1.1's seven rules do not
	// run at read time at all.
	g.pf("  // THE BYTES THE LOCK RECORDED, compared and never walked: every §1.1\n")
	g.pf("  // malformation under a KNOWN hash is one name, layout_malformed.\n")
	g.pf("  final known = %sFixedKnown[pick];\n", lower)
	g.pf("  if (layoutBytes != known.layoutBytes) {\n")
	g.pf("    report.refused = TableFixedRefusal.layoutMalformed;\n    return -1;\n  }\n")
	g.pf("  for (var i = 0; i < layoutBytes; i++) {\n")
	g.pf("    if (bytes[TableFixedLimits.layoutAt + i] != known.layout[i]) {\n")
	g.pf("      report.refused = TableFixedRefusal.layoutMalformed;\n      return -1;\n    }\n")
	g.pf("  }\n")
	g.pf("  report.hash = hash; // the header's hash; digest is not on the wire\n")
	// STEP 8: the plan this peer resolves to, and the record size FROM THE LOCK.
	g.pf("  // THE PLAN THIS PEER RESOLVES TO, and the record size FROM THE LOCK and\n")
	g.pf("  // never from the file.\n")
	g.pf("  var entries = %sFixedIdentity;\n", lower)
	g.pf("  var entryCount = %sFixedIdentityCount;\n", lower)
	g.pf("  var recordBytes = known.recordBytes;\n")
	g.pf("  var fills = plan.fill;\n")
	g.pf("  // THE PREFILL'S RANGES. EMPTY ON THE IDENTITY PLAN, and not by a flag:\n")
	g.pf("  // the rule is the type's value bytes minus what the plan lands, and the\n")
	g.pf("  // identity plan lands all of them.\n")
	g.pf("  var fillCount = 0;\n")
	g.pf("  var remap = plan.remap;\n")
	g.pf("  // THE COMPILE CENSUS IS THE PLAN'S OWN NUMBERS, carried onto the report\n")
	g.pf("  // ONCE after the record loop and only on a read that returns (§5.9 #6):\n")
	g.pf("  // once per peer, never per record, and REFUSE moves no counter at all.\n")
	g.pf("  var censusUnknown = 0;\n")
	g.pf("  var censusKind = 0;\n")
	g.pf("  if (hash != %sFixedHash) {\n", lower)
	g.pf("    final lane = %sFixedLineagePlans[pick];\n", lower)
	g.pf("    if (lane.why != TableFixedRefusal.none) {\n")
	g.pf("      report.refused = lane.why;\n      return -1;\n    }\n")
	g.pf("    // THE CALLER'S PLAN STAYS A CAPACITY DECLARATION and is no longer\n")
	g.pf("    // written through (§5.9 #5): the entry count is checked against it and\n")
	g.pf("    // a plan past it refuses by name.\n")
	g.pf("    if (lane.count > plan.capacity) {\n")
	g.pf("      report.refused = TableFixedRefusal.planTooLarge;\n      return -1;\n    }\n")
	g.pf("    entries = lane.entries;\n    entryCount = lane.count;\n")
	g.pf("    fills = lane.fill;\n    fillCount = lane.fillCount;\n")
	g.pf("    remap = lane.remap;\n")
	g.pf("    censusUnknown = lane.unknown;\n    censusKind = lane.kindMismatch;\n")
	g.pf("  }\n")
	g.pf("  // THE HEADER NAMES THE LAYOUT ONCE. Identity memcmps the file's layout\n")
	g.pf("  // against this build's; digest is not on the wire, so the header hash is\n")
	g.pf("  // not a hash of the layout bytes alone (§3, bill §13).\n")
	g.pf("  final rest = byteLength - TableFixedLimits.layoutAt - layoutBytes;\n")
	g.pf("  // BYTES LEFT OVER ARE malformed, which is §3's rule for the same reason:\n")
	g.pf("  // the two ends of the file have met.\n")
	g.pf("  if (recordBytes <= 8 || rest %% recordBytes != 0) {\n")
	g.pf("    report.malformed = true;\n    return -1;\n  }\n")
	g.pf("  final n = rest ~/ recordBytes;\n")
	g.pf("  if (n > capacity) {\n")
	g.pf("    report.refused = TableFixedRefusal.batchTooLarge;\n    return -1;\n  }\n")
	g.pf("  var at = TableFixedLimits.layoutAt + layoutBytes;\n")
	g.pf("  for (var k = 0; k < n; k++) {\n")
	g.pf("    // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS IS A REFUSAL BY\n")
	g.pf("    // NAME, never a guess and never damage: nothing is decoded and no\n")
	g.pf("    // counter moves.\n")
	g.pf("    if (view.getUint64(at, Endian.little) != hash) {\n")
	g.pf("      report.refused = TableFixedRefusal.noLayout;\n      return -1;\n    }\n")
	g.call("    ", "", "tableFixedFillRun",
		[]string{"fills", "fillCount", lower + "FixedPrefill", "plan.image"}, ";")
	g.pf("    tableFixedRun(\n")
	g.pf("      entries,\n      entryCount,\n      bytes,\n      view,\n      at + 8,\n")
	g.pf("      plan.image,\n      plan.imageView,\n      remap,\n      plan.conv,\n")
	g.pf("      report,\n    );\n")
	g.call("    ", "", lower+"FixedDecode",
		[]string{"values[k]", "plan.image", "plan.imageView", "0", "report"}, ";")
	g.pf("    at += recordBytes;\n")
	g.pf("  }\n")
	g.pf("  // THE CENSUS LANDS ONCE, HERE, on a read that returns (§5.4, §5.9 #6).\n")
	g.pf("  report.unknown += censusUnknown;\n")
	g.pf("  report.kindMismatch += censusKind;\n")
	g.pf("  return n;\n}\n\n")
}

func (g *fixedModule) emitBytes(b []byte) {
	for i := 0; i < len(b); i += 12 {
		end := min(i+12, len(b))
		g.pf(" ")
		for _, v := range b[i:end] {
			g.pf(" 0x%02x,", v)
		}
		g.pf("\n")
	}
}

// fixedDartHex renders a 64-bit hash as a Dart int literal: the hex BIT
// PATTERN, which Dart reads as the two's complement value, because a decimal
// past the signed range is not a literal Dart accepts.
func fixedDartHex(v uint64) string {
	if v <= 0x7fffffffffffffff {
		return fmt.Sprintf("0x%x", v)
	}
	return fmt.Sprintf("0x%016x", v)
}

func (g *fixedModule) assemble() []byte {
	var h strings.Builder
	if g.file == nil {
		fmt.Fprintf(&h, "// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", g.unit.Package)
	} else {
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	}
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.\n", g.unit.Package)
	h.WriteString("//\n")
	h.WriteString("// A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND THEN\n")
	h.WriteString("// THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND. There is no field\n")
	h.WriteString("// reference, no kind byte, no length, no terminator and no trailer: a body's\n")
	h.WriteString("// every byte is a value or the declared slack behind one.\n")
	h.WriteString("//\n")
	h.WriteString("// THIS BACKEND CARRIES FORM 3 AND ONLY FORM 3. Dart has no form-1 table wire\n")
	h.WriteString("// (schema#514), so nothing here reads or writes one, and nothing about form 1\n")
	h.WriteString("// moves for any port that does.\n")
	h.WriteString("//\n")
	if len(g.wide) > 0 {
		// NAMED, NEVER SILENT: the accelerators this unit does not get, and why.
		h.WriteString("// THIS UNIT GETS NO BLOCK AND NO COOK READ HALF (docs/SPEC-TABLES.md §19, §7):\n")
		h.WriteString("// their row layout has no fixed-point and no 128-bit kind in this backend yet\n")
		fmt.Fprintf(&h, "// (schema#366), and this unit declares %s.\n", strings.Join(g.wide, ", "))
		h.WriteString("// THE FIXED FORM BELOW CARRIES THOSE KINDS and is emitted in full: §3.4 fixes\n")
		h.WriteString("// their constant sizes, a fixed-point value riding as the raw scaled integer\n")
		h.WriteString("// at its storage width and a 128-bit one as the low half then the high.\n")
		h.WriteString("//\n")
	}
	h.WriteString("// Buffers are caller-owned Uint8Lists and the plan storage is the caller's.\n")
	h.WriteString("// Nothing throws: a write answers its byte count or -1, a read answers its\n")
	h.WriteString("// record count or -1 with the reason named in the report.\n")
	h.WriteString("\n")
	h.WriteString("import 'dart:typed_data';\n\n")

	bases := make([]string, 0, len(g.imports))
	for b := range g.imports {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, b := range bases {
		names := make([]string, 0, len(g.imports[b]))
		for s := range g.imports[b] {
			names = append(names, s)
		}
		sort.Strings(names)
		list := strings.Join(names, ", ")
		one := fmt.Sprintf("import '%s.dart' show %s;", b, list)
		if len(one) <= 80 {
			h.WriteString(one + "\n")
			continue
		}
		// the formatter's middle form: the URI, then the whole show list on
		// one continuation line, before it gives up and takes one name a line
		if len("    show "+list+";") <= 80 {
			fmt.Fprintf(&h, "import '%s.dart'\n    show %s;\n", b, list)
			continue
		}
		fmt.Fprintf(&h, "import '%s.dart'\n    show\n", b)
		for i, n := range names {
			sep := ","
			if i == len(names)-1 {
				sep = ";"
			}
			fmt.Fprintf(&h, "        %s%s\n", n, sep)
		}
	}
	if len(bases) > 0 {
		h.WriteString("\n")
	}
	h.WriteString(strings.Trim(g.body.String(), "\n"))
	h.WriteString("\n")
	return []byte(h.String())
}

package jstable

// THE FIXED FORM'S MODULES: <Base>Table.js per unit file that declares a
// table, and the unit's ONE RUNTIME HOME, <Package>Table.js, where the shared
// runtime and every type's write/decode helper land — the same home rule the
// block form's modules follow (block.go), for the same reason: an ES module is
// file-scoped, so a shared runtime is DEFINED once and imported everywhere
// else, and the module that defines it wants a name that does not move.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateFixedFiles emits <Base>Table.js for every unit file that declares a
// table, plus the unit's runtime home. A unit with no fixed-form root at all
// gets no Table.js: nothing would be in it but a refusal nobody imports.
func generateFixedFiles(u *ir.Unit, wide []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	roots := map[string][]*ir.Struct{}
	any := false
	for _, f := range u.Files {
		rs := fixedRoots(u, f.Tables)
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
	// `type`s alone, which gets no table surface of its own, and a consumer
	// would then import a helper nothing emitted.
	seen := map[string]bool{}
	var closure []*ir.Struct
	for _, f := range u.Files {
		for _, st := range roots[f.Base] {
			fixedCollectTypes(st, seen, &closure)
		}
	}

	homeWritten := false
	for _, f := range u.Files {
		if len(f.Tables) == 0 && f.Base != home {
			continue
		}
		g := &fixedModule{unit: u, file: f, base: f.Base, home: f.Base == home, homeBase: home,
			wide: wide, imports: map[string]map[string]bool{}}
		g.emit(closure, roots[f.Base], f.Tables)
		if g.home {
			homeWritten = true
		}
		out[f.Base+"Table.js"] = g.assemble()
	}
	if !homeWritten {
		g := &fixedModule{unit: u, base: home, home: true, homeBase: home,
			wide: wide, imports: map[string]map[string]bool{}}
		g.emit(closure, nil, nil)
		out[home+"Table.js"] = g.assemble()
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
	imports  map[string]map[string]bool
	gen      fixedGen
	body     strings.Builder
}

func (g *fixedModule) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

func (g *fixedModule) need(base string, symbols ...string) {
	if base == "" || base == g.base+"Table" {
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

func (g *fixedModule) needHome(symbols ...string) { g.need(g.homeBase+"Table", symbols...) }

func (g *fixedModule) emit(closure []*ir.Struct, roots []*ir.Struct, tables []*ir.Struct) {
	if g.home {
		g.pf("%s\n", fixedRuntime)
		g.gen.unit = g.unit
		// A TABLE gets its storage class here; a `type` already has one, from
		// internal/codegen/js, and is imported rather than emitted twice.
		inClosure := map[string]bool{}
		for _, st := range closure {
			if st.IsTable {
				inClosure[st.Name] = true
			}
		}
		need := func(cls, typeName string) {
			if inClosure[cls] {
				return // a table of this unit's closure, emitted right here
			}
			base := fixedDeclaringFile(g.unit, cls)
			if base == "" {
				return
			}
			// A UNION HAS NO Init IN internal/codegen/js — its reset is the tag
			// going back to None and nothing else — so asking for one imported
			// a name that module does not export, and the generated file died
			// at its own import line before a single record was read.
			if fixedDeclaredUnion(g.unit, cls) {
				g.need(base, cls)
				return
			}
			g.need(base, cls, "Init"+cls)
			_ = typeName
		}
		g.gen.need = need
		for _, st := range closure {
			if st.IsTable {
				g.gen.emitTableClass(st)
			}
		}
		for _, st := range closure {
			g.gen.emitWriteBody(st)
			g.gen.emitDecodeBody(st)
		}
		g.pf("%s", g.gen.body.String())
	}
	for _, st := range tables {
		if reason := fixedRefusal(st); reason != "" {
			// NAMED, NEVER SILENT: a table with no fixed form says which and why.
			g.pf("// table %s has NO FIXED FORM in JavaScript: %s.\n", st.Name, reason)
			g.pf("// Its form-1 surface is unaffected; nothing about form 1 moves (docs/SPEC-TABLES.md §3.4).\n\n")
		}
	}
	for _, st := range roots {
		g.emitRoot(st)
	}
}

// emitRoot is one table's whole fixed-form surface.
func (g *fixedModule) emitRoot(st *ir.Struct) {
	w := fixedWalkRoot(st)
	block := fixedBlockBytes(w.entries)
	hash := fixedBlockHash(block)
	body := fixedTypeBytes(st)
	prefill := fixedPrefillBytes(st)

	g.needHome("TableFixedForm", "TableFixedRun", "TableFixedCompile", "TableFixedHashOf",
		"TableFixedParseBlock", "TableFixedBlockView", "TableFixedPlan", "TableFixedSize",
		"TableFixedRefusal", "TableFixedResetReport")
	g.needHome(st.Name+"FixedWriteBody", st.Name+"FixedDecode", st.Name, "Init"+st.Name)

	g.pf("// ---- %s, THE FIXED FORM (docs/SPEC-TABLES.md §3.4) ----\n//\n", st.Name)
	g.pf("// A record is an eight-byte hash of the writer's vocabulary block and then\n")
	g.pf("// the values in declared order, every field at its declared storage width,\n")
	g.pf("// little-endian, nothing padded between fields.\n\n")

	g.pf("// MeasureBody IS A CONSTANT on this form: the body is the same size for\n")
	g.pf("// every value the type can hold.\n")
	g.pf("export const %sFixedBodyBytes = %d;\n", st.Name, body)
	g.pf("export const %sFixedRecordBytes = %d; // the hash and the body\n\n", st.Name, fixedHashBytes+body)

	g.pf("// fnv1a64 over the block's bytes, carried as two uint32 lanes: a hash is\n")
	g.pf("// compared ONCE PER RECORD, and a BigInt there is one allocation per record.\n")
	g.pf("export const %sFixedHashLo = 0x%08x;\n", st.Name, uint32(hash))
	g.pf("export const %sFixedHashHi = 0x%08x;\n\n", st.Name, uint32(hash>>32))

	g.pf("// THE VOCABULARY BLOCK: %d entries, a PRE-ORDER walk of the closure in the\n", len(w.entries))
	g.pf("// writer's declared order. Every byte is settled by the compiler.\n")
	g.pf("export const %sFixedBlock = new Uint8Array([\n", st.Name)
	g.emitBytes(block)
	g.pf("]);\n")
	g.pf("export const %sFixedBlockBytes = %d;\n\n", st.Name, len(block))

	g.pf("// THE PREFILL: the declared defaults as a constant run of bytes, laid down\n")
	g.pf("// with one set. A field this record does not carry has no plan entry, so it\n")
	g.pf("// keeps what this put there and the loop never learns it existed.\n")
	g.pf("const %sFixedPrefill = new Uint8Array([\n", st.Name)
	g.emitBytes(prefill)
	g.pf("]);\n\n")

	g.pf("// MY SIDE of the block, five lanes per entry: the storage facts a block\n")
	g.pf("// entry cannot carry. In C++ these are offsetof rows; the reader's own\n")
	g.pf("// storage in JavaScript is the canonical image, so they are its offsets.\n")
	g.pf("const %sFixedDst = new Int32Array([\n", st.Name)
	for i, d := range w.dst {
		g.pf("  %d, %d, %d, %d, %d, // %d %s\n", d.dst, d.stride, d.aux, d.counted, d.arg, i, w.entries[i].note)
	}
	g.pf("]);\n\n")

	g.pf("// THE IDENTITY PLAN. Every entry a copy, with ADJACENT RUNS COALESCED — and\n")
	g.pf("// because the reader's own storage in this language IS the wire's layout,\n")
	g.pf("// source and destination advance together over the whole body and the walk\n")
	g.pf("// coalesces to exactly ONE run. That is the same coalescer the plan compiler\n")
	g.pf("// runs, reaching its best case rather than skipping a step.\n")
	g.pf("const %sFixedIdentity = new Int32Array([0, 0, 0, %d, 0, -1, 0, 0]);\n\n", st.Name, body)

	// ---- the caller's plan storage ----
	g.pf("// THE PLAN'S STORAGE IS THE CALLER'S, DECLARED BY CAPACITY, AND THE CODEC\n")
	g.pf("// NEVER ALLOCATES. One of these per peer; a block whose plan does not fit is\n")
	g.pf("// a refusal by name.\n")
	g.pf("export function %sFixedNewPlan(entryCapacity, remapCapacity) {\n", st.Name)
	g.pf("  return new TableFixedPlan(entryCapacity || 4096, %sFixedBodyBytes, remapCapacity || 4096);\n}\n\n", st.Name)

	// ---- the file ----
	g.pf("// A FILE: the form byte, the block length, the block, then the records to\n")
	g.pf("// the end of it. A file ALWAYS carries the block — a file is read by\n")
	g.pf("// somebody who was not there when it was written.\n")
	g.pf("export function %sFixedMeasure(count) {\n", st.Name)
	g.pf("  return 1 + 4 + %sFixedBlockBytes + count * %sFixedRecordBytes;\n}\n\n", st.Name, st.Name)

	// ---- the write ----
	g.pf("// THE WRITE IS A TEMPLATE: the hash and zeros laid down first — which is\n")
	g.pf("// also what zero-fills every byte of declared slack — and then value stores\n")
	g.pf("// at constant offsets. No measuring pass, no id interning, no trailer and no\n")
	g.pf("// second walk. Answers the bytes written, or -1, exactly as the flat packet\n")
	g.pf("// writer does; nothing here throws.\n")
	g.pf("export function %sFixedSave(values, count, bytes) {\n", st.Name)
	g.pf("  const need = %sFixedMeasure(count);\n", st.Name)
	g.pf("  if (count < 0 || bytes === null || bytes.length < need) { return -1; }\n")
	g.pf("  bytes[0] = TableFixedForm;\n")
	g.pf("  bytes[1] = %sFixedBlockBytes & 0xff;\n", st.Name)
	g.pf("  bytes[2] = (%sFixedBlockBytes >>> 8) & 0xff;\n", st.Name)
	g.pf("  bytes[3] = (%sFixedBlockBytes >>> 16) & 0xff;\n", st.Name)
	g.pf("  bytes[4] = (%sFixedBlockBytes >>> 24) & 0xff;\n", st.Name)
	g.pf("  bytes.set(%sFixedBlock, 5);\n", st.Name)
	g.pf("  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);\n")
	g.pf("  let at = 5 + %sFixedBlockBytes;\n", st.Name)
	g.pf("  for (let k = 0; k < count; k++) {\n")
	g.pf("    view.setUint32(at, %sFixedHashLo, true);\n", st.Name)
	g.pf("    view.setUint32(at + 4, %sFixedHashHi, true);\n", st.Name)
	g.pf("    bytes.fill(0, at + 8, at + %sFixedRecordBytes); // the template's zeros\n", st.Name)
	g.pf("    %sFixedWriteBody(view, at + 8, values[k]);\n", st.Name)
	g.pf("    at += %sFixedRecordBytes;\n", st.Name)
	g.pf("  }\n  return need;\n}\n\n")

	// ---- the read ----
	g.pf("// THE READ: a prefill and ONE loop over ONE plan — the identity plan when\n")
	g.pf("// the block's hash is this build's own, and a plan compiled once from the\n")
	g.pf("// writer's block otherwise. THE SAME LOOP EITHER WAY: there is no strict\n")
	g.pf("// flag, no fast path and no second reader to keep honest against the first,\n")
	g.pf("// which is the owner's own ruling — a form whose cost moved when a peer\n")
	g.pf("// shipped would be a cliff at exactly the moment a deployment cannot afford\n")
	g.pf("// one. Answers the records read, or -1 with the reason named in the report.\n")
	g.pf("export function %sFixedLoad(values, capacity, bytes, byteLength, plan, report) {\n", st.Name)
	g.pf("  TableFixedResetReport(report);\n")
	g.pf("  if (bytes === null || byteLength < 5) { report.malformed = true; return -1; }\n")
	g.pf("  if (bytes[0] !== TableFixedForm) { report.refused = TableFixedRefusal.NewerForm; return -1; }\n")
	g.pf("  const blockBytes = (bytes[1] | (bytes[2] << 8) | (bytes[3] << 16) | (bytes[4] << 24)) >>> 0;\n")
	g.pf("  if (blockBytes + 5 > byteLength) { report.refused = TableFixedRefusal.BlockMalformed; return -1; }\n")
	g.pf("  const h = TableFixedHashOf(bytes, 5, blockBytes);\n")
	g.pf("  const hashLo = h[0] >>> 0, hashHi = h[1] >>> 0;\n")
	g.pf("  report.hashLo = hashLo | 0; report.hashHi = hashHi | 0;\n")
	g.pf("  if (plan === null) { report.refused = TableFixedRefusal.NoBlock; return -1; }\n")
	g.pf("  let entries = %sFixedIdentity, entryCount = 1, remap = null;\n", st.Name)
	g.pf("  let recordBytes = %sFixedRecordBytes;\n", st.Name)
	g.pf("  if (hashLo !== (%sFixedHashLo >>> 0) || hashHi !== (%sFixedHashHi >>> 0)) {\n", st.Name, st.Name)
	g.pf("    // ANOTHER WRITER: the same loop, over a plan compiled from its block and\n")
	g.pf("    // CACHED BY HASH, so the compile is paid once per peer, not per record.\n")
	g.pf("    if (!plan.ready || (plan.hashLo >>> 0) !== hashLo || (plan.hashHi >>> 0) !== hashHi) {\n")
	g.pf("      const theirs = new TableFixedBlockView();\n")
	g.pf("      if (!TableFixedParseBlock(bytes, 5, blockBytes, theirs)) { report.refused = TableFixedRefusal.BlockMalformed; return -1; }\n")
	g.pf("      const made = TableFixedCompile(theirs, %sFixedBlock, %sFixedDst, plan, report);\n", st.Name, st.Name)
	g.pf("      if (made < 0) { report.refused = TableFixedRefusal.PlanTooLarge; return -1; }\n")
	g.pf("      plan.recordBytes = 8 + TableFixedSize(theirs, 0);\n")
	g.pf("      plan.hashLo = hashLo | 0; plan.hashHi = hashHi | 0; plan.ready = true;\n")
	g.pf("    }\n")
	g.pf("    entries = plan.entries; entryCount = plan.count; remap = plan.remap;\n")
	g.pf("    recordBytes = plan.recordBytes;\n")
	g.pf("  }\n")
	g.pf("  const rest = byteLength - 5 - blockBytes;\n")
	g.pf("  // BYTES LEFT OVER ARE malformed, which is §3's rule for the same reason:\n")
	g.pf("  // the two ends of the file have met.\n")
	g.pf("  if (recordBytes <= 8 || rest %% recordBytes !== 0) { report.malformed = true; return -1; }\n")
	g.pf("  const n = (rest / recordBytes) | 0;\n")
	g.pf("  if (n > capacity) { report.refused = TableFixedRefusal.BatchTooLarge; return -1; }\n")
	g.pf("  const image = plan.image, imageView = plan.view;\n")
	g.pf("  let at = 5 + blockBytes;\n")
	g.pf("  for (let k = 0; k < n; k++) {\n")
	g.pf("    // A RECORD WHOSE HASH NAMES NO BLOCK THIS READER HOLDS IS A REFUSAL BY\n")
	g.pf("    // NAME, never a guess and never damage: nothing is decoded, no counter moves.\n")
	g.pf("    if (((bytes[at] | (bytes[at + 1] << 8) | (bytes[at + 2] << 16) | (bytes[at + 3] << 24)) >>> 0) !== hashLo ||\n")
	g.pf("        ((bytes[at + 4] | (bytes[at + 5] << 8) | (bytes[at + 6] << 16) | (bytes[at + 7] << 24)) >>> 0) !== hashHi) {\n")
	g.pf("      report.refused = TableFixedRefusal.NoBlock; return -1;\n")
	g.pf("    }\n")
	g.pf("    image.set(%sFixedPrefill);            // the declared defaults, one prefill\n", st.Name)
	g.pf("    TableFixedRun(entries, entryCount, bytes, at + 8, image, remap, report);\n")
	g.pf("    %sFixedDecode(values[k], imageView, 0, report);\n", st.Name)
	g.pf("    at += recordBytes;\n")
	g.pf("  }\n  return n;\n}\n\n")
}

func (g *fixedModule) emitBytes(b []byte) {
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		g.pf(" ")
		for _, v := range b[i:end] {
			g.pf(" 0x%02x,", v)
		}
		g.pf("\n")
	}
}

func (g *fixedModule) assemble() []byte {
	var h strings.Builder
	h.WriteString(generatedFrom(g.file, g.unit))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.\n", g.unit.Package)
	h.WriteString("//\n")
	if g.home {
		h.WriteString("// " + runtimeHomeMarker + " — <Package>Table.js, one home per unit, named by\n")
		h.WriteString("// the package and independent of file order.\n")
		h.WriteString("//\n")
	}
	h.WriteString("// A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH OF THE WRITER'S VOCABULARY BLOCK,\n")
	h.WriteString("// AND THEN THE VALUES IN DECLARED ORDER, EVERY FIELD AT ITS BOUND. There is no\n")
	h.WriteString("// field reference, no kind byte, no length, no terminator and no trailer: a\n")
	h.WriteString("// body's every byte is a value or the declared slack behind one.\n")
	h.WriteString("//\n")
	h.WriteString("// THIS BACKEND CARRIES FORM 3 AND ONLY FORM 3. JavaScript has no form-1 table\n")
	h.WriteString("// wire (schema#516), so nothing here reads or writes one, and nothing about\n")
	h.WriteString("// form 1 moves for any port that does.\n")
	h.WriteString("//\n")
	if len(g.wide) > 0 {
		// NAMED, NEVER SILENT: the accelerators this unit does not get, and why.
		h.WriteString("// THIS UNIT GETS NO BLOCK AND NO COOK READ HALF (docs/SPEC-TABLES.md §19, §7):\n")
		fmt.Fprintf(&h, "// their row layout has no fixed-point and no 128-bit kind in this backend yet\n")
		fmt.Fprintf(&h, "// (schema#366), and this unit declares %s.\n", strings.Join(g.wide, ", "))
		h.WriteString("// THE FIXED FORM BELOW CARRIES THOSE KINDS and is emitted in full: §3.4 fixes\n")
		h.WriteString("// their constant sizes, a fixed-point value riding as the raw scaled integer at\n")
		h.WriteString("// its storage width and a 128-bit one as the low half then the high.\n")
		h.WriteString("//\n")
	}
	h.WriteString("// Buffers are caller-owned Uint8Arrays and the plan storage is the caller's.\n")
	h.WriteString("// Nothing throws: a write answers its byte count or -1, a read answers its\n")
	h.WriteString("// record count or -1 with the reason named in the report.\n")

	bases := make([]string, 0, len(g.imports))
	for b := range g.imports {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	if len(bases) > 0 {
		h.WriteString("\n")
	}
	for _, b := range bases {
		syms := make([]string, 0, len(g.imports[b]))
		for s := range g.imports[b] {
			syms = append(syms, s)
		}
		sort.Strings(syms)
		fmt.Fprintf(&h, "import { %s } from \"./%s.js\";\n", strings.Join(syms, ", "), b)
	}
	h.WriteString("\n")
	h.WriteString(g.body.String())
	return []byte(h.String())
}

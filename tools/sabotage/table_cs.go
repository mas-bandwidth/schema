package main

func init() {
	sabotages["table-cs-message-blob-header"] = []edit{{
		old: `NativePut(body + offset, length, 4);`,
		new: `NativePut(body + offset, length, 8); /* SABOTAGED: length consumes the reserved word */`,
	}}
	sabotages["table-cs-pack-short"] = []edit{{
		old: `extent = checked(extent + (long)count * f.NativeElementSize);`,
		new: `extent = checked(extent + (long)count * f.NativeElementSize); if (w.Bytes == null && count > 0) { extent--; } // SABOTAGED: the probe consumes one byte too few`,
	}}
	sabotages["table-cs-duplicate-ids"] = []edit{{
		// Target Load's verdict, where report lives, rather than a comparison
		// inside the vocabulary helper's small or hashed validation path.
		old: `if (!DistinctVocabulary(vocabulary)) { Damage(report); return Finish(report, Verdict.Damaged); }`,
		new: `if (!DistinctVocabulary(vocabulary) && report.Refused) { Damage(report); return Finish(report, Verdict.Damaged); } /* SABOTAGED: duplicate identities pass */`,
	}}
	sabotages["table-cs-json-field"] = []edit{{
		old: `TableFieldInfo f = info.Fields[index];`,
		new: `TableFieldInfo f = info.Fields[info.Name == "WeaponConfig" && index == 0 ? 1 : index]; // SABOTAGED: read damage through the adjacent float descriptor`,
	}}
}

func init() {
	sabotages["table-cs-retain-capture"] = []edit{{
		old: "        retain->Used += need; retain->Count++; report.Retained++; return true;",
		new: "        report.RetainLost++; return true; // SABOTAGED: the framed record is not admitted",
	}}
}

func init() {
	sabotages["fixed-form-cs-arm-text-flavour"] = []edit{
		{
			old: "                    TableFixedEntry e = new TableFixedEntry(\n" +
				"                        their_at, at, units, aux_at, guard, Text, arg, 0, 0, d.Arg);",
			new: "                    TableFixedEntry e = new TableFixedEntry(\n" +
				"                        their_at, at, units, aux_at, guard, Text, d.Arg, 0, 0); // SABOTAGED: the flavour over the arm ordinal",
		},
		{
			old: "                        uint unit = (p.Meta == TextWide) ? 2u : 1u;",
			new: "                        uint unit = (p.Arg == TextWide) ? 2u : 1u; // SABOTAGED: the flavour read back off the guard's lane",
		},
	}
}

// THE FINITE RULE'S THREE BLADES (docs/SPEC-TABLES.md §3.3), one per control
// mode of test/cs-tables/finite-negative-control: the form-1 read's raw-bits
// test, the fixed read's finite pass, and the form-1 writer's refusal. Each
// keeps its line compiling warning-free and turns only its own vector red.
func init() {
	sabotages["table-cs-finite-read"] = []edit{{
		old: `if ((kind == 10 || kind == 11) && NonFiniteBits(kind, raw)) { NonFinite(f, report); return true; }
            if (kind == 10 && f.Kind == 11) { raw = WidenFloat((uint)raw); }
            if (f.ClampRaw != null) { raw = f.ClampRaw(raw, report); }
        }
        if (f.GetBuffer != null) { f.GetBuffer(value)[index] = (byte)raw; }`,
		new: `if ((kind == 10 || kind == 11) && NonFiniteBits(kind, raw) && report == null) { NonFinite(f, report); return true; } // SABOTAGED: the read lands a NaN
            if (kind == 10 && f.Kind == 11) { raw = WidenFloat((uint)raw); }
            if (f.ClampRaw != null) { raw = f.ClampRaw(raw, report); }
        }
        if (f.GetBuffer != null) { f.GetBuffer(value)[index] = (byte)raw; }`,
	}}
	sabotages["table-cs-finite-fixed"] = []edit{{
		old: `g.pf("        if (finite_why != null)\n        {\n")`,
		new: `g.pf("        if (finite_why != null && report == null) // SABOTAGED: the fixed read keeps a NaN\n        {\n")`,
	}}
	sabotages["table-cs-finite-write"] = []edit{{
		old: `if (f.Kind == 10 || f.Kind == 11) { return !NonFiniteBits(f.Kind, f.GetRaw(value, i)); }`,
		new: `if (f.Kind == 10 || f.Kind == 11) { return f.GetRaw != null; } // SABOTAGED: the writer emits a NaN`,
	}}
}

// The C# table leg's controls (make/cs.mk, cs_leg_sabotage): each removes one
// rule a docs/PORTING.md row names, and the leg must go red on that rule.
func init() {
	// M5: the None-only guard, the defect the symmetric compare replaced.
	sabotages["table-cs-keyed-none-only"] = []edit{{
		old: `if ((uint)(key - 1) >= (uint)SlotCount)`,
		new: `if (key == 0 && SlotCount > 0) // SABOTAGED: the None-only guard`,
	}}
	// M6: the identity map forgets a closed node, so a node two pointers name
	// is numbered and written twice.
	sabotages["table-cs-identity-map"] = []edit{
		{old: `if (Indices.TryGetValue(child, out ulong old))`, new: `if (Nodes.Count < 0 && Indices.TryGetValue(child, out ulong old)) // SABOTAGED: no identity`},
		{old: `Indices.Add(child, (ulong)Nodes.Count + 2);`, new: `Indices[child] = (ulong)Nodes.Count + 2;`},
	}
	// M10: the caller's free is dropped, so a hook pair no longer balances.
	sabotages["table-cs-allocator-free"] = []edit{{
		old: `if (Free != null) { Free(Context, (IntPtr)pointer); }`,
		new: `if (Free != null) { /* SABOTAGED: the pair's free is dropped */ }`,
	}}
	// M14: the text writer never labels a shared node; it spells it in full at
	// every reference, so the text reads back as two nodes.
	sabotages["table-cs-text-label"] = []edit{{
		old: `if (o.Graph.Uses[child] <= 1) { return WriteValue(ref o, child, f.Table, depth); }`,
		new: `if (o.Graph.Uses[child] >= 1) { return WriteValue(ref o, child, f.Table, depth); } // SABOTAGED: no &node label`,
	}}
	// M17: the size test that sends a node larger than a slab to its own span
	// never fires, so the node is bump-allocated in a slab it does not fit.
	sabotages["table-cs-blob-span"] = []edit{{
		old: `if (bytes > TableArena.SlabBytes) { return Arena.Grab(bytes); }`,
		new: `if (bytes > TableArena.SlabBytes && bytes < 0) { return Arena.Grab(bytes); } // SABOTAGED: no span`,
	}}
	// M19: the builder map appends instead of inserting at the key's place.
	sabotages["table-cs-map-insert-order"] = []edit{{
		old: "        for (int i = slot->Count - 1; i > at; i--) { head->Items[i] = head->Items[i - 1]; }\n        head->Items[at] = entry; return (T*)entry;",
		new: "        head->Items[slot->Count - 1] = entry; return (T*)entry; // SABOTAGED: insertion order, no sort",
	}}
	// §2.5: the native region writer numbers the graph without the shared-blob
	// refusal, so a blob two slots name is written.
	sabotages["table-cs-region-blob-shared"] = []edit{{
		old: `if (type.Variable) { ids.Graph = RegionNumber(value, type, allocator, true); if (!ids.Graph.Valid) { return -1; } }`,
		new: `if (type.Variable) { ids.Graph = RegionNumber(value, type, allocator); if (!ids.Graph.Valid) { return -1; } } // SABOTAGED: shared blobs written`,
	}}
	// I7: the magic no longer refuses, so a cook of the other byte order is
	// refused later and not by name.
	sabotages["cook-open-order-cs"] = []edit{{
		old: `if (Schema.TableCookRead64(at + %d) != Schema.TableCookMagic) { reason =`,
		new: `if (Schema.TableCookRead64(at + %d) == ~Schema.TableCookMagic) { /* SABOTAGED: the magic does not refuse */ reason =`,
	}}
}

// M12: one fixed-form flat array read goes back to an unguarded host-order
// bulk copy, which a little-endian host cannot tell apart from the wire image.
func init() {
	sabotages["table-cs-host-order-read"] = []edit{{
		old: `return fmt.Sprintf("(t, b, l) => { if (%s != null) TableFixedWire.ReadLittle<%s>(b.Slice(0, Math.Min(b.Length, %s.Length * %d)), %s); }", fieldExpr, elemType, fieldExpr, elemBytes, fieldExpr)`,
		new: `return fmt.Sprintf("(t, b, l) => { if (%s != null) MemoryMarshal.Cast<byte, %s>(b.Slice(0, Math.Min(b.Length, %s.Length * %d))).CopyTo(%s); } /* SABOTAGED: host order */", fieldExpr, elemType, fieldExpr, elemBytes, fieldExpr)`,
	}}
}

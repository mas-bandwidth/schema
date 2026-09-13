RESULT: AUDIT cs at b7ab66a8 rows=86 implemented-asserted=38 weak=26 owed=19 inapplicable=3

## Corpus (make tables-fixedform-corpus, SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1)

```
fixed form: the C++ reference's byte oracle is in build/fixedform-corpus
fixed form: what each file holds is in build/fixedform-corpus/manifest.txt (66 lines)
```

## Gate (make tables-cs-leg tables-cs-versioning, SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1)

Result lines verbatim (exit 0):

```
cd test/cs-tables && /Users/glenn/.local/bin/dotnet run
SKIPPED: TestFixedFxCrossGeneration — §5.6 retires the run-time walk of a stranger's layout; the coverage moves to the lineage harness
SKIPPED: TestFixedVCase — §5.6 retires the compiled read of another generation's layout; the coverage moves to the lineage harness
SKIPPED: TestFixedPCase — §5.6 retires the compiled read of another generation's layout; the coverage moves to the lineage harness
SKIPPED: TestFixedNegativeControlForwardRead — §5.6 retires the forward read; the coverage moves to the lineage harness
SKIPPED: TestFixedLayoutValidation — §5.6 retires the run-time walk of a stranger's layout; §1.1's seven rules move to the LOCK's validation of what it records
cs fixed form: 5 test(s) skipped by name, §5.6
cs tables test passed
cd test/cs-tables && /Users/glenn/.local/bin/dotnet run -c Release
<same 5 SKIPPED lines>
cs fixed form: 5 test(s) skipped by name, §5.6
cs tables test passed
ok  	github.com/mas-bandwidth/schema/v2/internal/codegen/cstable	5.745s
tables C# versioning: §5 read both columns of every row against the C++ reference bytes
```

The 5 SKIPPED lines are §5.6 retirements (forward reads and the run-time layout walk), not G5 skips: the gate ran under `SCHEMA_SLOW=1`, and no skip came from the slow-test harness. No per-leg RED lines were printed (exit 0). The packet's known pre-existing reds — the two `RED [Reference fixes pending, fix 2]` lines `bool_byte_two` / `present_byte_two` — are cpp (`make tables-fixedform`) only and do not appear in the cs gate. The seven cross-cutting CI reds (G1, G2, G3) are CI-machinery, not the cs leg. Nothing was repaired (read-only).

## Exceptions

1. The #876 audit matrix — the exact item texts for `L1-L7`, `F1`–`F12`, `E1`–`E9`, `C1`–`C14`, `W1`–`W16`, `P1`–`P4` — is NOT in the tree at `b7ab66a8`; it lives in the #876 issue comment, which this read-only child cannot open. I reconstructed the item semantics from `docs/FIXED-FORM-ALGORITHM.md` §1–§7, `docs/FIXED-FORM-BILL-READS-BACKWARD.md`, the reference's own check-string prefixes (`test/tables/fixedform_main.cpp`, `test/tables/fixedform_properties.cpp`) and the packet's recast table. Items reconstructed rather than packet-defined are named in their `note` column: `E3 E5 E6 E8 E9`, `C8 C12 C13 C14`, and several `W` rows.

2. The packet's recast/strike of specific items was applied exactly as written: `C11` and `E1` are struck (excluded); `C1 C2 C7 C9 C10` keep the hostile half only; `E2 E4 E7` returned `inapplicable` with the retiring sentence quoted; `C6` audited as `R31`'s cell; `W10`/`W16` audited alongside `R14`/`R28`; `W5` requires a release run (present).

3. `L1-L7` is returned `weak`: `TestFixedLayoutValidation` (the seven §1.1 refusals) is skipped by name under §5.6, and the seven names now fire at read time only as `layout_malformed` under a known hash (R9). The LOCK's validation is group A's cell.

4. The cs leg has NO properties gate (`make tables-fixedform` is cpp). `P1` is partly covered in cs (`UtSame` compiled-vs-identity, `FixedFormChecks.cs:728`); `P2` (write-read-write byte-identity), `P3` (byte mutation), `P4` are owed.

5. The cs versioning probes assert "no refusal, counters clean" for all 25 rows, but only `field_append` asserts the actual landed VALUES (11/22/33, default 77) against the corpus; the other rows hardcode or omit value checks and none parse `manifest.txt` (§5.9 #32/#37). Flagged on `R27`.

6. `no_layout`, `plan_too_large`, `layout_kind_unknown` and the header-hash-mismatch refusals are asserted only inside `TestFixedNegativeControlForwardRead` / `TestFixedLayoutValidation`, both SKIPPED by §5.6; the runtime still emits them (`fixedruntime.go`), so their items are `weak` not `implemented-asserted`.

7. `R14` owed: `layout_record_too_large` is wired in cs only to the 65536 bound and the zero root (`fixedruntime.go:379,399,477`), not to the entry-past-writer's-record (fix 3) that §5.8 row 13 requires.

8. The seven owed versioning rows are exactly the packet's §2.7 list — `cfloat_res_refine`, `cfloat_range_widen`, `fixed_I_grow_element`, `writer_bound_count`, `refuse_writes_nothing`, `unknown_census`, `forged_ordinal_both_plans` — none present in `csVersionRows`.

9. `R24`/`C4` (ill-formed text refuses by name, `text_ill_formed`) rides open-PR #971, unmerged: `fixedbounds.go:124` — "THE CONTENT RULE IS NOT THIS LEG'S YET".

10. `R28` (guard chain, arm inside an arm answers the OUTER tag via `guard2`/`arg2`) is not implemented in cs — the emitted plan has a single guard lane (no `guard2`/`arg2`).

11. `R4` (hash) and `R5` (definitions digest) are computed by the shared Go compiler (`ir.TableFixedLayoutHash`, `ir.TableFixedDefinitionsDigest`); the cs backend only carries the digest verbatim (`fixedlineage.go:50`). These are compiler-side; the cs-leg runtime assertions cover the "never re-derive" half (`R7`).

12. I cloned `serialize.cs` (the C# runtime sibling) in addition to the packet's two named clones, because `test/cs-tables/schematables.csproj` references `../../../serialize.cs`; the leg gate cannot build without it.

13. `R16` returned owed as a whole because its two named probe rows (`unknown_census`, `forged_ordinal_both_plans`) are both in the owed list, even though `C10` (ordinal past top → None + clamped on both plans) IS asserted running at `FixedFormChecks.cs:765-790`.

## Status counts (this leg)

implemented-asserted 38 · weak 26 · owed 19 · inapplicable 3 = 86

## TSV (audit-cs.tsv, inline)

leg	item_id	status	evidence_path	assertion_or_counterexample	gate_run	lane_state	note
cs	L1-L7	weak	repo/test/cs-tables/src/FixedFormChecks.cs:52	"SKIPPED: TestFixedLayoutValidation — §5.6 retires the run-time walk of a stranger's layout; §1.1's seven rules move to the LOCK's validation of what it records"	make tables-cs-leg -> "cs tables test passed" (5 skips)	merged-on-lane	the seven §1.1 layout refusals; the read-side walk is retired by §5.6, the seven names fire only under a known hash as layout_malformed (R9)
cs	F1	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:340	"Check(bad < 0 && r4.Refused && r4.Reason == row.want, row.what)" over previous_form / message_form_as_file / newer_form	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§2 step 2: form byte b[0]!=3 refuses by direction
cs	F2	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.3 step 1: fewer than 20 bytes -> malformed; no running cs assertion (only the skipped layout-validation residue)
cs	F3	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:319	"Check(bad < 0 && r3.Refused && r3.Reason == "layout_malformed", "REFUSED BY NAME: layout_malformed")" (layout length exceeds buffer)	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§2 step 3: 20+L > bytes -> layout_malformed
cs	F4	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:622	"if (r.LayoutHash != 0) { bad += ProbeLog.Fail(@NAME@, "layout_hash is zero on every path but the two layout refusals (§5.9 #15)"); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 step 4: header hash taken as given; identity selected by index, never recomputed
cs	F5	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§2: layout appears once per carrier; no dedicated running cs assertion
cs	F6	weak	repo/test/cs-tables/src/FixedFormChecks.cs:402	"Check(bad < 0 && rNoLayout.Refused && rNoLayout.Reason == "no_layout", ...)"	make tables-cs-leg -> "cs tables test passed" (skipped)	merged-on-lane	§5.3 step 11: per-record hash no_layout; assertion lives in TestFixedNegativeControlForwardRead, skipped by §5.6
cs	F7	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.3 step 9: ragged tail rest%record_bytes!=0 -> malformed; not asserted running
cs	F8	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.3 step 10: batch_too_large; runtime emits it (fixedform.go:1789) but no running cs assertion
cs	F9	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:618	"if (n < 1 || r.Refused || r.Malformed) { bad += ProbeLog.Fail(@NAME@, "the reader's OWN hash selects the identity plan: n=" + n ...); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§4.2/§5.8 row 3: identity plan baked, no runtime compile (hash_identity probe)
cs	F10	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:585	"if (n != -1 || r.Reason != "layout_newer") { bad += ProbeLog.Fail(@NAME@, "a hash in no lineage entry owes layout_newer..."); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 step 5: plan selected by hash against the lineage
cs	F11	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:102	"if (back[0].W != 77) { bad += ProbeLog.Fail(@NAME@, "the appended field is not its declared default: " + back[0].W); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§4.3 prefill: appended field lands its declared default (field_append)
cs	F12	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:99	"Check(Run(2, 2u, new byte[] { 0x01, 0x01, 0xAA }) == 0, "ArgW: a two-byte tag 0x0101 whose low byte is 1 does NOT run arm 1");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.4 one run loop, guarded entries test the guard at its width
cs	E2	inapplicable	repo/docs/FIXED-FORM-ALGORITHM.md:932	"the remap of an unknown variant to `None`, the drop-and-count of an unknown field — all forward reads, now `layout_newer`"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	unknown nested type is a forward read retired by §5.6 (reader-judgement; the record names E1 only)
cs	E3	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:158	"t.Run(r.row+"/new_reads_old", ...)" for enum_append/enum_width (the ordinal prefix row)	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	reconstructed as the enum-ordinal prefix evolution; asserted by enum_append/enum_width probes
cs	E4	inapplicable	repo/docs/FIXED-FORM-ALGORITHM.md:394	"LADDER(a, b) or kind(a) == kind(b) or FAIL "kind changed""	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	kind_mismatch pairs: compile-time refusal is §5.1/S1's; the read-side case is unreachable through a lawful lineage
cs	E5	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:158	"t.Run(r.row+"/new_reads_old", ...)" for union_arm_payload_widen / union_append	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	reconstructed as the union arm/append evolution; asserted by the union rows
cs	E6	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:90	"widened int" (array_elem_widen/int_widen/uint_widen/float_widen/fixed_I_grow rows, widens: true)	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	reconstructed as the widening count (widened); asserted by the widens rows
cs	E7	inapplicable	repo/docs/FIXED-FORM-ALGORITHM.md:402	"FAIL "variant removed | inserted | reordered | renamed""	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	variant/arm inserted mid-list: compile-time refusal is §5.1/S1's
cs	E8	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:102	"if (back[0].W != 77) ... the appended field is not its declared default"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	reconstructed as the appended-field default; asserted by field_append/nested_append
cs	E9	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:158	"t.Run(r.row+"/new_reads_old", ...)" for keyed_array_enum_append / optional_add	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	reconstructed as keyed-slot / optional-add evolution
cs	C1	weak	repo/internal/codegen/cstable/fixedruntime.go:1133	"if (v < 0) { v = 0; if (report != null) report.Clamped++; }"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.5 count clamp v<0 (hostile half); runtime emits it, but the forged-count probe (writer_bound_count/R11) is owed
cs	C2	weak	repo/internal/codegen/cstable/fixedruntime.go:1134	"else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.5 count clamp v>Max (hostile half); no running forged-count assertion
cs	C3	weak	repo/internal/codegen/cstable/fixedruntime.go:1143	"if (v < 0) { v = 0; if (report != null) report.Clamped++; }" (text length clamp)	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.5 text length clamp; runtime emits it, no running forged-length assertion
cs	C4	owed	-	-	make tables-cs-leg -> "cs tables test passed"	open-PR #971	§4.5 fix 11: content violation refuses by name (text_ill_formed); fixedbounds.go:124 "THE CONTENT RULE IS NOT THIS LEG'S YET"
cs	C5	weak	repo/internal/codegen/cstable/fixedversioning_test.go:158	wstring_grow row probes (wide text units in code units)	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§4.5 wide text units; wstring_grow read row runs, no unit/clamp counter asserted
cs	C6	weak	repo/internal/codegen/cstable/fixedbounds_test.go:132	"TestFixedOrdinalOpIsSixtyFourBit ... strings.Contains(op, "ulong raw = 0;")"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.8 row 7 ordinal 64-bit temporary; generator-side Go test, not run by the leg gate (superseded by R31)
cs	C7	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:503	"Check(r.Clamped == 1, "signed fixed low end: and counts exactly ONE clamped");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.6 ranged-scalar clamp (hostile half), on both identity and compiled plans
cs	C8	weak	repo/internal/codegen/cstable/fixedbounds.go:216	"bits(N) width clamp: `maxv := 2^N - 1` emitted"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	reconstructed as bits(N) clamp; emitted, no running forged-bits assertion
cs	C9	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:747	"Check(v.Pick.Type == UT.UtPickType.None, "tag past the last arm: the union lands None"); Check(r1.Clamped == 1, ...)"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.6 union tag past arm count lands None, counts clamped (identity path)
cs	C10	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:784	"Check(bent.Pick.B.Grade == UT.UtGrade.None, "ordinal past the last variant: the compiled plan lands None too"); Check(r2.Clamped == 1, ...)"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.6 enum ordinal past top lands None, counts clamped on both plans
cs	C12	weak	repo/internal/codegen/cstable/fixedbounds.go:179	"if ((ulong)%s > %d) { %s = (%s)0; clamped++; }" (enum ordinal clamp)	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	reconstructed (ordinal remap/extent); emitted, running assertion is C10
cs	C13	weak	repo/internal/codegen/cstable/fixedbounds.go:151	"if ((ulong)%s.Type > %d) { %s.Type = (%s)0; clamped++; }" (union tag clamp)	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	reconstructed (union tag extent clamp on both plans)
cs	C14	weak	repo/internal/codegen/cstable/fixedbounds.go:187	"float range clamp (csClampBoth over f32/f64)"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	reconstructed (float range clamp); emitted, no running forged-float assertion
cs	W1	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:822	"Check(body.IndexOf((byte)0xAA) < 0, "SLACK IS ZERO: not one stained TEXT byte reached the wire");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 zeroed template + straight stores; text slack zero
cs	W2	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:823	"Check(body.IndexOf((byte)0x5A) < 0, "SLACK IS ZERO: not one stained ARRAY byte reached the wire");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 write only live elements / used length, never the whole bound
cs	W3	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:659	"Check(UT.Schema.UtRootFixedSave(root, wire) == wire.Length, "arm text: save fills what measure says");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 union writes the tag and the taken arm only (arm text save)
cs	W4	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:1027	"Check(body.IndexOf((byte)0x5A) < 0, "ABSENT OPTIONAL: not one byte of the absent payload reached the wire");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 absent optional writes flag 0 and skips the payload
cs	W5	weak	repo/test/cs-tables/src/FixedFormChecks.cs:822	release run green (dotnet run -c Release)	make tables-cs-leg -> "cs tables test passed" (Release)	merged-on-lane	§3.1 write-side bound checks are DEBUG only; release run passes, no assertion the checks are stripped
cs	W6	weak	repo/internal/codegen/cstable/fixedform.go:1765	"plan partitioned: `split` emitted, coalesce never crosses the split"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.1/§4.4 plan partition; generator-side only, no running cs assertion
cs	W7	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:126	"Check(FX1.Schema.FxRootFixedSave(one, w1) == w1.Length, "FX1 save");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3 measure == bytes, save fills the measured length
cs	W8	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:1006	"Check(resaved[homingOffset] == 1, "hostile bool: resaved byte is strictly 1, not 0x7F");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3 bool writes 0/1 on the wire
cs	W9	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:1059	"Check(pbody.IndexOf((byte)0x5A) >= 0, "absent optional: the present twin puts the bytes on the wire");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3 present flag and payload write (the discriminating twin)
cs	W10	weak	repo/internal/codegen/cstable/fixedruntime.go:379	"if (e.Size > RecordMaxBytes) { c.Fail("layout_record_too_large"); return 0; }"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.2 entry bounded by writer's record size; the name is wired only to the 65536 bound and zero root, not the entry-level fix 3 (R14)
cs	W11	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:843	"Check(back.MarksCount == 1 && back.Marks[0] == 7, "slack: the live count reads");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 counted array writes count then live elements
cs	W12	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:840	"Check(back.LabelLength == 2 && back.Label[0] == (byte)'h' ... "slack: the used length reads, and the buffer terminates at it");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 text writes length then payload; terminator at used length
cs	W13	weak	repo/internal/codegen/cstable/fixedversioning_test.go:158	wstring_grow row (wide text read, not write)	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§3.1 wide text write; read row runs, no write-side wide assertion
cs	W14	weak	repo/internal/codegen/cstable/fixedversioning_test.go:158	keyed_array_enum_append row	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§3.1 keyed array write every slot; read row runs, no write-side keyed assertion
cs	W15	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:566	"Check(back.Podium[0] == TD.Grade.Gold && ... "LoadoutConfig 3-byte fixed enum array reproduced");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§3.1 fixed array writes every element (folded run)
cs	W16	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§4.1 nested union arm answers the OUTER tag (guard2/arg2 conjunction); cs has no guard2 lane (R28)
cs	P1	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:728	"Check(UtSame(viaPlan, id), "arm text: a COMPILED plan lands exactly what the identity plan lands");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	P1 identity == compiled on fields and counters (UtSame + signed-fixed compiled path)
cs	P2	weak	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	P2 write-read-write byte-identical; no dedicated cs round-trip-identity assertion (cpp properties gate)
cs	P3	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	P3 mutate every record byte {00,01,02,7f,80,ff} both paths; only in cpp fixedform_properties gate
cs	P4	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	P4 (fourth property); no cs assertion found at this head
cs	R1	implemented-asserted	repo/internal/codegen/cstable/fixedlineage.go:103	"entries := append([]FixedLineageEntry(nil), g.lineage[st.Name]...)" oldest-first, own appended last	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.2 COMPILE lays lineage as static data, oldest first (exercised by lineage_merge probe)
cs	R2	implemented-asserted	repo/internal/codegen/cstable/fixedlineage.go:72	"Record: 8 + fixedTypeBytes(st)"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.2 record_bytes is 8+body; backend takes it whole, adds nothing
cs	R3	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:562	"if (r.Reason != "layout_unsupported") { bad += ProbeLog.Fail(@NAME@, "a hash below the floor owes layout_unsupported, not " + r.Reason); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.2/§5.3 floor, below it layout_unsupported reporting the file's hash (floor_below/floor_raise_live)
cs	R4	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:589	"if (r.LayoutHash != want) { bad += ProbeLog.Fail(@NAME@, "layout_newer reports THE FILE'S hash and nothing else"); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 hash never re-derived at runtime; the hash is a handed constant, identity by index
cs	R5	weak	repo/internal/codegen/cstable/fixedlineage.go:50	"Digest []byte ... carried and not recomputed"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.2 definitions digest is the compiler's; the cs backend carries it verbatim, no leg-side assertion runs
cs	R6	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.2 table past 65536 ceiling is not a fixed-form root (JS hurt #931); no cs-leg assertion
cs	R7	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:618	"if (n < 1 || r.Refused || r.Malformed) ... the reader's OWN hash selects the identity plan"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 step 8 identity lane is an index comparison, never a recomputed hash
cs	R8	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:585	"if (n != -1 || r.Reason != "layout_newer") ... a hash in no lineage entry owes layout_newer"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 hash in no lineage -> layout_newer, file's hash and nothing else
cs	R9	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:607	"if (n != -1 || r.Reason != "layout_malformed") ... a KNOWN hash whose layout bytes differ is one name, layout_malformed"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 known hash, different bytes -> layout_malformed (the seven §1.1 names collapse)
cs	R10	weak	repo/test/cs-tables/src/FixedFormChecks.cs:52	"SKIPPED: TestFixedLayoutValidation — §5.6 retires the run-time walk of a stranger's layout"	make tables-cs-leg -> "cs tables test passed" (skip)	merged-on-lane	§5.6 runtime walk + header-hash recompute retired by name; the skip is the evidence
cs	R11	owed	-	-	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3/§5.8 row 4 writer_bound_count probe owed (forged count 7 lands writer's bound, clamped==1)
cs	R12	owed	-	-	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3/§5.8 row 9 refuse_writes_nothing probe owed (hash check before prefill)
cs	R13	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:534	"if (r.Malformed) { bad += ProbeLog.Fail(@NAME@, "a refusal by name never sets malformed too (§5.3, the joint answer)"); }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.3 REFUSE is total: refused+reason and malformed never both, no counter moves
cs	R14	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.3/§5.8 row 13 layout_record_too_large for an entry (fix 3); cs wires the name only to the 65536 bound and zero root (fixedruntime.go:379,477)
cs	R15	weak	repo/docs/FIXED-FORM-ALGORITHM.md:932	"the count clamp across bounds, the range clamp across versions, the remap of an unknown variant to `None`, the drop-and-count of an unknown field — all forward reads, now `layout_newer`"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.6 four forward-read clamps retired; evidenced by the named skips, no dedicated assertion
cs	R16	owed	-	-	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.4/§5.8 rows 11,12 unknown_census + forged_ordinal_both_plans probes owed (the two named rows)
cs	R17	implemented-asserted	repo/test/cs-tables/src/FixedFormChecks.cs:1001	"Check(back.Homing == true, "hostile bool: nonzero byte normalized to true");"	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.4 bool/present byte !=0/1 normalises and counts nothing
cs	R18	weak	repo/internal/codegen/cstable/fixedbounds.go:176	"if csOrdinalFillsStorage(...) { return }" (a clamp that cannot fire is not emitted)	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.4 a clamp that cannot fire is not emitted; generator-side, no running cs assertion
cs	R19	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:458	"if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 ...) { bad += ... "counters moved on a clean backward read" }"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.4 clean NEW-READS-OLD of an append moves no counter
cs	R20	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:102	"if (back[0].W != 77) ... the appended field is not its declared default"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§21.2 landing rules: appended field lands its declared default (nonzero unknown withdrawn §5.9 #30)
cs	R21	weak	repo/internal/codegen/cstable/fixedversioning_test.go:110	float_widen row (widens)	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§21 widenf bit-exact (NaN payload kept); float_widen row runs but no NaN-payload assertion
cs	R22	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§21.4 closure rule: pointer/map/unbounded array compile refusal is the shared compiler's check, no cs-leg assertion
cs	R23	weak	repo/internal/codegen/cstable/fixedlineage.go:47	"FixedLineageEntry { Wire; Layout; Digest; Record; Retired; Reason }" member order	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.2/§5.9 #19,#15 static-data member names and layout_hash last; generator-side, no running cs assertion
cs	R24	owed	-	-	make tables-cs-leg -> "cs tables test passed"	open-PR #971	§4.5 fix 11 ill-formed text refuses by name (text_ill_formed); fixedbounds.go:124 "NOT THIS LEG'S YET", rides #971
cs	R25	weak	repo/test/cs-tables/src/FixedFormChecks.cs:422	"Check(bad < 0 && r5.Refused && r5.Reason == "plan_too_large", ...)"	make tables-cs-leg -> "cs tables test passed" (skipped)	merged-on-lane	§5.3 plan_too_large; runtime emits it, assertion lives in the §5.6-skipped forward-read test
cs	R26	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.3/§5.9 #36 a lineage entry that would not build -> named refusal, never a throw; no cs assertion
cs	R27	weak	repo/internal/codegen/cstable/fixedversioning_test.go:98	"if (back[0].X != 11 || back[0].Y != 22 || back[0].Z != 33) ... the old writer's values did not land"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.9 #32/#37 manifest is the oracle; cs reads the corpus bytes but hardcodes values, does not parse manifest.txt
cs	R28	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	queue row 4 / §5.8 row 6 guard chain (arm inside arm answers OUTER tag); cs has no guard2/arg2 lane
cs	R29	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.9 #27 band case (widen across 65536, clamp to writer's bounds); no cs assertion
cs	R30	owed	-	-	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	queue row 6 compressed float rides as the float; cfloat_res_refine/cfloat_range_widen rows owed
cs	R31	weak	repo/internal/codegen/cstable/fixedform_test.go:18	"TestFixedGuardComparedAtArgW ... "public byte ArgW;"" full-width guard + 64-bit ordinal temp	make tables-cs-leg -> "cs tables test passed"	merged-on-lane	§5.8 rows 5,7,14 full-width lanes; ArgW runtime assertion runs, ordinal-64 is generator-side
cs	R32	implemented-asserted	repo/internal/codegen/cstable/fixedversioning_test.go:562	"if (r.Reason != "layout_unsupported") ... a hash below the floor owes layout_unsupported"	make tables-cs-versioning -> "ok .../cstable 5.745s"	merged-on-lane	§5.6 retire for real: refused by name (floor probes); idempotent retire is S3's

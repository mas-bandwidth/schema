;; STRICT 100, the Java leg's audit (2026-11-08, main e254a5cba): every Java ✅ cell
;; whose cited evidence does not run its row under the one strict read today, with
;; a negative control, goes back to not-done here. Applied after every other
;; evidence file (it sorts last), so a later :done for one of these ids lands in
;; THIS file, beside the test that earns it.
(:id "java/R27" :state :todo :note "2026-11-08 audit: TestFixedRoadmapJavaT18OracleHostile/java/R27 fails on main with the fixed-form corpus present (SCHEMA_REQUIRE_CORPUS=1): hostile_range_widen.bin and hostile_range_widen_array.bin refuse out_of_range where the manifest lands one record each.")
(:id "vt/blobs/java" :state :todo :note "2026-11-08 audit: the cited TestJavaBlobSharedReportCensus is skipped on main (a node record no pointer reaches is not refused by the Java load), and the conformance wire and cook-write surfaces exit on blob_shared (main CI 42459510328, conformance (java)).")
(:id "java/per-value-shared-marker" :state :todo :note "2026-11-08 audit: the cited TestJavaBlobSharedReportCensus is skipped on main; TestJavaWritersRefuseASharedBlob alone has no negative control.")
(:id "vt/wire-fuzzer/java" :state :todo :note "2026-11-08 audit: the evidence is the 2026-11-02 nightly, run before the strict cut against the tolerant read; under the strict oracle make tables-java-wire-fuzz is red on main (negative controls (java-wire-fuzz), main CI 42459510328: read acceptance differs after 416 mutants).")
(:id "vt/retain-unknown/java" :state :todo :note "2026-11-08 audit: the evidence is the 2026-11-02 nightly, run before the strict cut; the conformance retain surface is 0/8 on main (the Java driver prints the retired two-field outcome, main CI 42459510328).")
(:id "vt/cards/java" :state :todo :note "2026-11-08 audit: make cards-java-test does not compile on main: tables/cards/java (Cards.java, Driver.java, CardsTest.java) reads the retired report fields malformed, kindMismatch and clamped.")
(:id "vt/cook-write/java" :state :todo :note "2026-11-08 audit: the cited conformance cook-write surface is 0/142 on main (the driver exits: blob_shared cook input did not load, main CI 42459510328).")
(:id "vt/text-form/java" :state :todo :note "2026-11-08 audit: the cited conformance json-hostile surface is 97/155 on main (the Java driver writes no canonical write-back wire, main CI 42459510328).")
(:id "vt/fixed-class/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main (the driver exits on blob_shared) and the report surface is red (main CI 42459510328).")
(:id "vt/defaults-and-table-rename/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main and the report surface is red (main CI 42459510328).")
(:id "vt/maps/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main (main CI 42459510328); framing map_L1_inert reads where the strict verdict refuses.")
(:id "vt/renames/java" :state :todo :note "2026-11-08 audit: the cited conformance wire and report surfaces are red on main (main CI 42459510328).")
(:id "vt/union-arms-any/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main (main CI 42459510328).")
(:id "vt/union-table-arms/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main (main CI 42459510328).")
(:id "vt/wide-scalars/java" :state :todo :note "2026-11-08 audit: the cited conformance wire and report surfaces are red on main (main CI 42459510328).")
(:id "vt/wstring/java" :state :todo :note "2026-11-08 audit: the cited conformance wire surface is 0/121 on main (main CI 42459510328).")
(:id "vt/widening/java" :state :todo :note "2026-11-08 audit: TestWideningReasonsJava keeps a widened list element the strict verdict refuses, and the cited conformance report surface is red on main (list_scalar_widen: got read, want refused; main CI 42459510328).")

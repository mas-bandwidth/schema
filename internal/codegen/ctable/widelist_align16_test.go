package ctable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWidelistExtentAlign16C pins the two wire-fuzz failures found on
// widelist_wide (widelist.Wide) at 949ea720287bdd6a17200a8bc7a82cc56b73f437:
//
//  1. corpus seed widelist_wide (widelist.Wide), pass reference #1, run seed 1
//     (sha256 11e2e71d936ef21ea45934556237cf6c0de18c66f11c61671164840bfc83ceda):
//     Wide is 40 bytes (8 mod 16) and wides []uint128 has element alignment 16
//     (ir.ListElementLayout), while C serialize_uint128_t is a pair of uint64_t
//     with SCHEMA_TABLE_ALIGNOF == 8. Starting wire_storage / region carving at
//     sizeof(Wide) (40) instead of table_align_up64(sizeof(Wide)) (48) and
//     reserving wides at alignment 8 instead of 16 caused wide_load_measure to
//     answer 208 region bytes where the framing commands 224.
//  2. corpus seed widelist_wide (widelist.Wide), pass widen #0, run seed 1
//     (sha256 7a71d97ba02cf1031bf721d0809d3f373f9c91cd44883d8201b29f79cbaa8ef6):
//     wides []uint128 widened from wire_kind 6 (uint8, 1 byte per element) must
//     lower the framing floor in wire_extent from 16 to 1; gating the widening
//     floor table on kind <= tkU64 excluded tkU128 (19) and caused
//     wide_load_measure to refuse the framing with -1 instead of 224.
func TestWidelistExtentAlign16C(t *testing.T) {
	t.Parallel()
	ser, ok := cSerializeDir(t)
	if !ok {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("the sibling runtime serialize.c is absent at %s", ser)
		}
		t.Skipf("named as not run: serialize.c is not staged at %s", ser)
	}
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "widelist", "Wides.schema"))
	if err != nil {
		t.Fatal(err)
	}
	refMutant, err := hex.DecodeString("01051102020e32130301000000000000000000000000000000ffffffffffffffffffffffffffffffff00000000000000000100000000000000030e1a0903010000000000000002000000000000000300000000000000040c39010536020e32130307000000000000000000000000000000ffffffffffffffffffffffffffffff7f00000000000000000000000000000000000023d79807ef564cd95b8ea02f93dea0db80856f07ef3b1cd9ffffffffffffffffbcc2428f1af3b2c10500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	widenMutant, err := hex.DecodeString("01011102020e05060301ff00030e1a0903010000000000000002000000000000000300000000000000040c39010536020e32130307000000000000000000000000000000ffffffffffffffffffffffffffffff7f00000000000000000000000000000000000023d79807ef564cd95b8ea02f93dea0db80856f07ef3b1cd9ffffffffffffffffbcc2428f1af3b2c10500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	var cRef, cWiden strings.Builder
	for i, b := range refMutant {
		if i > 0 {
			cRef.WriteString(",")
		}
		fmt.Fprintf(&cRef, "0x%02x", b)
	}
	for i, b := range widenMutant {
		if i > 0 {
			cWiden.WriteString(",")
		}
		fmt.Fprintf(&cWiden, "0x%02x", b)
	}
	mainSrc := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
%s
static const uint8_t ref_mutant[] = { %s };
static const uint8_t widen_mutant[] = { %s };

int main(void)
{
    SCHEMA_C_ALIGN16 uint8_t region[224];
    uint8_t retain_bytes[4096];
    TableRetainId retain_ids[64];
    TableRetain retain;
    TableReport report;
    const Wide * root;
    const serialize_uint128_t * elem;

    /* 1. Reference #1 mutant: framing commands exactly 224 region bytes. */
    CHECK(wide_load_measure(ref_mutant, (int64_t)sizeof(ref_mutant)) == 224);
    memset(&report, 0, sizeof(report));
    root = wide_load(region, (int64_t)sizeof(region), ref_mutant, (int64_t)sizeof(ref_mutant), &report);
    CHECK(root != NULL);
    CHECK(!report.refused && report.unknown == 1);
    elem = wide_wides_at(&root->wides, 0);
    CHECK(elem != NULL && (((uintptr_t)elem) & 15u) == 0u);

    memset(&report, 0, sizeof(report));
    memset(&retain, 0, sizeof(retain));
    retain.bytes = retain_bytes;
    retain.capacity = (int64_t)sizeof(retain_bytes);
    retain.ids = retain_ids;
    retain.id_capacity = 64;
    root = wide_load_retain(region, (int64_t)sizeof(region), ref_mutant, (int64_t)sizeof(ref_mutant), &retain, &report);
    CHECK(root != NULL);
    CHECK(!report.refused && report.unknown == 1 && report.retain_lost == 1);

    /* 2. Widen #0 mutant: uint8 -> uint128 list widening commands 224 bytes. */
    CHECK(wide_load_measure(widen_mutant, (int64_t)sizeof(widen_mutant)) == 224);
    memset(&report, 0, sizeof(report));
    root = wide_load(region, (int64_t)sizeof(region), widen_mutant, (int64_t)sizeof(widen_mutant), &report);
    CHECK(root != NULL);
    CHECK(!report.refused && report.widened == 1);
    elem = wide_wides_at(&root->wides, 1);
    CHECK(elem != NULL && elem->lo == 255u && elem->hi == 0u);
    return 0;
}
`, t09Check, cRef.String(), cWiden.String())
	runCGenerated(t, string(schemaBytes), mainSrc, append(identityCCFlags(), "-I", ser)...)
}

// TestUnplacedBlobMarkDoesNotCountBlobSharedC pins the arms_gate_text random
// mutant (seed 1, after 32,051 mutants; sha256
// 78affc19702f64b42f100d36929e39c0d87f15bbbc9ee88b9f589c7c50d06173) where
// Gate.reach = text(index=2) appears twice in the root body while node 2 has an
// unknown wire type id (0x704be0ecfaaffc58, offset UINT64_MAX). A repeated
// field id in one body is damage under the one strict read
// (docs/SPEC-TABLES.md §4, §6.5), so the whole load is refused and no blob
// marking is ever reached; before the strict read landed, the load kept the
// value and this pin held table_node_blob_mark to not counting blob_shared on
// the duplicate field overwrite.
func TestUnplacedBlobMarkDoesNotCountBlobSharedC(t *testing.T) {
	t.Parallel()
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "arms", "Gate.schema"))
	if err != nil {
		t.Fatal(err)
	}
	mutant, err := hex.DecodeString("01010f02110102010f02110102030406000000040c0601050361726d0058b8de05f3a11d897e409519eff404fae9ea716f0f0182bfffffffffffffffff58fcaffaece04b700500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	var cMut strings.Builder
	for i, b := range mutant {
		if i > 0 {
			cMut.WriteString(",")
		}
		fmt.Fprintf(&cMut, "0x%02x", b)
	}
	mainSrc := fmt.Sprintf(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
%s
static const uint8_t gate_mutant[] = { %s };

int main(void)
{
    SCHEMA_C_ALIGN16 uint8_t region[56];
    TableReport report;
    const Gate * root;

    CHECK(gate_load_measure(gate_mutant, (int64_t)sizeof(gate_mutant)) == 56);
    memset(&report, 0, sizeof(report));
    root = gate_load(region, (int64_t)sizeof(region), gate_mutant, (int64_t)sizeof(gate_mutant), &report);
    /* the repeated reach field is damage: the one strict read refuses the
       whole load and lands nothing (§4, §6.5) */
    CHECK(root == NULL);
    CHECK(report.refused);
    return 0;
}
`, t09Check, cMut.String())
	runCGenerated(t, string(schemaBytes), mainSrc, identityCCFlags()...)
}

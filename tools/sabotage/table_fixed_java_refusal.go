// THE VERSIONING GATE'S NEGATIVE CONTROL ON THE JAVA LEG (§5.3 of
// docs/FIXED-FORM-ALGORITHM.md).
//
// A FILE OUTSIDE THE LINEAGE is the one verdict §5.3 settles before any record
// is touched: LOAD matches the file's hash against the lock's entries and, when
// nothing matches, refuses by the name `layoutNewer` carrying that hash — the
// one answer that tells the operator to ship the reader rather than to suspect
// the bytes. The name is the whole of the answer, so a leg that refuses by some
// other name has lost the gate's only output while refusing just as loudly.
//
// The edit below puts `noLayout` there, which is the name for a file that
// carries NO layout at all: every count in the report still reads refused, and
// `make tables-java-versioning` must go RED on the NAME — the OLD-REFUSES-NEW
// column of every row and the `hash_unknown` case — and say which name it
// wanted.
//
// IT REPLACES THE RETIRED `plan_too_large` CONTROL, which watched the
// grow-and-retry cap in lineagePlans. That cap is gone: the plan is now sized
// from the lock's own entry counts by a stated formula (§5.9 #21), so there is
// no cap left to red, and the control moved here with its case — the gate still
// has one sabotage that proves it is watching §5.3's verdict.
package main

func init() {
	sabotages["fixed-form-java-refusal-name"] = []edit{{
		old: "if (pick < 0) { report.refuseHash(TableFixed.Reason.layoutNewer, fileHash); return -1; }",
		new: "if (pick < 0) { report.refuseHash(TableFixed.Reason.noLayout, fileHash); return -1; } // SABOTAGED: the wrong name for a file outside the lineage",
	}}
}

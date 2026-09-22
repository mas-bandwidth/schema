// F9 — batch_too_large
// This test ensures that when the caller capacity is exceeded by the data being
// loaded, the fixed-table loader returns -1 and reports BatchTooLarge.
// It uses the bench FixedTable API directly from the repository's generated
// bench code.

import { FixedTable, InitFixedTable } from "../../../../generated/bench/paired/js/BenchTable.js";
import { FixedTableFixedLoad, FixedTableFixedNewPlan, FixedTableFixedMeasure, FixedTableFixedSave } from "../../../../generated/bench/paired/js/FixedTableTable.js";
import { TableFixedReport, TableFixedRefusal } from "../../../../generated/bench/paired/js/BenchTable.js";

// Build a single fixed-table value (identity). We only need a valid body to
// produce a well-formed file with one record.
const v = new FixedTable();
InitFixedTable(v);

// Prepare a single-record file using the known constant layout measure.
const oneBuf = new Uint8Array(FixedTableFixedMeasure(1));
FixedTableFixedSave([v], 1, oneBuf);

// Now load with capacity 0 to trigger BatchTooLarge.
const plan = FixedTableFixedNewPlan(0);
const values = [new FixedTable()];
const report = new TableFixedReport();
const n = FixedTableFixedLoad(values, 0, oneBuf, oneBuf.length, plan, report);
const ok = (n === -1) && (report.refused === TableFixedRefusal.BatchTooLarge);
console.log(ok ? "PASS: batch_too_large" : `FAIL: batch_too_large (n=${n}, refused=${report.refused})`);
process.exit(ok ? 0 : 1);

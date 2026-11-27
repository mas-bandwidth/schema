// THE tblfixedevo UNIT ADAPTER (test/conformance/lobster/hooks.lobster): the
// read-into row ri_fixed_lineage reads EvoRoot through its generated batch
// load, tblfixedevo.EvoRootFixedLoad, into the caller values this batch holds:
// the v3 record, then a lawful v1 record read through the lineage into the
// SAME values (docs/SPEC-TABLES.md §3.4: a field v1 does not supply holds its
// declared default).

import .conformance_hooks
import .FixedEvoTable

class EvoRootBatch : FixedBatch
    values:[tblfixedevo.EvoRoot] = []
    def fresh(count:int) -> void:
        truncate(values, 0)
        for(count):
            push(values, tblfixedevo.NewEvoRoot())
    def load_into(data:string, report:schema_rt.TableReport) -> int:
        let plan:[schema_rt.TableFixedEntry] = []
        for(tblfixedevo.EvoRootFixedPlanMax):
            push(plan, schema_rt.TableFixedEntry {})
        return tblfixedevo.EvoRootFixedLoad(values, data, plan, report)
    // EvoRoot has no generated record bridge, so each value is saved as one
    // record under this build's own layout (every byte of the value, the
    // fields an older writer lacked included) and that record read back into
    // a Record of the root for the text writer
    def texts() -> [string]:
        let out:[string] = []
        for(values) v:
            let saved = tblfixedevo.EvoRootFixedSave([v])
            if saved == "":
                push(out, "unwritable fixed save\n")
            else:
                let rep = schema_rt.LoadReport {}
                let rec = schema_rt.load("EvoRoot", saved, rep)
                if rec:
                    let js, why = schema_rt.json_write(rec)
                    push(out, if why == "": js else: "unwritable " + why + "\n")
                else:
                    push(out, "unreadable fixed save\n")
        return out

register_fixed_batch(EvoRootBatch { root: "EvoRoot" })

// THE tblsurf UNIT ADAPTER (test/conformance/lobster/hooks.lobster): the read-into
// fixed rows of test/tables/surfaces read Narrow through its generated batch
// load, surfaces.NarrowFixedLoad, into the caller values this batch holds. The
// load takes the caller values and their count is its capacity, so it has no
// call without one (the fixed-nocap row answers unrepresentable in main.lobster).

import .conformance_hooks
import .SurfacesTable

class NarrowBatch : FixedBatch
    values:[surfaces.Narrow] = []
    def fresh(count:int) -> void:
        truncate(values, 0)
        for(count):
            push(values, surfaces.NewNarrow())
    def load_into(data:string, report:schema_rt.TableReport) -> int:
        let plan:[schema_rt.TableFixedEntry] = []
        for(surfaces.NarrowFixedPlanMax):
            push(plan, schema_rt.TableFixedEntry {})
        return surfaces.NarrowFixedLoad(values, data, plan, report)
    // each value is read out field by field into a Record of the root, the
    // way the generated NarrowFixedRecordCodec bridges it, and written by
    // the text writer
    def texts() -> [string]:
        let out:[string] = []
        for(values) v:
            let rec = schema_rt.new_record(schema_rt.table_of("Narrow"))
            schema_rt.slot_named(rec, "hp").cell.i = v.Hp
            let js, why = schema_rt.json_write(rec)
            push(out, if why == "": js else: "unwritable " + why + "\n")
        return out

register_fixed_batch(NarrowBatch { root: "Narrow" })

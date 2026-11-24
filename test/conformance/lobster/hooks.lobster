// THE UNIT HOOKS of the Lobster conformance program (test/conformance/lobster/driver
// stages this file into every generated unit as conformance_hooks.lobster).
//
// The fixed form's batch load is generated per root (<Root>FixedLoad, over the
// root's own native class), so only a unit that declares the root can name it.
// A unit adapter, test/conformance/lobster/units/<unit>.lobster, staged as
// conformance_unit.lobster, registers one FixedBatch per root it serves, and
// main.lobster finds it by the root's name. A root no adapter serves has no
// batch, and its read-into row says so.

import .load
import .json
import .fixedevolve

// FixedBatch holds one set of caller values of a root and hands them to the
// root's generated batch load (read-into, test/conformance/README.md).
class FixedBatch:
    root:string
    // fresh makes count values at their declared defaults
    def fresh(count:int) -> void:
        assert false
    // load_into is the generated load of data into the values the batch
    // holds, with the one report: its record count, or its refusal (-1)
    def load_into(data:string, report:schema_rt.TableReport) -> int:
        assert false
        return -1
    // texts is the ToJson text of every value the batch holds, in order
    def texts() -> [string]:
        assert false
        return []

let fixed_batches:[FixedBatch] = []

def register_fixed_batch(b:FixedBatch):
    push(fixed_batches, b)

def fixed_batch(root:string) -> FixedBatch?:
    for(fixed_batches) b:
        if b.root == root:
            return b
    return nil

// variable_class.lobster — the variable class's join to the per-kind hook
// table (docs/SPEC-TABLES.md §2.1, §2.6, §2.9, §3.1; docs/PORT-LOBSTER.md,
// THE LOAD). The walk itself is load.lobster's, and the writer is
// schema_save.lobster's: this file registers, AFTER the load's own
// registration, every shape key the variable class rides:
//
//   ptr    a *T node index into the flat node table (wire index 0 is null
//          and 1 the root, so index k + 2 names nodes[k], §3.1); a *string
//          and a *bytes blob share the key through shape_key
//   union  a union and the arm header it reads (§2.6); the table-body arm,
//          the pointer arm, the array arm and the payload-free arm are all
//          load's arm_read, which this registration keeps
//   tab    a table or packet type by value (§3)
//   arr    an array, including an array of pointers and an array of unions
//          (an array of pointers is a pointer slot per element, §2.1)
//
// The last registration of a key wins, so this file is where a later change
// to a variable-class shape lands without editing load.lobster.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// variable_class_keys is the shape table this file carries, in the order the
// card names the constructs.
let variable_class_keys:[string] = ["ptr", "union", "tab", "arr"]

// variable_class_carries answers whether this file registers a key; the load
// test asks it so a shape that stops being carried is named rather than
// silently fallen through to the scalar reader.
def variable_class_carries(key:string) -> bool:
    for(variable_class_keys) k:
        if k == key:
            return true
    return false

// Each registration re-publishes the walk's shape under its key. Registering
// the same Shape object again is how this card joins without editing the
// load: the key table now names this file as its last writer.
let _variable_class_pointer = register_shape(shape("ptr"))
let _variable_class_union = register_shape(shape("union"))
let _variable_class_table = register_shape(shape("tab"))
let _variable_class_array = register_shape(shape("arr"))

// wire_retain.goose — RETAIN-UNKNOWN on the table wire (docs/SPEC-TABLES.md §6.6).
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .wire_load;
import .wire_save;

struct RetainStore {
    capacity: i64,
    id_capacity: i64,
}

fn retain_tail(c: SaveCtx&, rec: i64) {}

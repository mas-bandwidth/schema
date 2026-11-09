// The text glue for a unit that has no units/<unit>.goose: the shell driver
// copies it beside that unit's generated modules as unit.goose. It names no
// root, so every one of the unit's json-hostile rows reads through the flat
// value table (main.goose, surface_json_hostile).
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&, wire: u8[>..<]&) -> bool {
    return false;
}

// The text glue for a unit that has no units/<unit>.goose: the shell driver
// copies it beside that unit's generated modules as unit.goose. It carries no
// root, so every one of the unit's text cases is ABSENT.
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) -> bool {
    return false;
}

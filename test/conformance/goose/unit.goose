// The glue main.goose imports when no unit is copied beside it (`list`, and
// the leg's own strict check): it carries no root, so every case is ABSENT.
// The shell driver puts a unit's own glue, test/conformance/goose/units/
// <unit>.goose, in its place beside that unit's generated modules.
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) -> bool {
    return false;
}

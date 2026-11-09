// THE tabledemo UNIT's glue for the Goose conformance leg (main.goose; the
// shell driver copies this file beside the unit's generated modules as
// unit.goose): which roots of tables/examples answer the text surfaces, and
// through which generated calls. A root this file does not name is ABSENT.
// The tabledemo unit answers its json-hostile rows through the flat value
// table (main.goose, surface_json_hostile, conformance_json_verdict), which
// writes both the §16 text and the canonical write-back wire. It names no root
// of its own: the flat value table carries every table root.
fn unit_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&, wire: u8[>..<]&) -> bool {
    return false;
}

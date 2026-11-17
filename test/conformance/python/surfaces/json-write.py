"""Write canonical text from each committed wire and test write refusals."""

from _common import absent, entries, root_class, write_bytes
from _json_common import raise_if_unsupported, read_wire


def run(manifest, outdir):
    for row in entries(manifest, "instance"):
        case, unit, root, wire = row[1:5]
        if len(row) > 5 and row[5] == "no-text":
            continue
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case + ".json", "unit not generated for python")
            continue
        value = read_wire(cls, wire)
        write_bytes(outdir, case + ".json", value.to_json().encode("utf-8"))
    for row in entries(manifest, "json-write-refusal"):
        case, unit, root, wire, damage = row[1:6]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case + ".json", "unit not generated for python")
            continue
        value = read_wire(cls, wire)
        if damage != "none":
            form, name = damage.split(":", 1)
            if form != "narrow":
                raise ValueError("unsupported text damage: " + damage)
            setattr(value, name, "\udcff")
        try:
            text = value.to_json().encode("utf-8")
        except ValueError as error:
            raise_if_unsupported(error)
            text = b"refused\n"
        write_bytes(outdir, case + ".json", text)

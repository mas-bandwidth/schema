"""Read each committed table text and write its canonical wire."""

from _common import absent, entries, read_bytes, root_class, unit_package, write_bytes
from _json_common import write_wire


def run(manifest, outdir):
    for row in entries(manifest, "instance"):
        case, unit, root = row[1:4]
        if len(row) > 5 and row[5] == "no-text":
            continue
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        text = read_bytes("testdata/conformance/tables/json/" + case + ".json").decode("utf-8")
        try:
            value = cls.from_json(text)
        except unit_package(unit).Refused as err:
            # Every committed text reads. A refusal is the runtime's answer for
            # this ONE case: it is written as that case's answer (a red row
            # naming the refusal), never a crash of the whole surface.
            write_bytes(outdir, case, ("refused " + err.reason + "\n").encode("utf-8"))
            continue
        write_bytes(outdir, case, write_wire(cls, value))

"""Read each committed table text and write its canonical wire."""

from _common import answering, no_codec, entries, read_bytes, root_class, write_bytes
from _json_common import write_wire


def run(manifest, outdir):
    for row in entries(manifest, "instance"):
        with answering(row[1]):
            case, unit, root = row[1:4]
            if len(row) > 5 and row[5] == "no-text":
                continue
            cls = root_class(unit, root)
            if cls is None:
                no_codec(outdir, case)
                continue
            text = read_bytes("testdata/conformance/tables/json/" + case + ".json").decode("utf-8")
            write_bytes(outdir, case, write_wire(cls, cls.from_json(text)))

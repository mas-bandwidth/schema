"""Read each hostile text as one whole value or a refusal."""

from _common import absent, entries, read_bytes, root_class, write_bytes
from _json_common import answer, raise_if_unsupported


def run(manifest, outdir):
    for row in entries(manifest, "json-hostile"):
        case, unit, root, tree = row[1:5]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        try:
            text = read_bytes(tree + "/" + root + ".json").decode("utf-8")
            value = cls.from_json(text)
        except ValueError as error:
            raise_if_unsupported(error)
            write_bytes(outdir, case, b"refused\n")
            continue
        answer(outdir, case, cls, value)

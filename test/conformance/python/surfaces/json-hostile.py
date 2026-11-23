"""Read each hostile text as one whole value or a refusal."""

from _common import answering, no_codec, entries, read_bytes, root_class, write_bytes
from _json_common import answer, raise_if_unsupported


def run(manifest, outdir):
    for row in entries(manifest, "json-hostile"):
        with answering(row[1]):
            case, unit, root, tree = row[1:5]
            cls = root_class(unit, root)
            if cls is None:
                no_codec(outdir, case)
                continue
            try:
                text = read_bytes(tree + "/" + root + ".json").decode("utf-8")
                value = cls.from_json(text)
            except ValueError as error:
                raise_if_unsupported(error)
                write_bytes(outdir, case, b"refused\n")
                continue
            answer(outdir, case, cls, value)

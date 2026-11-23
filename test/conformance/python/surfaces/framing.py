"""Read framing vectors strictly and report the whole accepted value."""

import os

from _common import answering, no_codec, ROOT, entries, root_class, write_bytes
from _json_common import raise_if_unsupported, read_wire, write_wire


def run(manifest, outdir):
    for row in entries(manifest, "framing"):
        with answering(row[1]):
            case, unit, root, wire = row[1:5]
            # Read form 1 through the variable-class sibling of each fixed root
            # on the framing surface. Save the value with the original fixed class.
            if unit == "tblr1" and root == "Cfg":
                tree = os.environ.get("PYTHON_FRAMING_GENERATED", os.path.join(ROOT, "tblr1", "_framing"))
            elif unit == "listdemo" and root == "Bounded":
                tree = os.path.join(ROOT, "listdemo", "_framing")
            elif unit == "tabledemo" and root == "CountedMin":
                tree = os.path.join(ROOT, "tabledemo", "_framing")
            else:
                tree = None
            cls = root_class(unit, root, tree)
            if cls is None:
                no_codec(outdir, case)
                continue
            try:
                value = read_wire(cls, wire)
            except ValueError as error:
                raise_if_unsupported(error)
                write_bytes(outdir, case, b"refused\n")
                continue
            text = value.to_json()
            if tree is not None:
                fixed_cls = root_class(unit, root)
                if fixed_cls is None:
                    no_codec(outdir, case)
                    continue
                saved = fixed_cls.from_json(text).save()
            else:
                saved = write_wire(cls, value)
            write_bytes(outdir, case + ".wire", saved)
            write_bytes(outdir, case, b"read\n" if "no-value" in row[5:] else b"read\n" + text.encode("utf-8"))

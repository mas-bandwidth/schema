"""Exercise the framing adapter with real generated classes and wire vectors."""

import importlib.util
from pathlib import Path
import sys
import tempfile

root = Path(__file__).resolve().parent
sys.path.insert(0, str(root / "surfaces"))
source = (root / "surfaces" / "framing.py").read_text(encoding="utf-8")
negative = len(sys.argv) == 2 and sys.argv[1] == "--negative"
if negative:
    old = 'write_bytes(outdir, case, b"refused\\n")'
    assert source.count(old) == 1
    source = source.replace(old, 'write_bytes(outdir, case, b"read\\n")')

with tempfile.TemporaryDirectory() as temp:
    temp = Path(temp)
    adapter = temp / "framing.py"
    adapter.write_text(source, encoding="utf-8")
    spec = importlib.util.spec_from_file_location("framing_probe", adapter)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    manifest = temp / "manifest.txt"
    manifest.write_text(
        "framing value tblr1 Cfg testdata/wire/tables/framing/frame_empty_table.bin read -\n"
        "framing no_value tblr1 Cfg testdata/wire/tables/framing/frame_empty_table.bin read - no-value\n"
        "framing unused tblr1 Cfg testdata/wire/tables/framing/frame_unreferenced_entry.bin refused -\n",
        encoding="utf-8",
    )
    module.run(str(manifest), str(temp))
    refused = (temp / "unused").read_bytes()
    if negative:
        assert refused != b"refused\n", "the planted refusal fault escaped"
    else:
        assert refused == b"refused\n", refused
        assert (temp / "no_value").read_bytes() == b"read\n"
        assert (temp / "value").read_bytes().startswith(b"read\n{")
        value_wire = (temp / "value.wire").read_bytes()
        assert value_wire and value_wire == (temp / "no_value.wire").read_bytes()
print("framing adapter fault detected" if negative else "framing adapter passed")

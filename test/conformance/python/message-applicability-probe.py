"""Prove the no-message roster row and a healthy fixed message root."""

import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import tempfile

root = Path(__file__).resolve().parent
sys.path.insert(0, str(root / "surfaces"))
import _common  # noqa: E402
from _common import unit_package  # noqa: E402

package = unit_package("graphdemo")
assert package is not None
message = package._schema._message_unit
fixed = package.Settings
vocab = message.MessageVocabulary()
message.announce_read(vocab, message.ANNOUNCEMENT)
wire = message.save_messages(fixed, [fixed()])
assert len(message.load_messages(fixed, vocab, wire)) == 1

spec = importlib.util.spec_from_file_location("message_probe", root / "surfaces" / "message.py")
assert spec is not None and spec.loader is not None
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
with tempfile.TemporaryDirectory() as temp:
    temp = Path(temp)
    manifest = temp / "manifest"
    manifest.write_text(
        "connection graph_conn graphdemo 0x2e0dc8d1c2bff1ae testdata/wire/tables/graph_conn_current.bin\n"
        "message graph_tree graph_conn Scene testdata/wire/tables/graph_tree.bin -\n",
        encoding="utf-8",
    )
    if "--negative" in sys.argv[1:]:
        message.ROOTS = message.ROOTS | {"Scene"}
        errors = io.StringIO()
        with contextlib.redirect_stderr(errors):
            module.run(str(manifest), str(temp))
        # a fault in one case is a DRIVER ERROR: the case is named, no answer is written, the surface exits non-zero
        assert _common.DRIVER_ERRORS == ["graph_tree"], "the planted message-root fault escaped"
        assert "unexpectedly carries" in errors.getvalue(), errors.getvalue()
        assert not (temp / "graph_tree").exists() and not (temp / "graph_tree.absent").exists()
        print("message applicability fault detected")
    else:
        module.run(str(manifest), str(temp))
        assert (temp / "graph_tree").read_bytes() == b"refused\n"
        assert not (temp / "graph_tree.absent").exists()
        print("message applicability passed")

"""Card conformance driver: one manifest, one surface, one output directory."""
from __future__ import annotations

import importlib.util
import os
from pathlib import Path
import sys

sys.path.insert(0, os.environ.get("CARDS_PYTHON_GENERATED", "build/cards-python"))

spec = importlib.util.spec_from_file_location("card_facade", os.environ.get("CARDS_PYTHON_FACADE", "tables/cards/python/facade.py"))
assert spec is not None and spec.loader is not None
facade = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = facade
spec.loader.exec_module(facade)

SURFACES = ("load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view")


def report_text(report: object) -> str:
    return (f"Retained={report.retained}\nRetainLost={report.retain_lost}\n"
            f"Widened={report.widened}\nUnknown={report.unknown}\n"
            "KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed=false\n")


def view() -> str:
    from cards import UnitView

    rows = []
    for table in sorted(UnitView().tables, key=lambda row: row.name):
        for fd in table.type.fields:
            shape = "list" if fd.array_kind == 3 else "blob" if fd.codec == "string_blob" else "value"
            bound = str(fd.size) if shape == "value" and fd.type_name == "string" and fd.size else "-"
            rows.append(f"{table.name} {fd.name} {fd.type_name} {shape} {bound}\n")
    return "".join(rows)


def one(surface: str, row: list[str], output: Path) -> None:
    name, path = row[1:3]
    target = output / name
    if surface == "view":
        (output / "view.txt").write_bytes(view().encode("utf-8"))
        return
    data = Path(path).read_bytes()
    try:
        if surface == "load":
            record = facade.load(data)
            target.with_suffix(".json").write_bytes(facade.to_json(record).encode("utf-8"))
            target.with_suffix(".report").write_bytes(report_text(record.report).encode("utf-8"))
        elif surface in ("save", "bounds"):
            target.with_suffix(".wire").write_bytes(facade.from_json(data.decode("utf-8")).wire)
        elif surface == "render":
            answer = facade.render(facade.load(data))
            target.with_suffix(".txt").write_bytes(answer.encode("utf-8"))
        elif surface == "parse":
            target.with_suffix(".wire").write_bytes(facade.parse(data.decode("utf-8")).wire)
        elif surface == "upgrade":
            wire, report = facade.upgrade(data)
            target.with_suffix(".wire").write_bytes(wire)
            target.with_suffix(".report").write_bytes(report_text(report).encode("utf-8"))
        elif surface == "edit":
            record = facade.load(data)
            if len(row) != 4 or "=" not in row[3]:
                raise facade.CardError("malformed")
            key, value = row[3].split("=", 1)
            setters = {"paths": facade.set_paths, "needs": facade.set_needs,
                       "tier": facade.set_tier, "priority": facade.set_priority,
                       "column": facade.set_column, "holder": facade.set_holder}
            if key not in setters:
                raise facade.CardError("malformed")
            setters[key](record, value.split(",") if key in ("paths", "needs") and value else [] if key in ("paths", "needs") else value)
            target.with_suffix(".wire").write_bytes(record.wire)
        elif surface == "taskhash":
            target.with_suffix(".hex").write_bytes(facade.task_hash(facade.load(data)).encode("ascii"))
    except (facade.CardError, UnicodeError) as error:
        if surface in ("bounds", "save", "parse", "upgrade", "edit"):
            reason = error.reason if isinstance(error, facade.CardError) else "malformed"
            target.with_suffix(".refused").write_bytes((reason + "\n").encode("utf-8"))
        else:
            raise


def main() -> int:
    if len(sys.argv) < 3:
        return 2
    manifest, surface = sys.argv[1:3]
    if surface == "list":
        print("\n".join(SURFACES))
        return 0
    if surface not in SURFACES or len(sys.argv) != 4:
        return 2
    output = Path(sys.argv[3])
    for line in Path(manifest).read_text(encoding="utf-8").splitlines():
        if line and not line.startswith("#"):
            row = line.split()
            if row and row[0] == surface:
                one(surface, row, output)
    return 0

if __name__ == "__main__":
    raise SystemExit(main())

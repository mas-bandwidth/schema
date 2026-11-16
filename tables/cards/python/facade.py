"""Owning card facade over the generated Card table.

Reads keep their original wire, edits publish only after validation, and the
customer limits apply before any replacement is written.
"""
from __future__ import annotations

import hashlib
import re
import struct
from dataclasses import dataclass
from typing import Any, cast

from cards import BlobRef, Card, CardID, Column, InvalidValue, KeyValue, LoadReport, PathText, Priority, Refused, Tier

MAX_BODY_BYTES = 65536
MAX_LIST_ENTRIES = 512
MAX_RETAIN_BYTES = 1 << 20
MAX_RETAIN_IDS = 4096

_TIERS = {"flash": 1, "pro": 2, "heavy": 3, "frontier": 4}
_PRIORITIES = {"normal": 1, "high": 2, "critical": 3, "blocker": 4}
_COLUMNS = {"waiting": 1, "ready": 2, "working": 3, "review": 4, "merging": 5, "landed": 6, "plan": 7, "check": 8}
_RESERVED = {"RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW", "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE"}
_STRANDED = {"KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON", "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"}
_HEADER = re.compile(r"^([A-Za-z][A-Za-z0-9-]*):[ \t\f\r]*(.*)$")

class CardError(ValueError):
    """A card refusal and its portable conformance class."""
    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason

@dataclass
class Record:
    wire: bytes
    root: Card
    report: LoadReport


def _text(value: str, bound: int | None = None) -> None:
    try:
        data = value.encode("utf-8")
    except (UnicodeError, AttributeError):
        raise CardError("malformed") from None
    if "\0" in value or (bound is not None and len(data) > bound):
        raise CardError("malformed")


def _blob_text(value: str | BlobRef | None) -> str | None:
    payload = value.value if isinstance(value, BlobRef) else value
    if payload is not None and not isinstance(payload, str):
        raise CardError("malformed")
    return payload


def _check(root: Card) -> None:
    body = _blob_text(root.body)
    if body is not None:
        _text(body)
        if len(body.encode("utf-8")) > MAX_BODY_BYTES:
            raise CardError("body-bound 65536")
    for name in ("paths", "_new", "shared", "needs", "extra", "consumers"):
        seq = getattr(root, name)
        if len(seq) > MAX_LIST_ENTRIES:
            raise CardError(f"list-bound {name} 512")
    for name in ("paths", "_new", "shared", "needs"):
        for item in getattr(root, name):
            _text(item.s, 64 if name == "needs" else 192)
    for raw in root.extra:
        item = cast(KeyValue, raw)
        _text(item.key, 32)
        if not re.fullmatch(r"[A-Za-z][A-Za-z0-9-]*", item.key) or item.key in _RESERVED:
            raise CardError("malformed")
        value = _blob_text(item.value)
        if value is not None:
            _text(value)
            if "\n" in value or "\r" in value:
                raise CardError("malformed")
    for item in root.consumers:
        for name in ("kind", "who", "route", "model", "outcome", "line"):
            _text(getattr(item, name))


def _save(root: Card) -> bytes:
    _check(root)
    try:
        return root.save()
    except (InvalidValue, ValueError, TypeError) as exc:
        raise CardError("malformed") from exc


def load(wire: bytes) -> Record:
    report = LoadReport()
    try:
        root = Card.load(wire, report)
        _check(root)
        return Record(bytes(wire), root, report)
    except Refused as exc:
        raise CardError("malformed") from exc


def read_strict(wire: bytes) -> Record:
    """Load a card only when no unrecognized data would be lost."""
    record = load(wire)
    if record.report.unknown or record.report.retain_lost:
        raise CardError("unknown-present")
    return record


def from_json(text: str) -> Record:
    try:
        report = LoadReport()
        root = Card.from_json(text, report)
        if report.unknown:
            raise CardError("malformed")
        return Record(_save(root), root, report)
    except (Refused, InvalidValue) as exc:
        raise CardError("malformed") from exc


def to_json(record: Record) -> str:
    try:
        return record.root.to_json()
    except (Refused, InvalidValue) as exc:
        raise CardError("malformed") from exc


def _row(cls: type[Any], value: str) -> Any:
    obj = cls()
    obj.s = value
    return obj


def parse(text: str) -> Record:
    _text(text)
    parts = text.split("\n")
    lines = [part + "\n" for part in parts[:-1]]
    if parts[-1]:
        lines.append(parts[-1])
    if not lines:
        raise CardError("malformed")
    first = _HEADER.fullmatch(lines[0].rstrip("\r\n"))
    if first is None or first.group(1) != "RESULT":
        raise CardError("malformed")
    value = first.group(2)
    try:
        rest, tier = value.rsplit(" tier:", 1)
        ident, sha = rest.rsplit(" sha=", 1)
    except ValueError:
        raise CardError("malformed") from None
    ident, tier = ident.strip(), tier.strip()
    _text(ident, 64)
    if not re.fullmatch(r"(?:[0-9a-fA-F]{12}|[0-9a-fA-F]{40})", sha) or tier not in _TIERS:
        raise CardError("malformed")
    root = Card()
    root.id, root.sha, root.tier = ident, sha, _TIERS[tier]
    seen = {"RESULT"}
    body_at = len(lines[0])
    offset = body_at
    prose = False
    strings = {"REPO": ("repo", 96), "BASE": ("_base", 96), "KIND": ("kind", 24), "TEST": ("test", 192)}
    lists = {"PATHS": ("paths", PathText, 192), "NEW": ("_new", PathText, 192), "SHARED": ("shared", PathText, 192), "DEPENDS-ON": ("needs", CardID, 64)}
    for line in lines[1:]:
        content = line.rstrip("\r\n")
        end = offset + len(line)
        offset = end
        if not content.strip(" \t\r"):
            continue
        header = _HEADER.fullmatch(content)
        if header is None:
            prose = True
            continue
        key, val = header.groups()
        if prose:
            if key in _STRANDED:
                raise CardError("stranded-key " + key)
            continue
        if key in _RESERVED:
            if key in seen:
                raise CardError("malformed")
            seen.add(key)
        if key == "tier":
            if val.strip() != tier:
                raise CardError("malformed")
        elif key in strings:
            name, bound = strings[key]
            _text(val, bound)
            setattr(root, name, val)
        elif key == "PRIORITY":
            if val.strip() not in _PRIORITIES:
                raise CardError("malformed")
            root.priority = _PRIORITIES[val.strip()]
        elif key in lists:
            name, cls, bound = lists[key]
            value = val.strip()
            parts = [] if value in ("", "-") or (key == "PATHS" and value == "none") else [p.strip() for p in value.split(",") if p.strip()]
            if len(parts) > MAX_LIST_ENTRIES:
                raise CardError("malformed")
            for part in parts:
                _text(part, bound)
            setattr(root, name, [_row(cls, part) for part in parts])
        elif key == "DONE-WHEN":
            root.done = val or None
        elif key == "SOURCE":
            root.source = val or None
        elif key == "DEADLINE":
            m = re.fullmatch(r"finish within ([0-9]+) minutes\.", val.strip())
            if m is None or int(m.group(1)) > 100000:
                raise CardError("malformed")
            root.deadline_minutes = int(m.group(1))
        elif key not in _RESERVED:
            _text(key, 32)
            item = KeyValue()
            item.key, item.value = key, val
            root.extra.append(item)
        body_at = end
    root.body = text[body_at:] or None
    return Record(_save(root), root, LoadReport())


def render(record: Record) -> str:
    root = load(record.wire).root
    tier = Tier.NAMES.get(root.tier)
    if tier is None or root.tier == 0:
        raise CardError("malformed")
    out = [f"RESULT: {root.id} sha={root.sha} tier: {tier}\n"]
    def line(key: str, value: str) -> None:
        out.append(f"{key}: {value}\n")
    for key, name in (("REPO", "repo"), ("BASE", "_base"), ("KIND", "kind")):
        value = getattr(root, name)
        if value:
            line(key, value)
    if root.priority:
        line("PRIORITY", Priority.NAMES[root.priority])
    for key, name in (("PATHS", "paths"), ("NEW", "_new"), ("SHARED", "shared"), ("DEPENDS-ON", "needs")):
        values = getattr(root, name)
        if values:
            line(key, ", ".join(x.s for x in values))
    if root.test:
        line("TEST", root.test)
    done = _blob_text(root.done)
    if done is not None:
        line("DONE-WHEN", done)
    if root.deadline_minutes:
        line("DEADLINE", f"finish within {root.deadline_minutes} minutes.")
    source = _blob_text(root.source)
    if source is not None:
        line("SOURCE", source)
    for raw in root.extra:
        item = cast(KeyValue, raw)
        line(item.key, _blob_text(item.value) or "")
    body = _blob_text(root.body)
    if body is not None:
        out.append(body)
    return "".join(out)


def _frame(value: str) -> bytes:
    data = value.encode("utf-8")
    return struct.pack("<Q", len(data)) + data


def task_hash(record: Record) -> str:
    if record.report.unknown or record.report.retain_lost:
        raise CardError("unknown-present")
    root = record.root
    data = _frame(_blob_text(root.body) or "")
    for name in ("paths", "_new", "shared"):
        items = getattr(root, name)
        data += struct.pack("<Q", len(items)) + b"".join(_frame(item.s) for item in items)
    data += _frame(root.test) + _frame(_blob_text(root.done) or "")
    return hashlib.sha256(data).hexdigest()


def edit(record: Record, name: str, value: str | list[str]) -> None:
    if record.report.unknown or record.report.retain_lost:
        raise CardError("unknown-present")
    root = load(record.wire).root
    if name in ("paths", "needs"):
        values = value.split(",") if isinstance(value, str) and value else value if isinstance(value, list) else []
        if len(values) > MAX_LIST_ENTRIES:
            raise CardError(f"list-bound {name} 512")
        for part in values:
            _text(part, 192 if name == "paths" else 64)
        setattr(root, name, [_row(PathText if name == "paths" else CardID, part) for part in values])
    elif name in ("tier", "priority", "column"):
        if not isinstance(value, str):
            raise CardError("malformed")
        variants = {"tier": _TIERS, "priority": _PRIORITIES, "column": _COLUMNS}[name]
        if value not in variants:
            raise CardError("malformed")
        setattr(root, name, variants[value])
    elif name == "holder":
        if not isinstance(value, str):
            raise CardError("malformed")
        _text(value, 32)
        root.holder = value
    else:
        raise CardError("malformed")
    wire = _save(root)
    record.root, record.wire = root, wire


def set_paths(record: Record, values: list[str]) -> None:
    edit(record, "paths", values)


def set_needs(record: Record, values: list[str]) -> None:
    edit(record, "needs", values)


def set_tier(record: Record, value: str) -> None:
    edit(record, "tier", value)


def set_priority(record: Record, value: str) -> None:
    edit(record, "priority", value)


def set_column(record: Record, value: str) -> None:
    edit(record, "column", value)


def set_holder(record: Record, value: str) -> None:
    edit(record, "holder", value)


def upgrade(wire: bytes) -> tuple[bytes, LoadReport]:
    """Preserve unknown fields while upgrading the known card declaration."""
    from cards import TableRetain
    retain = TableRetain(bytearray(MAX_RETAIN_BYTES), bytearray(MAX_RETAIN_IDS * 24))
    report = LoadReport()
    try:
        root = Card.load_retain(wire, retain, report)
        _check(root)
        if report.retain_lost:
            raise CardError("retain-lost")
        output = root.save_retain(retain, report)
        if report.retain_lost:
            raise CardError("retain-lost")
        return (output, report)
    except (Refused, InvalidValue) as exc:
        raise CardError("malformed") from exc

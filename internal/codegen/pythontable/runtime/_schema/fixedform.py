"""Form 3: baked plans over fixed record images.

The generated _fixed module supplies every layout, record size, schema and
operation. A file selects a known hash and compares bytes; it never supplies
an executable layout. All records are checked before any value is built.
"""

import math
import struct
from typing import Any, TypeVar

from . import InvalidValue, LoadReport, Refused
from . import descriptor
from .load import buffer_view

_T = TypeVar("_T")
_Spec = dict[str, Any]
_ROOTS: dict[str, _Spec] = {}


class FixedLoadReport(LoadReport):
    """The ordinary success-only counters plus the selected layout hash."""

    __slots__ = ("layout_hash",)
    layout_hash: int

    def reset(self) -> None:
        super().reset()
        self.layout_hash = 0


def _integer(data: bytes | bytearray | memoryview, at: int, width: int, signed: bool = False) -> int:
    return int.from_bytes(data[at:at + width], "little", signed=signed)


def _store(out: bytearray, at: int, width: int, value: int, signed: bool = False) -> None:
    out[at:at + width] = value.to_bytes(width, "little", signed=signed)


def _dead(data: bytes | bytearray | memoryview, begin: int, end: int) -> None:
    if any(data[begin:end]):
        raise Refused("wire_damaged", "nonzero bytes outside the live extent")


def _bool(data: bytes | bytearray | memoryview, at: int) -> bool:
    value = data[at]
    if value > 1:
        raise Refused("wire_damaged", "a bool or presence byte is not 0 or 1")
    return value == 1


def _text(raw: bytes) -> str:
    try:
        value = raw.decode("utf-8", "strict")
    except UnicodeError:
        raise Refused("text_ill_formed") from None
    if "\0" in value:
        raise Refused("text_ill_formed")
    return value


def _default(spec: _Spec, current: Any = None) -> Any:
    shape = spec["shape"]
    if shape == "optional":
        return None
    if shape == "table":
        value = descriptor.new_by_name(spec["target"])
        for field in spec["children"]:
            setattr(value, field["name"], _default(field, getattr(value, field["name"])))
        return value
    if shape == "union":
        return descriptor.new_union_by_name(spec["target"])
    if shape in ("array", "keyed"):
        n = spec.get("minimum", 0) if spec.get("counted", False) else spec["bound"]
        values = [_default(spec["children"][0]) for _ in range(n)]
        if shape == "keyed":
            current._values = values
            return current
        return values
    return spec.get("default", 0)


def _encode(spec: _Spec, value: Any, out: bytearray, at: int, path: str, check: bool = True) -> None:
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional":
        if value is not None:
            out[at] = 1
            _encode(spec["children"][0], value, out, at + 1, path, check)
        return
    if shape == "table":
        cls = descriptor.by_name(spec["target"])
        if cls is None or not isinstance(value, cls):
            raise InvalidValue(path, "not a " + spec["target"])
        cursor = at
        for field in spec["children"]:
            name = field["name"]
            _encode(field, getattr(value, name), out, cursor, path + "." + name, check)
            cursor += field["size"]
        return
    if shape in ("array", "keyed"):
        if shape == "keyed":
            try:
                values = value._values
            except AttributeError:
                raise InvalidValue(path, "not a keyed array") from None
        else:
            values = value
        if type(values) is not list:
            raise InvalidValue(path, "not a list")
        count = len(values)
        counted = spec.get("counted", False)
        if counted:
            if count < spec.get("minimum", 0) or count > spec["bound"]:
                raise InvalidValue(path, "count outside its declared bound")
            _store(out, at, 4, count)
            at += 4
        elif count != spec["bound"]:
            raise InvalidValue(path, "wrong fixed array length")
        elem = spec["children"][0]
        for index, item in enumerate(values):
            _encode(elem, item, out, at + index * elem["size"], f"{path}[{index}]", check)
        return
    if shape == "union":
        cls = descriptor._union_by_name(spec["target"])
        if cls is None or not isinstance(value, cls):
            raise InvalidValue(path, "not a " + spec["target"])
        tag = value.type
        arms = spec["children"]
        if type(tag) is not int or tag < 0 or tag > len(arms):
            raise InvalidValue(path, "union tag has no arm")
        _store(out, at, spec["tag"], tag)
        if tag:
            arm = arms[tag - 1]
            if arm["shape"] != "void":
                _encode(arm, getattr(value, arm["name"]), out, at + spec["tag"], path + "." + arm["name"], check)
        return
    if shape == "bool":
        if type(value) is not bool:
            raise InvalidValue(path, "not a bool")
        out[at] = 1 if value else 0
    elif shape in ("int", "enum"):
        if type(value) is not int:
            raise InvalidValue(path, "not an int")
        lo, hi = spec.get("lo", 0), spec.get("hi", len(spec.get("variants", [])))
        if value < lo or value > hi or (check and value in spec.get("deprecated", [])):
            raise InvalidValue(path, "integer outside its declared range or set")
        _store(out, at, size, value, spec.get("signed", False))
    elif shape == "float":
        if type(value) is not float or not math.isfinite(value):
            raise InvalidValue(path, "not a finite float")
        if spec.get("ranged", False) and value == 0:
            value = 0.0
        try:
            encoded = struct.pack("<f" if size == 4 else "<d", value)
        except (OverflowError, struct.error):
            raise InvalidValue(path, "float overflows its storage") from None
        stored = struct.unpack("<f" if size == 4 else "<d", encoded)[0]
        if not math.isfinite(stored):
            raise InvalidValue(path, "float overflows its storage")
        if spec.get("ranged", False) and not spec["lo"] <= stored <= spec["hi"]:
            raise InvalidValue(path, "float outside its declared range")
        out[at:at + size] = encoded
    elif shape in ("string", "bytes"):
        if shape == "string":
            if type(value) is not str or "\0" in value:
                raise InvalidValue(path, "not NUL-free text")
            try:
                raw = value.encode("utf-8", "strict")
            except UnicodeError:
                raise InvalidValue(path, "text is not UTF-8") from None
        else:
            if not isinstance(value, (bytes, bytearray, memoryview)) or isinstance(value, memoryview) and value.format != "B":
                raise InvalidValue(path, "not bytes or a B-format buffer")
            raw = bytes(value)
        if len(raw) > spec["bound"]:
            raise InvalidValue(path, "length outside its declared bound")
        _store(out, at, 4, len(raw))
        out[at + 4:at + 4 + len(raw)] = raw
    elif shape != "void":
        raise InvalidValue(path, "unsupported fixed value")


def _seed(spec: _Spec, out: bytearray, at: int) -> None:
    """Write a declaration's default image without constructing user values."""
    shape = spec["shape"]
    if shape == "table":
        for field in spec["children"]:
            _seed(field, out, at)
            at += field["size"]
    elif shape in ("array", "keyed"):
        count = spec.get("minimum", 0) if spec.get("counted", False) else spec["bound"]
        if spec.get("counted", False):
            _store(out, at, 4, count)
            at += 4
        elem = spec["children"][0]
        for index in range(count):
            _seed(elem, out, at + index * elem["size"])
    elif shape not in ("optional", "union", "void"):
        _encode(spec, spec.get("default", 0), out, at, "default", False)


def _source_spec(node: _Spec, reader: _Spec, ranges: _Spec) -> _Spec:
    """Resolve the lock's check descriptors once, outside the load path."""
    result = dict(reader)
    result["size"] = node["size"]
    result["kind"] = node["kind"]
    shape = reader["shape"]
    if shape == "optional" and node["kind"] != 35:
        return _source_spec(node, reader["children"][0], ranges)
    if shape == "table":
        named = {field["id"]: field for field in reader["children"]}
        result["children"] = [_source_spec(child, named[child["id"]], ranges) for child in node["children"]]
    elif shape == "optional":
        result["children"] = [_source_spec(node["children"][0], reader["children"][0], ranges)]
    elif shape in ("array", "keyed"):
        elem = node["children"][1 if shape == "keyed" else 0]
        result["children"] = [_source_spec(elem, reader["children"][0], ranges)]
        result["bound"] = (node["size"] - (4 if reader.get("counted", False) else 0)) // elem["size"]
    elif shape == "union":
        result["children"] = [_source_spec(child, reader["children"][index], ranges) for index, child in enumerate(node["children"])]
        result["tag"] = node["size"] - max((child["size"] for child in node["children"]), default=0)
    elif shape == "enum":
        result["variants"] = [child["id"] for child in node["children"]]
    elif shape == "int":
        kind = node["kind"]
        result["signed"] = 2 <= kind <= 5 or kind == 18 or 20 <= kind <= 24
        bits = node["size"] * 8
        result["lo"] = -(1 << (bits - 1)) if result["signed"] else 0
        result["hi"] = (1 << (bits - (1 if result["signed"] else 0))) - 1
        if reader.get("name", "") in ranges:
            result["lo"], result["hi"] = ranges[reader["name"]]
    elif shape in ("string", "bytes"):
        result["bound"] = node["size"] - 4
    return result


def _check(spec: _Spec, data: memoryview, at: int) -> None:
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional":
        if _bool(data, at):
            _check(spec["children"][0], data, at + 1)
        else:
            _dead(data, at + 1, at + size)
    elif shape == "table":
        for field in spec["children"]:
            _check(field, data, at)
            at += field["size"]
    elif shape in ("array", "keyed"):
        elem = spec["children"][0]
        count = spec["bound"]
        if spec.get("counted", False):
            count = _integer(data, at, 4, True)
            if count < spec.get("minimum", 0) or count > spec["bound"]:
                raise Refused("count_over_bound")
            _dead(data, at + 4 + count * elem["size"], at + size)
            at += 4
        for index in range(count):
            _check(elem, data, at + index * elem["size"])
    elif shape == "union":
        tag = _integer(data, at, spec["tag"])
        arms = spec["children"]
        if tag > len(arms):
            raise Refused("out_of_range")
        used = spec["tag"]
        if tag:
            arm = arms[tag - 1]
            used += arm["size"]
            _check(arm, data, at + spec["tag"])
        _dead(data, at + used, at + size)
    elif shape == "bool":
        _bool(data, at)
    elif shape in ("int", "enum"):
        value = _integer(data, at, size, spec.get("signed", False))
        if value < spec.get("lo", 0) or value > spec.get("hi", len(spec.get("variants", []))):
            raise Refused("out_of_range")
        if shape == "enum" and value > len(spec["variants"]):
            raise Refused("out_of_range")
    elif shape == "float":
        raw = _integer(data, at, size)
        exponent = 0x7F800000 if size == 4 else 0x7FF0000000000000
        if raw & exponent == exponent:
            raise Refused("non_finite_float")
        value = struct.unpack_from("<f" if size == 4 else "<d", data, at)[0]
        if spec.get("ranged", False) and not spec["lo"] <= value <= spec["hi"]:
            raise Refused("out_of_range")
    elif shape in ("string", "bytes"):
        count = _integer(data, at, 4, True)
        if count < 0 or count > spec["bound"]:
            raise Refused("length_over_bound")
        _dead(data, at + 4 + count, at + size)
        if shape == "string":
            _text(bytes(data[at + 4:at + 4 + count]))


def _decode(spec: _Spec, data: bytes | bytearray | memoryview, at: int, current: Any = None) -> Any:
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional":
        return _decode(spec["children"][0], data, at + 1) if data[at] else None
    if shape == "table":
        value = descriptor.new_by_name(spec["target"])
        for field in spec["children"]:
            name = field["name"]
            setattr(value, name, _decode(field, data, at, getattr(value, name)))
            at += field["size"]
        return value
    if shape in ("array", "keyed"):
        n = spec["bound"]
        if spec.get("counted", False):
            n = _integer(data, at, 4)
            at += 4
        elem = spec["children"][0]
        values = [_decode(elem, data, at + index * elem["size"]) for index in range(n)]
        if shape == "keyed":
            current._values = values
            return current
        return values
    if shape == "union":
        value = descriptor.new_union_by_name(spec["target"])
        value.type = _integer(data, at, spec["tag"])
        if value.type:
            arm = spec["children"][value.type - 1]
            if arm["shape"] != "void":
                setattr(value, arm["name"], _decode(arm, data, at + spec["tag"]))
        return value
    if shape == "bool":
        return data[at] == 1
    if shape in ("int", "enum"):
        return _integer(data, at, size, spec.get("signed", False))
    if shape == "float":
        result: float = struct.unpack_from("<f" if size == 4 else "<d", data, at)[0]
        return 0.0 if spec.get("ranged", False) and result == 0 else result
    if shape in ("string", "bytes"):
        n = _integer(data, at, 4)
        raw = bytes(data[at + 4:at + 4 + n])
        return _text(raw) if shape == "string" else raw
    return None


def _guarded(op: _Spec, source: memoryview) -> bool:
    for at, width, value, mode in op["guards"]:
        actual = _integer(source, at, width)
        if mode == "eq" and actual != value or mode == "gt" and actual <= value:
            return False
    return True


def _apply(op: _Spec, source: memoryview, image: bytearray) -> int:
    src, dst, size = op["src"], op["dst"], op["size"]
    kind = op["op"]
    if kind == "copy":
        image[dst:dst + size] = source[src:src + size]
    elif kind == "widen":
        _store(image, dst, op["width"], _integer(source, src, size, op["signed"]), op["signed"])
        return 1
    elif kind == "widenf":
        struct.pack_into("<d", image, dst, struct.unpack_from("<f", source, src)[0])
        return 1
    elif kind == "const":
        _store(image, dst, size, op["value"])
    elif kind == "count":
        image[dst:dst + 4] = source[src:src + 4]
    elif kind == "text":
        image[dst:dst + 4 + size] = source[src:src + 4 + size]
    elif kind == "ordinal":
        ordinal = _integer(source, src, size)
        _store(image, dst, op["width"], op["remap"][ordinal])
        return int(size < op["width"])
    return 0


def fixed_register(roots: dict[str, _Spec]) -> None:
    """Bind generated immutable facts once after the unit's classes exist."""
    for name, root in roots.items():
        root["layout"] = bytes.fromhex(root["layout"])
        root["prefix"] = root["hash"].to_bytes(8, "little")
        root["header"] = b"\x03" + bytes(7) + root["prefix"] + len(root["layout"]).to_bytes(4, "little") + root["layout"]
        reset = bytearray(root["body"])
        _seed(root["spec"], reset, 0)
        root["reset"] = bytes(reset)
        known = {}
        for entry in root["known"]:
            entry["layout"] = bytes.fromhex(entry["layout"])
            if not entry["refusal"]:
                entry["check"] = entry.get("check_spec") or (root["spec"] if entry["hash"] == root["hash"] else _source_spec(entry["source"], root["spec"], entry["ranges"]))
                for fill in entry["fills"]:
                    image = bytearray(fill["size"])
                    _seed(fill["spec"], image, 0)
                    fill["image"] = bytes(image)
                cover = bytearray(root["body"])
                for op in entry["plan"]:
                    count = op["size"]
                    if op["op"] in ("widen", "widenf", "ordinal"):
                        count = op["width"]
                    elif op["op"] == "count":
                        count = 4
                    elif op["op"] == "text":
                        count += 4
                    cover[op["dst"]:op["dst"] + count] = bytes([1]) * count
                holes = []
                at = 0
                while at < len(cover):
                    if cover[at]:
                        at += 1
                        continue
                    end = at + 1
                    while end < len(cover) and not cover[end]:
                        end += 1
                    holes.append((at, end))
                    at = end
                entry["holes"] = holes
                entry["split"] = next((index for index, op in enumerate(entry["plan"]) if op["guards"]), len(entry["plan"]))
            known[entry["hash"]] = entry
        root["known"] = known
        cls = descriptor.by_name(name)
        if cls is not None:
            cls.fixed_plan_max = root["plan_max"]
        _ROOTS[name] = root


def _root(cls: type[Any], writing: bool = False) -> _Spec:
    root = _ROOTS.get(cls.__name__)
    if root is None:
        if writing:
            raise InvalidValue(cls.__name__, "layout_unsupported")
        raise Refused("layout_unsupported")
    return root


def fixed_measure(cls: type[Any], count: int) -> int:
    root = _root(cls, True)
    if type(count) is not int or count < 1:
        raise InvalidValue(cls.__name__, "a fixed file holds at least one record")
    return int(len(root["header"]) + count * (root["body"] + 8))


def fixed_save(cls: type[_T], values: list[_T]) -> bytes:
    root = _root(cls, True)
    if type(values) is not list or not values:
        raise InvalidValue(cls.__name__, "a fixed file holds at least one record")
    # Each record starts as its own zeroed image. Only live values are stored;
    # the bytes behind counts, lengths, tags and clear flags remain zero.
    records = []
    for index, value in enumerate(values):
        if not isinstance(value, cls):
            raise InvalidValue(f"[{index}]", "not a " + cls.__name__)
        image = bytearray(root["body"])
        _encode(root["spec"], value, image, 0, f"[{index}]")
        records.append(root["prefix"] + image)
    return bytes(root["header"]) + b"".join(records)


def fixed_load(cls: type[_T], data: bytes | bytearray | memoryview, capacity: int = 4096, report: LoadReport | None = None, plan_capacity: int | None = None) -> list[_T]:
    source = buffer_view(data)
    if not source:
        raise Refused("truncated")
    if source[0] != 3:
        raise Refused("previous_form" if source[0] == 1 else "message_form_as_file" if source[0] == 2 else "newer_form")
    if len(source) < 20:
        raise Refused("truncated")
    if any(source[1:8]):
        raise Refused("reserved_not_zero")
    length = _integer(source, 16, 4)
    if length < 4 or 20 + length > len(source):
        raise Refused("layout_malformed")
    root = _root(cls)
    wire_hash = _integer(source, 8, 8)
    entry = root["known"].get(wire_hash)
    if entry is None:
        raise Refused("layout_unknown")
    if entry["refusal"] == "layout_unsupported":
        raise Refused("layout_unsupported")
    if source[20:20 + length] != entry["layout"]:
        raise Refused("layout_malformed")
    if entry["refusal"]:
        raise Refused(entry["refusal"])
    if plan_capacity is not None and (type(plan_capacity) is not int or plan_capacity < len(entry["plan"]) + len(entry["fills"])):
        raise Refused("plan_too_large")
    record = entry["record"]
    rest = len(source) - 20 - length
    if record <= 8 or rest <= 0 or rest % record:
        raise Refused("truncated")
    count = rest // record
    if type(capacity) is not int or capacity < 0 or count > capacity:
        raise Refused("batch_too_large")
    start = 20 + length
    # The check pass is total over the batch: no caller report or class
    # instance is touched before every hash, live value and dead byte passes.
    for index in range(count):
        at = start + index * record
        if _integer(source, at, 8) != wire_hash:
            raise Refused("no_layout")
        _check(entry["check"], source, at + 8)
    values = []
    widened = 0
    for index in range(count):
        at = start + index * record + 8
        body = source[at:at + record - 8]
        image = bytearray(root["body"])
        for begin, end in entry["holes"]:
            image[begin:end] = root["reset"][begin:end]
        for fill in entry["fills"]:
            if not fill["guards"] or _guarded(fill, body):
                image[fill["dst"]:fill["dst"] + fill["size"]] = fill["image"]
        plan = entry["plan"]
        split = entry["split"]
        for op in plan[:split]:
            widened += _apply(op, body, image)
        for op in plan[split:]:
            if _guarded(op, body):
                widened += _apply(op, body, image)
        values.append(_decode(root["spec"], image, 0))
    if report is not None:
        report.reset()
        report.widened = widened
        if isinstance(report, FixedLoadReport):
            report.layout_hash = wire_hash
    return values

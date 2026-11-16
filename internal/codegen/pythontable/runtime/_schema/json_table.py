"""Table text conversion over generated descriptors."""

from __future__ import annotations

import base64
import binascii
import copy
import datetime
import math
import re
import struct
from typing import Any, Callable, TypeVar

from . import BlobRef, InvalidValue, LoadReport, Refused
from .descriptor import FieldDesc, by_name, new_union_by_name
from .json_scan import Number, parse
from .kind_map import _entry_fields

_WIDE_INTEGER_KINDS = frozenset((5, 9, 18, 19))
_FIXED_KINDS = frozenset(range(20, 30))
_WIDE_FIXED_KINDS = frozenset((23, 24, 28, 29))
_DECIMAL = re.compile(r"(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?\Z")
_TIMESTAMP = re.compile(r"([0-9]{4})-([0-9]{2})-([0-9]{2})[Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]+))?([Zz]|[+-][0-9]{2}:[0-9]{2})\Z")
_EPOCH = datetime.datetime(1970, 1, 1)
_T = TypeVar("_T")
_Fill = Callable[[Any, dict[str, Any], dict[int, tuple[Any, bool]] | None, list[int]], None]


def _single(token: str, field: FieldDesc) -> float:
    match = _DECIMAL.fullmatch(token)
    if match is None:
        raise Refused("kind_mismatch", field.name)
    fraction = match.group(3) or ""
    exponent = match.group(4) or "0"
    digits = match.group(2) + fraction
    negative = bool(match.group(1))
    if not digits.strip("0"):
        return -0.0 if negative else 0.0
    exp_digits = exponent.lstrip("+-0")
    if len(exp_digits) > 6:
        if exponent.startswith("-"):
            return -0.0 if negative else 0.0
        raise Refused("out_of_range", field.name)
    exp = int(exp_digits or "0") * (-1 if exponent.startswith("-") else 1)
    point = len(match.group(2)) + exp
    trimmed = digits.lstrip("0")
    point -= len(digits) - len(trimmed)
    digits = trimmed.rstrip("0")
    if point > 40:
        raise Refused("out_of_range", field.name)
    if point < -60:
        return -0.0 if negative else 0.0
    if len(digits) > 200:
        tail = digits[200:]
        digits = digits[:200] + ("1" if tail.strip("0") else "")
    n = int(digits)
    scale = point - len(digits)
    d = 1
    if scale >= 0:
        n *= 10 ** scale
    else:
        d = 10 ** -scale
    exponent2 = n.bit_length() - d.bit_length()
    below = n < (d << exponent2) if exponent2 >= 0 else (n << -exponent2) < d
    if below:
        exponent2 -= 1
    shift = 149 if exponent2 < -126 else 23 - exponent2
    if shift >= 0:
        n <<= shift
    else:
        d <<= -shift
    mantissa, remainder = divmod(n, d)
    if remainder * 2 > d or (remainder * 2 == d and mantissa & 1):
        mantissa += 1
    if exponent2 >= -126:
        if mantissa >= 1 << 24:
            exponent2 += 1
            mantissa >>= 1
        if exponent2 > 127:
            raise Refused("out_of_range", field.name)
        bits = ((exponent2 + 127) << 23) | (mantissa & ((1 << 23) - 1))
    else:
        bits = mantissa
    if negative:
        bits |= 1 << 31
    return float(struct.unpack("<f", struct.pack("<I", bits))[0])


def _read_float(field: FieldDesc, token: Any) -> float:
    if not isinstance(token, Number):
        raise Refused("kind_mismatch", field.name)
    value = _single(token, field) if field.kind == 10 else float(token)
    if not math.isfinite(value):
        raise Refused("out_of_range", field.name)
    if getattr(field, "float_range", False):
        if value < getattr(field, "fmin") or value > getattr(field, "fmax"):
            raise Refused("out_of_range", field.name)
        if value == 0:
            value = 0.0
    return value


def _write_float(field: FieldDesc, value: Any) -> str:
    if type(value) is not float or not math.isfinite(value):
        raise InvalidValue(field.name, "not a finite float")
    single = field.kind == 10
    if single:
        try:
            value = struct.unpack("<f", struct.pack("<f", value))[0]
        except OverflowError:
            raise InvalidValue(field.name, "float32 overflow") from None
        if not math.isfinite(value):
            raise InvalidValue(field.name, "float32 overflow")
    if getattr(field, "float_range", False):
        if value < getattr(field, "fmin") or value > getattr(field, "fmax"):
            raise InvalidValue(field.name, "float is outside its declared range")
        if value == 0:
            value = 0.0
    low, high = (6, 9) if single else (15, 17)
    for precision in range(low, high + 1):
        text = format(value, f".{precision}g")
        if precision == high or (_single(text, field) if single else float(text)) == value:
            return text
    raise AssertionError("unreachable")


def _read_timestamp(field: FieldDesc, value: Any) -> int:
    if type(value) is not str:
        raise Refused("kind_mismatch", field.name)
    match = _TIMESTAMP.fullmatch(value)
    if match is None:
        raise Refused("timestamp_not_rfc3339", field.name)
    try:
        stamp = datetime.datetime(int(match.group(1)), int(match.group(2)), int(match.group(3)), int(match.group(4)), int(match.group(5)), int(match.group(6)))
    except ValueError:
        raise Refused("timestamp_not_rfc3339", field.name) from None
    offset = match.group(8)
    shift = 0
    if offset not in ("Z", "z"):
        hours, minutes = int(offset[1:3]), int(offset[4:6])
        if hours > 23 or minutes > 59:
            raise Refused("timestamp_not_rfc3339", field.name)
        shift = (hours * 3600 + minutes * 60) * (-1 if offset[0] == "-" else 1)
    delta = stamp - _EPOCH
    fraction = (match.group(7) or "")[:9].ljust(9, "0")
    nanos = (delta.days * 86400 + delta.seconds - shift) * 1000000000 + int(fraction)
    if nanos < -(1 << 63) or nanos > (1 << 63) - 1:
        raise Refused("timestamp_not_rfc3339", field.name)
    return nanos


def _write_timestamp(field: FieldDesc, value: Any) -> str:
    if type(value) is not int or value < -(1 << 63) or value > (1 << 63) - 1:
        raise InvalidValue(field.name, "timestamp outside int64")
    seconds, nanos = divmod(value, 1000000000)
    stamp = _EPOCH + datetime.timedelta(seconds=seconds)
    text = stamp.strftime("%Y-%m-%dT%H:%M:%S")
    if nanos:
        text += "." + str(nanos).rjust(9, "0").rstrip("0")
    return _quote(text + "Z")


def _read_fixed(field: FieldDesc, token: Any) -> int:
    if field.kind in _WIDE_FIXED_KINDS:
        if type(token) is not str:
            raise Refused("kind_mismatch", field.name)
    elif not isinstance(token, Number):
        raise Refused("kind_mismatch", field.name)
    match = _DECIMAL.fullmatch(token)
    if match is None:
        raise Refused("kind_mismatch", field.name)
    fraction = match.group(3) or ""
    exponent = match.group(4) or "0"
    digits = match.group(2) + fraction
    if not digits.strip("0"):
        if field.lo > 0 or field.hi < 0:
            raise Refused("out_of_range", field.name)
        return 0
    exp_digits = exponent.lstrip("+-0")
    if len(exp_digits) > 6:
        raise Refused("kind_mismatch" if exponent.startswith("-") else "out_of_range", field.name)
    exp = int(exp_digits or "0") * (-1 if exponent.startswith("-") else 1)
    point = len(match.group(2)) + exp
    stripped = digits.lstrip("0")
    point -= len(digits) - len(stripped)
    digits = stripped.rstrip("0")
    if point > 40:
        raise Refused("out_of_range", field.name)
    if point < -40 or len(digits) > 200:
        raise Refused("kind_mismatch", field.name)
    raw: int = int(digits) << field.frac_bits
    scale = point - len(digits)
    if scale >= 0:
        raw *= 10 ** scale
    else:
        raw, remainder = divmod(raw, 10 ** -scale)
        if remainder:
            raise Refused("kind_mismatch", field.name)
    if match.group(1):
        raw = -raw
    if raw < field.lo or raw > field.hi:
        raise Refused("out_of_range", field.name)
    return raw


def _format_fixed(value: int, frac: int) -> str:
    negative = value < 0
    magnitude = abs(value)
    whole, rest = divmod(magnitude, 1 << frac)
    if rest:
        digits = str(rest * 5 ** frac).rjust(frac, "0").rstrip("0")
    else:
        digits = "0"
    return ("-" if negative else "") + str(whole) + "." + digits


def _read_integer(field: FieldDesc, token: Any) -> int:
    if field.kind in _WIDE_INTEGER_KINDS:
        if type(token) is not str:
            raise Refused("kind_mismatch", field.name)
    elif not isinstance(token, Number):
        raise Refused("kind_mismatch", field.name)
    if not token or token == "-0" or (token[0] == "-" and len(token) == 1):
        raise Refused("kind_mismatch", field.name)
    digits = token[1:] if token[0] == "-" else token
    if not digits or (len(digits) > 1 and digits[0] == "0") or any(c < "0" or c > "9" for c in digits):
        raise Refused("kind_mismatch", field.name)
    # Bound the conversion before Python's host-specific decimal digit limit.
    if len(digits) > 39:
        raise Refused("out_of_range", field.name)
    value = int(token)
    if value < field.lo or value > field.hi:
        raise Refused("out_of_range", field.name)
    return value


def _read_field(field: FieldDesc, value: Any, graph: dict[int, tuple[Any, bool]] | None, unknown: list[int]) -> Any:
    if field.codec in ("string_blob", "bytes_blob"):
        if value is None:
            return None
        blob = copy.copy(field)
        blob.codec = "string" if field.codec == "string_blob" else "bytes"
        blob.size = 0xFFFFFFFF
        return _read_field(blob, value, graph, unknown)
    if field.codec == "optional":
        if value is None:
            return None
        return _read_field(_inner(field), value, graph, unknown)
    if field.kind in (10, 11) and field.codec not in ("array", "keyed", "map"):
        return _read_float(field, value)
    if field.codec == "union":
        if type(value) is not dict or len(value) > 1:
            raise Refused("kind_mismatch", field.name)
        union = new_union_by_name(field.target)
        if value:
            name, item = next(iter(value.items()))
            if "\x00" in name or len(name.encode("utf-8")) > 255:
                raise Refused("malformed")
            arm = next((arm for arm in union.descriptor.arms if arm.name == name), None)
            if arm is None:
                unknown[0] += 1
                _skip(item, graph)
            elif arm.field is None:
                if item is not None:
                    raise Refused("kind_mismatch", field.name)
                union.type = arm.tag
            else:
                union.type = arm.tag
                setattr(union, arm.name, _read_field(arm.field, item, graph, unknown))
        return union
    if field.json_flags and field.codec not in ("array", "pointer_array", "union_array", "map", "keyed"):
        if type(value) is not list:
            raise Refused("kind_mismatch", field.name)
        result_flags = 0
        for name in value:
            if type(name) is not str:
                raise Refused("kind_mismatch", field.name)
            if len(name.encode("utf-8")) > 255:
                raise Refused("length_over_bound", field.name)
            if name in field.json_variants:
                bit = 1 << field.json_variants.index(name)
                if result_flags & bit:
                    raise Refused("malformed", field.name)
                result_flags |= bit
            else:
                unknown[0] += 1
        return result_flags
    if field.codec == "enum":
        if type(value) is not str:
            raise Refused("kind_mismatch", field.name)
        if len(value.encode("utf-8")) > 255:
            raise Refused("length_over_bound", field.name)
        if value == "None":
            return 0
        if value not in field.json_variants:
            unknown[0] += 1
            return 0
        ordinal = field.json_variants.index(value) + 1
        return field.default if ordinal in field.deprecated else ordinal
    if field.codec == "map":
        if type(value) is not dict:
            raise Refused("kind_mismatch", field.name)
        key_field, value_field = _entry_fields(field)
        mapping: dict[Any, Any] = {}
        for key, item in value.items():
            if "\x00" in key:
                raise Refused("text_holds_zero", field.name)
            if key_field.codec == "string":
                if not key_field.unbounded and len(key.encode("utf-8")) > min(255, key_field.size):
                    raise Refused("length_over_bound", field.name)
                parsed_key: Any = key
            elif key_field.codec == "int":
                parsed_key = _read_integer(key_field, key if key_field.kind in _WIDE_INTEGER_KINDS else Number(key))
            else:
                raise Refused("unsupported", field.name)
            if parsed_key in mapping:
                raise Refused("malformed", field.name)
            if value_field.codec == "keyed":
                entry_cls = by_name(field.target)
                if entry_cls is None:
                    raise Refused("unsupported", field.name)
                container = getattr(entry_cls(), value_field.name)
                _read_keyed(value_field, item, container, graph, unknown)
                mapping[parsed_key] = container
            else:
                mapping[parsed_key] = _read_field(value_field, item, graph, unknown)
        return mapping
    if field.codec in ("array", "pointer_array", "union_array"):
        if type(value) is not list:
            raise Refused("kind_mismatch", field.name)
        element = _array_element(field)
        if field.array_kind != 3 and len(value) > field.bound:
            raise Refused("count_over_bound", field.name)
        if field.array_kind == 2 and len(value) < field.array_min:
            raise Refused("count_over_bound", field.name)
        if field.json_text_list:
            inner = copy.copy(element)
            inner.codec = "string"
            result = []
            spent = 0
            for item in value:
                text = None if element.codec == "text_optional" and item is None else _read_field(inner, item, graph, unknown)
                if text is not None:
                    spent += len(text.encode("utf-8"))
                if field.text_total and spent > field.text_total:
                    raise Refused("length_over_bound", field.name)
                result.append(text)
        else:
            result = [_read_field(element, item, graph, unknown) for item in value]
        if field.array_kind == 1:
            while len(result) < field.bound:
                if element.codec == "table":
                    cls = by_name(element.target)
                    if cls is None:
                        raise Refused("unsupported", field.name)
                    result.append(cls())
                elif element.codec == "pointer":
                    result.append(None)
                elif element.codec == "union":
                    result.append(new_union_by_name(element.target))
                elif element.codec == "bool":
                    result.append(False)
                elif element.kind in (10, 11):
                    result.append(0.0)
                elif element.codec in ("int", "enum"):
                    result.append(0)
                elif element.codec == "string":
                    result.append("")
                elif element.codec == "bytes":
                    result.append(b"")
                else:
                    raise Refused("unsupported", field.name)
        return result
    if field.codec in ("pointer", "table"):
        if field.codec == "pointer" and value is None:
            return None
        cls = by_name(field.target)
        if cls is None:
            raise Refused("kind_mismatch", field.name)
        if cls.descriptor.json_array and field.codec == "table":
            obj = cls()
            inner = obj.descriptor.fields[0]
            setattr(obj, inner.name, _read_field(inner, value, graph, unknown))
            return obj
        if type(value) is not dict:
            raise Refused("kind_mismatch", field.name)
        if field.codec == "pointer" and value and next(iter(value)) == "&node":
            if graph is None:
                raise Refused("malformed")
            label = value["&node"]
            if not isinstance(label, Number) or not label.isascii() or not label.isdigit() or label[0] == "0" or len(label) > 20 or int(label) > 18446744073709551615:
                raise Refused("malformed")
            number = int(label)
            if len(value) == 1:
                entry = graph.get(number)
                if entry is None or entry[1]:
                    raise Refused("malformed")
                if entry[0] is not None and not isinstance(entry[0], cls):
                    raise Refused("kind_mismatch", field.name)
                return entry[0]
            if number in graph:
                raise Refused("malformed")
            obj = cls()
            graph[number] = (obj, True)
            _fill(obj, {k: v for k, v in value.items() if k != "&node"}, graph, unknown)
            graph[number] = (obj, False)
            return obj
        obj = cls()
        _fill(obj, value, graph, unknown)
        return obj
    if field.codec == "bool":
        if type(value) is not bool:
            raise Refused("kind_mismatch", field.name)
        return value
    if field.codec == "int":
        if field.kind == 36:
            return _read_timestamp(field, value)
        if field.kind in _FIXED_KINDS:
            return _read_fixed(field, value)
        return _read_integer(field, value)
    if field.codec == "string":
        if type(value) is not str:
            raise Refused("kind_mismatch", field.name)
        if "\x00" in value:
            raise Refused("text_holds_zero", field.name)
        if not field.unbounded and len(value.encode("utf-8")) > field.size:
            raise Refused("length_over_bound", field.name)
        return value
    if field.codec == "bytes":
        if type(value) is not str:
            raise Refused("kind_mismatch", field.name)
        try:
            raw = base64.b64decode(value, validate=True)
        except (ValueError, binascii.Error):
            raise Refused("base64_damaged", field.name) from None
        if base64.b64encode(raw).decode("ascii") != value:
            raise Refused("base64_not_canonical", field.name)
        if len(raw) > field.size:
            raise Refused("length_over_bound", field.name)
        return raw
    raise Refused("unsupported", field.name)


def from_json(cls: type[_T], text: str, report: LoadReport | None = None, fill: _Fill | None = None) -> _T:
    """Read one value, or refuse without changing a caller's instance."""
    data = parse(text, None)
    value = cls()
    descriptor = getattr(value, "descriptor")
    graph: dict[int, tuple[Any, bool]] | None = {} if descriptor.variable else None
    unknown = [0]
    if descriptor.json_array:
        field = descriptor.fields[0]
        setattr(value, field.name, _read_field(field, data, graph, unknown))
    else:
        if type(data) is not dict:
            raise Refused("kind_mismatch")
        if fill is None:
            _fill(value, data, graph, unknown)
        else:
            fill(value, data, graph, unknown)
    if report is not None:
        report.unknown = unknown[0]
        report.widened = 0
        report.retained = 0
        report.retain_lost = 0
    return value


def _read_keyed(field: FieldDesc, item: Any, container: Any, graph: dict[int, tuple[Any, bool]] | None, unknown: list[int]) -> None:
    if type(item) is not dict or field.elem is None:
        raise Refused("kind_mismatch", field.name)
    slots = container if getattr(field, "keyed_list", False) else container._values
    for slot, current in item.items():
        if "\x00" in slot or len(slot.encode("utf-8")) > 255:
            raise Refused("malformed", field.name)
        if slot in field.json_keys:
            index = field.json_keys.index(slot)
            slots[index] = _read_field(field.elem, current, graph, unknown)
        else:
            unknown[0] += 1
            _skip(current, graph)


def _fill(value: Any, data: dict[str, Any], graph: dict[int, tuple[Any, bool]] | None, unknown: list[int]) -> None:
    fields = {field.json_key: field for field in value.descriptor.fields}
    for key, item in data.items():
        if "\x00" in key or len(key.encode("utf-8")) > 255 or key.startswith("&"):
            raise Refused("malformed")
        field = fields.get(key)
        if field is not None:
            if field.codec == "keyed":
                _read_keyed(field, item, getattr(value, field.name), graph, unknown)
            else:
                setattr(value, field.name, _read_field(field, item, graph, unknown))
        else:
            unknown[0] += 1
            _skip(item, graph)
    for field in value.descriptor.fields:
        if field.required and field.json_key not in data and _guard_holds(field, value):
            raise Refused("missing_required", field.name)


def _skip(value: Any, graph: dict[int, tuple[Any, bool]] | None) -> None:
    if type(value) is str:
        if "\x00" in value:
            raise Refused("malformed")
    elif type(value) is list:
        for item in value:
            _skip(item, graph)
    elif type(value) is dict:
        for index, (key, item) in enumerate(value.items()):
            if "\x00" in key or len(key.encode("utf-8")) > 255:
                raise Refused("malformed")
            if graph is not None and key.startswith("&"):
                if index != 0 or key != "&node" or not isinstance(item, Number) or not item.isascii() or not item.isdigit() or item[0] == "0" or len(item) > 20 or int(item) > 18446744073709551615:
                    raise Refused("malformed")
                graph.setdefault(int(item), (None, False))
            else:
                _skip(item, graph)


def _write_field(field: FieldDesc, value: Any) -> str:
    if field.codec in ("string_blob", "bytes_blob"):
        if value is None:
            return "null"
        if isinstance(value, BlobRef):
            value = value.value
        blob = copy.copy(field)
        blob.codec = "string" if field.codec == "string_blob" else "bytes"
        blob.size = 0xFFFFFFFF
        return _write_field(blob, value)
    if field.codec == "optional":
        return _write_field(_inner(field), value)
    if field.kind in (10, 11):
        return _write_float(field, value)
    if field.codec == "enum":
        if type(value) is not int or value < 0 or value > len(field.json_variants) or value in field.deprecated:
            raise InvalidValue(field.name, "enum value names no writable variant")
        return _quote("None" if value == 0 else field.json_variants[value - 1])
    if field.codec == "bool":
        if type(value) is not bool:
            raise InvalidValue(field.name, "not a bool")
        return "true" if value else "false"
    if field.codec == "int":
        if type(value) is not int or value < field.lo or value > field.hi:
            raise InvalidValue(field.name, "not an integer in range")
        if field.kind == 36:
            return _write_timestamp(field, value)
        if field.kind in _FIXED_KINDS:
            token = _format_fixed(value, field.frac_bits)
            return '"' + token + '"' if field.kind in _WIDE_FIXED_KINDS else token
        token = str(value)
        return '"' + token + '"' if field.kind in _WIDE_INTEGER_KINDS else token
    if field.codec == "string":
        if type(value) is not str or "\x00" in value:
            raise InvalidValue(field.name, "not a valid string")
        try:
            encoded = value.encode("utf-8")
        except UnicodeEncodeError:
            raise InvalidValue(field.name, "not UTF-8") from None
        if not field.unbounded and len(encoded) > field.size:
            raise InvalidValue(field.name, "string exceeds its bound")
        return _quote(value)
    if field.codec == "bytes":
        if not isinstance(value, (bytes, bytearray, memoryview)):
            raise InvalidValue(field.name, "not bytes")
        if isinstance(value, memoryview) and (value.format != "B" or value.ndim != 1):
            raise InvalidValue(field.name, "not a byte view")
        raw = bytes(value)
        if len(raw) > field.size:
            raise InvalidValue(field.name, "bytes exceed their bound")
        return _quote(base64.b64encode(raw).decode("ascii"))
    raise InvalidValue(field.name, "unsupported JSON field")


def to_json(value: Any) -> str:
    """Write a value in canonical text, or refuse an invalid value."""
    counts: dict[int, int] = {}
    _count(value, counts, {id(value)}, 0)
    return _write_table(value, 0, counts, {}, None) + "\n"


def _count(value: Any, counts: dict[int, int], opened: set[int], depth: int) -> None:
    if depth > 128:
        raise InvalidValue(value.descriptor.name, "nested past the depth cap")
    for field in value.descriptor.fields:
        if _guard_holds(field, value):
            _count_value(field, getattr(value, field.name), counts, opened, depth)


def _count_value(field: FieldDesc, item: Any, counts: dict[int, int], opened: set[int], depth: int) -> None:
    if field.codec in ("string_blob", "bytes_blob") and isinstance(item, BlobRef):
        identity = id(item)
        if identity in counts:
            raise InvalidValue(field.name, "blob_shared")
        counts[identity] = 1
        return
    if field.codec == "optional":
        if item is None:
            return
        field = _inner(field)
    if field.codec == "map":
        if type(item) is not dict:
            raise InvalidValue(field.name, "not a map")
        key_field, value_field = _entry_fields(field)
        for key, entry in item.items():
            _write_field(key_field, key)
            _count_value(value_field, entry, counts, opened, depth + 1)
        return
    if field.codec == "keyed":
        if field.elem is None:
            raise InvalidValue(field.name, "keyed element is unsupported")
        values = item if getattr(field, "keyed_list", False) else getattr(item, "_values", None)
        if type(values) is not list or len(values) != len(field.json_keys):
            raise InvalidValue(field.name, "keyed array has an unnamed slot")
        for entry in values:
            _count_value(field.elem, entry, counts, opened, depth + 1)
        return
    if field.codec == "union":
        if not isinstance(item, type(new_union_by_name(field.target))):
            raise InvalidValue(field.name, "not the declared union")
        if not item.type:
            return
        arm = next((arm for arm in item.descriptor.arms if arm.tag == item.type), None)
        if arm is None:
            raise InvalidValue(field.name, "union tag names no arm")
        if arm.field is not None:
            _count_value(arm.field, getattr(item, arm.name), counts, opened, depth + 1)
        return
    if field.codec in ("array", "pointer_array", "union_array"):
        element = _array_element(field)
        if type(item) is not list:
            raise InvalidValue(field.name, "not a list")
        if len(item) > 0x7FFFFFFF or (field.array_kind == 1 and len(item) != field.bound) or (field.array_kind == 2 and not field.array_min <= len(item) <= field.bound):
            raise InvalidValue(field.name, "array count outside its bounds")
        if not field.json_text_list:
            for entry in item:
                _count_value(element, entry, counts, opened, depth + 1)
        return
    if field.codec not in ("pointer", "table"):
        return
    if item is None and field.codec == "pointer":
        return
    cls = by_name(field.target)
    if cls is None or not isinstance(item, cls):
        raise InvalidValue(field.name, "not the declared table")
    key = id(item)
    if key in opened:
        raise InvalidValue(field.name, "cycle")
    if field.codec == "pointer":
        counts[key] = counts.get(key, 0) + 1
        if counts[key] > 1:
            return
    opened.add(key)
    _count(item, counts, opened, depth + 1)
    opened.remove(key)


def _array_element(field: FieldDesc) -> FieldDesc:
    if field.codec == "pointer_array":
        element = copy.copy(field)
        element.codec = "pointer"
        return element
    if field.codec == "union_array":
        element = copy.copy(field)
        element.codec = "union"
        return element
    if field.element is None:
        raise InvalidValue(field.name, "array element is unsupported")
    return field.element


def _write_table(value: Any, depth: int, counts: dict[int, int], labels: dict[int, int], label: int | None) -> str:
    if depth > 128:
        raise InvalidValue(value.descriptor.name, "nested past the depth cap")
    if value.descriptor.json_array:
        field = value.descriptor.fields[0]
        return _render(field, getattr(value, field.name), depth, counts, labels)
    lines: list[str] = []
    indent = "  " * (depth + 1)
    if label is not None:
        lines.append(f'{indent}"&node": {label}')
    for field in value.descriptor.fields:
        if not _guard_holds(field, value):
            continue
        item = getattr(value, field.name)
        if field.codec == "optional" and item is None:
            continue
        rendered = _render(field, item, depth + 1, counts, labels)
        lines.append(f'{indent}{_quote(field.json_key)}: {rendered}')
    if label is not None and len(lines) == 1:
        raise InvalidValue(value.descriptor.name, "a shared node with no field has no text spelling")
    if not lines:
        return "{}"
    return "{\n" + ",\n".join(lines) + "\n" + "  " * depth + "}"


def _render(field: FieldDesc, item: Any, depth: int, counts: dict[int, int], labels: dict[int, int]) -> str:
    if depth > 128 and not (field.codec == "pointer" and item is None) and (field.codec in ("pointer", "table", "array", "pointer_array", "union_array", "map", "keyed", "union") or field.json_flags):
        raise InvalidValue(field.name, "nested past the depth cap")
    if field.codec == "optional":
        return "null" if item is None else _render(_inner(field), item, depth, counts, labels)
    if field.codec in ("pointer", "table"):
        if item is None and field.codec == "pointer":
            return "null"
        key = id(item)
        if field.codec == "pointer" and counts.get(key, 0) > 1:
            if key in labels:
                return '{\n' + "  " * (depth + 1) + f'"&node": {labels[key]}' + '\n' + "  " * depth + '}'
            labels[key] = len(labels) + 1
            return _write_table(item, depth, counts, labels, labels[key])
        return _write_table(item, depth, counts, labels, None)
    if field.codec in ("array", "pointer_array", "union_array"):
        element = _array_element(field)
        if not item:
            return "[]"
        if field.json_text_list:
            parts = []
            spent = 0
            inner = copy.copy(element)
            inner.codec = "string"
            for text in item:
                rendered_text = "null" if element.codec == "text_optional" and text is None else _write_field(inner, text)
                if text is not None:
                    spent += len(text.encode("utf-8"))
                if field.text_total and spent > field.text_total:
                    raise InvalidValue(field.name, "text total exceeds its bound")
                parts.append("  " * (depth + 1) + rendered_text)
        else:
            parts = ["  " * (depth + 1) + _render(element, entry, depth + 1, counts, labels) for entry in item]
        return "[\n" + ",\n".join(parts) + "\n" + "  " * depth + "]"
    if field.codec == "map":
        key_field, value_field = _entry_fields(field)
        if not item:
            return "{}"
        parts = []
        for key in sorted(item):
            key_text = str(key) if key_field.codec == "int" else key
            parts.append("  " * (depth + 1) + _quote(key_text) + ": " + _render(value_field, item[key], depth + 1, counts, labels))
        return "{\n" + ",\n".join(parts) + "\n" + "  " * depth + "}"
    if field.codec == "keyed":
        values = item if getattr(field, "keyed_list", False) else item._values
        if field.elem is None:
            raise InvalidValue(field.name, "keyed element is unsupported")
        parts = ["  " * (depth + 1) + _quote(name) + ": " + _render(field.elem, values[index], depth + 1, counts, labels) for index, name in enumerate(field.json_keys)]
        return "{}" if not parts else "{\n" + ",\n".join(parts) + "\n" + "  " * depth + "}"
    if field.codec == "union":
        if not item.type:
            return "{}"
        arm = next((arm for arm in item.descriptor.arms if arm.tag == item.type), None)
        if arm is None:
            raise InvalidValue(field.name, "union tag names no arm")
        part = "null" if arm.field is None else _render(arm.field, getattr(item, arm.name), depth + 1, counts, labels)
        return "{\n" + "  " * (depth + 1) + _quote(arm.name) + ": " + part + "\n" + "  " * depth + "}"
    if field.json_flags:
        if type(item) is not int or item < 0 or item >> len(field.json_variants):
            raise InvalidValue(field.name, "flags name no variant")
        parts = ["  " * (depth + 1) + _quote(name) for index, name in enumerate(field.json_variants) if item & (1 << index)]
        return "[]" if not parts else "[\n" + ",\n".join(parts) + "\n" + "  " * depth + "]"
    return _write_field(field, item)


def _quote(value: str) -> str:
    escapes = {'"': '\\"', "\\": "\\\\", "\b": "\\b", "\f": "\\f", "\n": "\\n", "\r": "\\r", "\t": "\\t"}
    return '"' + "".join(escapes.get(c, f"\\u{ord(c):04x}" if ord(c) < 32 else c) for c in value) + '"'


def _inner(field: FieldDesc) -> FieldDesc:
    inner = copy.copy(field)
    inner.codec = field.inner
    if field.elem is not None:
        inner.element = field.elem
    return inner


def _guard_holds(field: FieldDesc, value: Any) -> bool:
    return all(getattr(value, name) is wanted for name, wanted in getattr(field, "guards", ()))

"""Scanner for the table text dialect (SPEC-TABLES sections 16.2 and 16.3).

Number tokens stay text until their field's declared type converts them.
"""

from __future__ import annotations

from typing import Any

from . import Refused


class Number(str):
    """An RFC 8259 number, retained exactly as written."""


class _Scanner:
    def __init__(self, text: str, max_key: int | None) -> None:
        self.text = text
        self.max_key = max_key
        self.pos = 1 if text.startswith("\ufeff") else 0

    def fail(self) -> None:
        raise Refused("malformed")

    def space(self) -> None:
        s = self.text
        while self.pos < len(s):
            if s[self.pos] in " \t\r\n":
                self.pos += 1
            elif s.startswith("//", self.pos):
                end = s.find("\n", self.pos + 2)
                if end < 0:
                    end = len(s)
                if "\x00" in s[self.pos:end]:
                    self.fail()
                self.pos = end
            elif s.startswith("/*", self.pos):
                end = s.find("*/", self.pos + 2)
                if end < 0 or "\x00" in s[self.pos:end]:
                    self.fail()
                self.pos = end + 2
            else:
                break

    def peek(self) -> str:
        self.space()
        return self.text[self.pos:self.pos + 1]

    def string(self) -> str:
        s = self.text
        self.pos += 1
        out: list[str] = []
        escapes = {'"': '"', "\\": "\\", "/": "/", "b": "\b", "f": "\f", "n": "\n", "r": "\r", "t": "\t"}
        while self.pos < len(s):
            c = s[self.pos]
            self.pos += 1
            if c == '"':
                return "".join(out)
            if ord(c) < 32 or 0xD800 <= ord(c) <= 0xDFFF:
                self.fail()
            if c != "\\":
                out.append(c)
                continue
            if self.pos >= len(s):
                self.fail()
            c = s[self.pos]
            self.pos += 1
            if c in escapes:
                out.append(escapes[c])
                continue
            if c != "u":
                self.fail()
            code = self.hex4()
            if 0xD800 <= code <= 0xDBFF:
                if s[self.pos:self.pos + 2] != "\\u":
                    self.fail()
                self.pos += 2
                low = self.hex4()
                if not 0xDC00 <= low <= 0xDFFF:
                    self.fail()
                code = 0x10000 + ((code - 0xD800) << 10) + low - 0xDC00
            elif 0xDC00 <= code <= 0xDFFF:
                self.fail()
            out.append(chr(code))
        self.fail()
        raise AssertionError("unreachable")

    def hex4(self) -> int:
        digits = self.text[self.pos:self.pos + 4]
        if len(digits) != 4 or any(c not in "0123456789abcdefABCDEF" for c in digits):
            self.fail()
        self.pos += 4
        return int(digits, 16)

    def number(self) -> Number:
        s = self.text
        start = self.pos
        if s[self.pos:self.pos + 1] == "-":
            self.pos += 1
        c = s[self.pos:self.pos + 1]
        if c == "0":
            self.pos += 1
        elif c and "1" <= c <= "9":
            self.digits()
        else:
            self.fail()
        if s[self.pos:self.pos + 1] == ".":
            self.pos += 1
            if not self.digits():
                self.fail()
        if s[self.pos:self.pos + 1] in ("e", "E"):
            self.pos += 1
            if s[self.pos:self.pos + 1] in ("+", "-"):
                self.pos += 1
            if not self.digits():
                self.fail()
        return Number(s[start:self.pos])

    def digits(self) -> bool:
        start = self.pos
        while self.pos < len(self.text) and "0" <= self.text[self.pos] <= "9":
            self.pos += 1
        return self.pos > start

    def value(self, depth: int) -> Any:
        c = self.peek()
        if c in ("{", "["):
            if depth > 128:
                self.fail()
            self.pos += 1
            if c == "{":
                return self.object(depth)
            return self.array(depth)
        if c == '"':
            return self.string()
        for word, value in (("true", True), ("false", False), ("null", None)):
            if self.text.startswith(word, self.pos):
                self.pos += len(word)
                return value
        if c and (c == "-" or "0" <= c <= "9"):
            return self.number()
        self.fail()
        raise AssertionError("unreachable")

    def object(self, depth: int) -> dict[str, Any]:
        result: dict[str, Any] = {}
        while self.peek() != "}":
            if self.peek() != '"':
                self.fail()
            key = self.string()
            if (self.max_key is not None and len(key.encode("utf-8")) > self.max_key) or key in result or self.peek() != ":":
                self.fail()
            self.pos += 1
            result[key] = self.value(depth + 1)
            if self.peek() == "}":
                break
            if self.peek() != ",":
                self.fail()
            self.pos += 1
        self.pos += 1
        return result

    def array(self, depth: int) -> list[Any]:
        result: list[Any] = []
        while self.peek() != "]":
            result.append(self.value(depth + 1))
            if self.peek() == "]":
                break
            if self.peek() != ",":
                self.fail()
            self.pos += 1
        self.pos += 1
        return result


def parse(text: str | bytes | bytearray | memoryview, max_key: int | None = 255) -> Any:
    """Scan one complete text (or its UTF-8 bytes), refusing malformed syntax without coercion."""
    if isinstance(text, (bytes, bytearray, memoryview)):
        # the bytes entry (SPEC-TABLES 16.3): validated as UTF-8 like a text entry
        try:
            text = bytes(text).decode("utf-8")
        except UnicodeDecodeError:
            raise Refused("invalid_utf8") from None
    if type(text) is not str:
        raise Refused("kind_mismatch")
    try:
        text.encode("utf-8")
    except UnicodeEncodeError:
        raise Refused("invalid_utf8") from None
    scanner = _Scanner(text, max_key)
    value = scanner.value(0)
    scanner.space()
    if scanner.pos != len(text):
        raise Refused("malformed")
    return value

"""The schema table runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), the load report, and the form-1 table codec that walks a descriptor.
The package is emitted verbatim into every generated package as its _schema
subpackage and is reached only by relative import, so two generated packages
never share or shadow each other's runtime.
"""

from typing import Final

__all__: Final = ["InvalidValue", "LoadReport", "Refused"]


class Refused(ValueError):
    """A read refused its input. It carries no value.

    reason is the refusal's name (past_end, wire_damaged, ...), a diagnostic
    that is never compared across languages; only the verdict is.
    """

    reason: str

    def __init__(self, reason: str, detail: str = "") -> None:
        super().__init__(f"{reason}: {detail}" if detail else reason)
        self.reason = reason


class InvalidValue(ValueError):
    """A write refused a value outside its contract, before writing a byte.

    path names the field (Player.name, Fleet.ships[3].hull) and rule what it
    broke.
    """

    path: str
    rule: str

    def __init__(self, path: str, rule: str) -> None:
        super().__init__(f"{path}: {rule}")
        self.path = path
        self.rule = rule


class LoadReport:
    """The counters a successful load fills (docs/PORT-PYTHON.md, ERRORS).

    A refused load leaves the report exactly as it was; the whole value or
    Refused is the one read.
    """

    __slots__ = ("unknown", "widened", "retained", "retain_lost")

    unknown: int
    widened: int
    retained: int
    retain_lost: int

    def __init__(self) -> None:
        self.reset()

    def reset(self) -> None:
        self.unknown = 0
        self.widened = 0
        self.retained = 0
        self.retain_lost = 0

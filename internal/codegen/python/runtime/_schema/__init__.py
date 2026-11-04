"""The schema runtime every generated Python package carries.

The errors every generated read and write raises (docs/PORT-PYTHON.md,
ERRORS), and serialize.py, the bit layer, vendored beside this module in
serialize/. The package is emitted verbatim into every generated package as
its _schema subpackage and is reached only by relative import, so two
generated packages never share or shadow each other's runtime.
"""

__all__ = ["InvalidValue", "Refused"]


class Refused(ValueError):
    """A read refused its input. It carries no value.

    reason is the refusal's name (past_end, non_finite_float, ...), a
    diagnostic that is never compared across languages; only the verdict is.
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

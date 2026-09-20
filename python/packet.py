"""Minimal packet wire implementation for schema fixed-form tables."""

from dataclasses import dataclass
from typing import List


@dataclass
class FixedEntry:
    """A plan entry with id, kind, size, children."""
    id: int
    kind: int
    size: int
    children: int


class TableReport:
    """Report of a fixed-form table load."""
    def __init__(self):
        self.refused: bool = False
        self.reason: str = ""
        self.malformed: bool = False


class FixedLoadResult:
    """Result of a fixed-form table load."""
    def __init__(self):
        self.count: int = -1  # -1 on refusal
        self.report: TableReport = TableReport()


def fixed_load(data: bytes, plan: List[FixedEntry]) -> FixedLoadResult:
    """
    Load a fixed-form table from bytes with the given plan capacity.
    
    Returns count=-1 and sets report.reason="plan_too_large" when the plan
    cannot hold all the layout's entries (F11: plan_too_large).
    """
    result = FixedLoadResult()
    report = result.report
    
    # Header check: form byte must be 3
    if len(data) < 20:
        report.malformed = True
        return result
    
    if data[0] != 3:
        report.malformed = True
        return result
    
    # Layout length at byte 16 (u32 LE)
    layout_len = data[16] | (data[17] << 8) | (data[18] << 16) | (data[19] << 24)
    
    # Entry count at start of layout (u32 LE)
    if len(data) < 20 + layout_len:
        report.malformed = True
        return result
    
    entry_count = data[20] | (data[21] << 8) | (data[22] << 16) | (data[23] << 24)
    
    # F11: plan_too_large - check if plan can hold all entries
    # The C++ reference: if (int64(entryCount) > int64(len(plan))) return plan_too_large
    if entry_count > len(plan):
        report.refused = True
        report.reason = "plan_too_large"
        return result
    
    result.count = entry_count
    return result

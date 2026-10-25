"""
F11: plan_too_large

Rule from docs/SPEC-TABLES.md:8411:
  | `plan_too_large` | a fixed-form layout whose compiled plan does not fit 
  the plan storage the caller declared (§3.4), which is the announced 
  vocabulary's `vocabulary_too_large` for the same reason and on the same rule: 
  this codec never allocates |

C++ reference oracle (internal/codegen/gotable/fixedguard_test.go:193-200):
  if n := HostFixedLoad(make([]Host, 1), buf, make([]TableFixedEntry, 1), &r); 
      n != -1 {
      t.Fatalf("a one-entry plan slice read n=%d", n)
  }
  if r.Reason != "plan_too_large" {
      t.Fatalf("the identity lane owes plan_too_large, got %q", r.Reason)
  }

The test: when the caller's plan slice has fewer entries than the layout
declares, the loader returns count=-1 with reason="plan_too_large".
"""

import sys, os
# the python leg's modules live at the repository root, three levels up from
# test/py-packet/<row>/ -- resolved from THIS file so the case runs from any checkout
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', '..', 'python'))

from packet import fixed_load, FixedEntry, TableReport


def test_plan_too_large():
    """
    F11: plan_too_large
    
    When a layout declares N entries but the caller's plan can only hold
    M entries (M < N), the loader must refuse with reason="plan_too_large".
    """
    # Build a minimal fixed-form layout with 5 entries (form byte 3)
    # Header: form=3, reserved(7 zeros), hash(8), layout_len(4), entry_count(4)
    # Layout bytes: entry_count (4 bytes) + 5 Entry structs (17 bytes each)
    
    entry_count = 5
    layout_len = 4 + 5 * 17  # 4 bytes count + 5 entries × 17 bytes
    
    # Build data with proper header
    data = bytearray(20 + layout_len)
    data[0] = 3  # form byte
    
    # Layout length at 16 (u32 LE)
    data[16] = layout_len & 0xFF
    data[17] = (layout_len >> 8) & 0xFF
    data[18] = (layout_len >> 16) & 0xFF
    data[19] = (layout_len >> 24) & 0xFF
    
    # Entry count at 20 (u32 LE)
    data[20] = entry_count & 0xFF
    data[21] = (entry_count >> 8) & 0xFF
    data[22] = (entry_count >> 16) & 0xFF
    data[23] = (entry_count >> 24) & 0xFF
    
    # Fill rest with zeros (entries don't matter for this test)
    
    # Plan has only 1 entry (shorter than layout's 5)
    plan = [FixedEntry(id=0, kind=13, size=0, children=0)]
    
    result = fixed_load(bytes(data), plan)
    
    # Should refuse with plan_too_large
    assert result.count == -1, f"Expected count=-1, got {result.count}"
    assert result.report.refused == True, "Expected refused=True"
    assert result.report.reason == "plan_too_large", f"Expected 'plan_too_large', got {result.report.reason}"
    
    print("PASS: plan_too_large")


def test_plan_fits():
    """
    Control: when plan fits the layout, load succeeds.
    """
    entry_count = 2
    layout_len = 4 + 2 * 17
    
    data = bytearray(20 + layout_len)
    data[0] = 3
    data[16] = layout_len & 0xFF
    data[17] = (layout_len >> 8) & 0xFF
    data[18] = (layout_len >> 16) & 0xFF
    data[19] = (layout_len >> 24) & 0xFF
    data[20] = entry_count & 0xFF
    data[21] = (entry_count >> 8) & 0xFF
    data[22] = (entry_count >> 16) & 0xFF
    data[23] = (entry_count >> 24) & 0xFF
    
    # Plan has exactly 2 entries
    plan = [FixedEntry(id=i, kind=13, size=0, children=0) for i in range(2)]
    
    result = fixed_load(bytes(data), plan)
    
    assert result.count == 2, f"Expected count=2, got {result.count}"
    assert result.report.refused == False, "Expected refused=False"
    
    print("PASS: plan fits")


def test_negative_control_too_many_entries():
    """
    Negative control: increase entries in layout, should trigger plan_too_large.
    """
    entry_count = 10  # layout declares 10 entries
    layout_len = 4 + 10 * 17
    
    data = bytearray(20 + layout_len)
    data[0] = 3
    data[16] = layout_len & 0xFF
    data[17] = (layout_len >> 8) & 0xFF
    data[18] = (layout_len >> 16) & 0xFF
    data[19] = (layout_len >> 24) & 0xFF
    data[20] = entry_count & 0xFF
    data[21] = (entry_count >> 8) & 0xFF
    data[22] = (entry_count >> 16) & 0xFF
    data[23] = (entry_count >> 24) & 0xFF
    
    # Plan has only 1 entry
    plan = [FixedEntry(id=0, kind=13, size=0, children=0)]
    
    result = fixed_load(bytes(data), plan)
    
    assert result.count == -1, f"Expected count=-1, got {result.count}"
    assert result.report.reason == "plan_too_large"
    
    print("PASS: negative control (too many entries)")


if __name__ == "__main__":
    test_plan_too_large()
    test_plan_fits()
    test_negative_control_too_many_entries()
    print("All tests passed.")

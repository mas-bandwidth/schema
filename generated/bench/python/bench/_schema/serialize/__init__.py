"""serialize.py: the serialize bit packer for Python, wire format 2.0.

The public surface (IMPLEMENTING.md U15, "Exports beyond the reference
operations"): the three streams and the Stream surface they share, Ref, the
refusal list and its name function, WriteMisuse, copy_string, and the
reference's helpers bits_required, compressed_float_params,
float_bits_are_finite and double_bits_are_finite, with the float32 and float64
bit casts and float32_round, which Python needs because it has no float32 type.
"""

from ._core import (
    FORMAT_VERSION,
    MeasureStream,
    ReadStream,
    Ref,
    Refusal,
    Stream,
    WriteMisuse,
    WriteStream,
    bits_required,
    compressed_float_params,
    copy_string,
    double_bits_are_finite,
    float32_bits,
    float32_from_bits,
    float32_round,
    float64_bits,
    float64_from_bits,
    float_bits_are_finite,
    refusal_name,
)

__all__ = [
    "FORMAT_VERSION",
    "MeasureStream",
    "ReadStream",
    "Ref",
    "Refusal",
    "Stream",
    "WriteMisuse",
    "WriteStream",
    "bits_required",
    "compressed_float_params",
    "copy_string",
    "double_bits_are_finite",
    "float32_bits",
    "float32_from_bits",
    "float32_round",
    "float64_bits",
    "float64_from_bits",
    "float_bits_are_finite",
    "refusal_name",
]

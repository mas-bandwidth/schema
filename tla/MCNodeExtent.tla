---------------------------- MODULE MCNodeExtent ------------------------------
\* The model-checking instance of NodeExtent: the element shapes, as records,
\* which a .cfg file cannot spell. Every size is a multiple of its alignment, as
\* the C ABI makes it; sixteen is a 128-bit kind's.
EXTENDS NodeExtent

MCElems == {[size |-> 1, align |-> 1], [size |-> 4, align |-> 4], [size |-> 8, align |-> 8],
            [size |-> 12, align |-> 4], [size |-> 24, align |-> 8],
            [size |-> 16, align |-> 16]}
=============================================================================

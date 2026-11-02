# Blob sharing report census

Nine literal file-wire vectors pin SPEC-TABLES section 2.5's tolerant-read
blob-sharing events and section 4's kind mismatches. They cover shared and
distinct equal blobs, empty blobs, null slots, wrong blob kinds, and a repeated
field naming one blob again. A successful extra naming counts even when a
repeated field subsequently replaces the same slot.

Shared is the expected blob_shared increment per load; KindMismatch is the
expected kind-mismatch increment. Saved is the canonical re-save for cases
whose recovered value is writable; an empty Saved means blob_shared refusal.
The counts are literal, checked independently with plain and retaining neutral
decoders. Both native JS entrypoints run every vector twice with fresh and
pre-populated report history (72 successful decode observations).

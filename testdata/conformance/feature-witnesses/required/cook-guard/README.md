The four pins are native generated-Go cooks of authored `Guard{On:false,N:n}`,
for `n=0` and `n=11`, in little and big endian order. Columns are n, byte-order
number (1 little, 2 big), and hex bytes. The schema's declared default is 5;
these authored physical values deliberately override it while the guard is off.

Generated with schema source `1c883940116befe4a5c1dcf9575c62631a7cab42`
(including Go runtime repair `dd7373d71f2138bed0313170b9ed7d8d30d7a8b4`), using
`generate --lang go --runtime standalone --surface cook`, then compiling and
executing `GuardCookMeasureReason` and `GuardCookFrom` on Hulk. Removing only
`, required` and independently regenerating/recompiling gives identical bytes
for all four pins. Required does not change the physical cook projection
(SPEC-TABLES section 2.3); inactive storage must not be silently zeroed.

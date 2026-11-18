# Frozen pre-v3 build identity

Both source declarations were checked by actual pre-v2 compiler
948ec717c6d047f7e45f6309d47f2bc312a6b1ca (WireLaw2, BuildVersionForm5).
`schema build-version --facts` emitted byte-identical cook projections for
`[..4]int32` and `[2..4]int32`; `schema build-version` printed
0x83c01f94234296fd for both. The saved projection is that actual output.

This freezes the historical collision. Form6 adds a separate countmin token;
it does not rewrite the old projection or claim its two schemas had distinct
historical build versions.

# The ruled headline table (#184): two columns, language and percent of
# generated C++ at 100%. Shared by the --quick sweep tail and --render.
# Lives in its own file so no bash quoting can ever truncate it — the
# 2026-09-01 render break was an apostrophe in a comment ending the
# single-quoted inline program.
# js emits two gen tiers; the flat tier is THE js path (codec
# column, $18), so codec=runtime rows never enter the table
$2 == "bench_mixed" && $13 == "gen" && $18 != "runtime" && $3 == "round_trip" { gt[$1] = 1e9 / $9; gs[$1] = $11 }
# ts[] is per-language time per message in ns; cpp is the
# denominator, so cpp prints 100% and a faster language prints
# below it.
# §2.3 spread policy, enforced without adding a column (the owner
# ruled the table to exactly two): a row over the INVALID
# threshold prints no number at all, and every noisy row is named
# in a note BELOW the table. spread_pct rides in the CSV as always.
function render(ts, sp, absent,    i, j, n, tt, tl, langs, m, sk, parts, ref, notes) {
    i = 0
    for (lang in ts) { i++; langs[i] = lang }
    n = i
    for (i = 1; i <= n; i++)
        for (j = i + 1; j <= n; j++)
            if (ts[langs[j]] < ts[langs[i]]) {
                tl = langs[i]; langs[i] = langs[j]; langs[j] = tl
            }
    ref = ("cpp" in ts) ? ts["cpp"] : 0
    if (n == 0 && !absent) {
        print "  (no rows in this run)"
        return 0
    }
    printf "  %-10s %6s\n", "language", "%"
    notes = ""
    for (i = 1; i <= n; i++) {
        lang = langs[i]
        if (sp[lang] > 40.0) {
            printf "  %-10s %6s\n", lang, "—"
            notes = notes sprintf("  §2.3 INVALID: %s spread %.1f%% > 40%% — the row does not publish as a number\n", lang, sp[lang])
        } else if (ref > 0) {
            # The c/cpp statistical tie (owner ruling 2026-09-01, §2.8):
            # the two legs are the same clang word codec, and their gap
            # has never left the spread of the pair itself — so when c sits
            # inside the combined c+cpp spread, both print 100%.
            # Display rule only: the CSV always carries the raw rates,
            # and a future sitting that separates them beyond the noise
            # prints the real figure, which is the exit the ruling names.
            pct = ts[lang] / ref * 100.0
            # tie threshold: the combined within-sitting
            # spread, floored at 3.0 points — the measured
            # cross-sitting noise floor for this pair (c has
            # printed 97-100% across the sittings of this era on
            # byte-frozen code; the tie paragraph in §2.8 carries the
            # derivation and the exit).
            tieband = sp["c"] + sp["cpp"]; if (tieband < 3.0) tieband = 3.0
            if (lang == "c" && ("cpp" in sp) && (pct - 100.0 <= tieband) && (100.0 - pct <= tieband)) {
                printf "  %-10s %5.0f%%\n", lang, 100.0
                notes = notes sprintf("  §2.8 TIE: c measured %.1f%% of cpp, inside the tie band (%.1f points) — reported as a statistical tie at 100%%\n", pct, tieband)
            } else {
                printf "  %-10s %5.0f%%\n", lang, pct
            }
            if (sp[lang] > 15.0)
                notes = notes sprintf("  §2.3 NOISY: %s spread %.1f%% > 15%% — judge this row against the noise, not the digit\n", lang, sp[lang])
        } else {
            printf "  %-10s %6s\n", lang, "—"
        }
    }
    if (ref <= 0 && n > 0)
        print "  (no cpp row in this run: cpp is the 100% DENOMINATOR, so no percentage is defined — CSV carries the rates)"
    if (notes != "")
        printf "%s", notes
    # loud skips (#175): a missing leg is an ABSENT row with its
    # reason, never a silently narrower table
    if (absent) {
        m = split(skips, sk, ";")
        for (i = 1; i <= m; i++) {
            if (sk[i] == "") continue
            split(sk[i], parts, "|")
            printf "  %-10s %6s   ABSENT — %s\n", parts[1], "—", parts[2]
        }
    }
    return n
}
END {
    print "subject: schema-GENERATED code (family gen) — what the compiler delivers; C++ = 100%"
    if (render(gt, gs, 1) == 0) {
        print "REFUSED (#175/§2.9): the gen headline section has ZERO rows — no leg reported a bench_mixed family-gen round_trip row. Nothing was measured; printing an empty table at exit 0 is the defect this refusal exists to stop." > "/dev/stderr"
        exit 3
    }
}

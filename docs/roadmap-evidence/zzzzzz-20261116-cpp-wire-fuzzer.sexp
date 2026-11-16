(:id "vt/wire-fuzzer/cpp" :state :done
 :evidence ("docs/validation/2026-11-16-cpp-wire-fuzzer.json: 12 strict C++ plain/retain normal/ASan campaigns, seeds 1/2/3, 3,356,796 mutations, zero divergences or absent codecs; 26 bounded C++ replays"
            "make tables-wire-fuzz-negative-control tables-wire-fuzz-retain-negative-control N=0 SEED=1: length, index, union-arm, oracle, message-text and retention controls detect their planted faults"
            "make tables-wire-fuzz-regressions: normal and ASan regressions pass"
            "internal/codegen/cpptable/retain_hashzero_test.go: TestCppRetainNestedHashZero")
 :tested-revision "f2b8acbddc01e4f0585822cfc1ee077168694781"
 :note "2026-11-16: Linux amd-2 under nova-reserve. Campaigns used frozen source 1240d3d8562397b0c111c524dbf0e4479f4042e4; current main has identical C++ emitter and driver, Go oracle, harness, corpus, schemas and IR, and all 1,130 generated C++ files compare byte-for-byte. Frozen binary hashes and serialize pins are recorded in the validation receipt. Current-main native controls and hash-zero tests pass. This evidence covers the wire-fuzzer leaf; it does not claim the separate full C++ nightly or C and JavaScript leaves.")

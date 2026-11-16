;; Restored complete landing-rule proof over generated old/new lineage modules.
(:id "go/R20" :state :done
 :evidence ("test/conformance/go/rows/R20_test.go: TestRowR20"
            "test/conformance/go/rows/R20_test.go: TestRowR20FloatWidensExactly")
 :tested-revision "7cf8b94f9ddbb6956e3b29e0ba4fbd371bb48374"
 :note "The whole R20 entry point runs added defaults, carried deprecated value B=2 with no unknown event, exact integer and finite float32-to-float64 widening with one widened event, clean same-width baseline, short string/array zero slack, and prefix-enum identity. Float controls include both signed zeros, subnormals, ordinary values and both finite extremes. The deprecated assertion is active; relax/deprecated-carried is removed. The title reflects the carried-slot rule.")

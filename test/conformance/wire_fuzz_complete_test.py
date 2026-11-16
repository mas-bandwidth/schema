#!/usr/bin/env python3
"""Boundary controls for native wire-fuzz roster and process verdicts."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("wire-fuzz-complete")
SUMMARY = "wire-fuzz: seed 1, 327 seeds over 63 roots, 224113 enumerated + 0 random = 224113 mutants, 0 divergences, 1.0 s, 224113 mutants/s"


class CompleteWireFuzz(unittest.TestCase):
    def run_gate(self, text, code=0):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp) / "nested" / "result.log"
            result = subprocess.run(
                ["sh", str(SCRIPT), str(log), sys.executable, "-c",
                 "import sys; print(sys.argv[1]); sys.exit(int(sys.argv[2]))", text, str(code)],
                text=True, capture_output=True, check=False,
            )
            self.assertEqual(log.read_text(), text + "\n")
            return result

    def test_complete_roster(self):
        self.assertEqual(self.run_gate(SUMMARY).returncode, 0)

    def test_absent_codec_is_not_a_pass(self):
        for prefix in ("wire-fuzz", "wire-fuzz retain"):
            with self.subTest(prefix=prefix):
                text = SUMMARY.replace("wire-fuzz:", prefix + ":").replace(
                    "63 roots,", "63 roots, 4 seeds absent (roots the leg has no codec for),")
                result = self.run_gate(text)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("wire-fuzz roster incomplete", result.stderr)

    def test_child_failure_keeps_its_exit(self):
        self.assertEqual(self.run_gate(SUMMARY, 7).returncode, 7)

    def test_empty_campaign_is_not_a_pass(self):
        for text in ("", "wire-fuzz: 0 seeds over 0 roots, 0 mutants, 0 divergences"):
            with self.subTest(text=text):
                result = self.run_gate(text)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("wire-fuzz coverage summary missing or empty", result.stderr)


if __name__ == "__main__":
    unittest.main()

"""Generate the binding-name corpus, type-check its consumers, and run them in Node."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


CORPUS = Path(__file__).resolve().parent
SCHEMA = Path(os.environ.get("SCHEMA", "bin/schema")).resolve()
# scratch stays inside the working tree, under build/
BUILD = Path("build/js-binding-names").resolve()


class BindingNames(unittest.TestCase):
    def setUp(self):
        BUILD.mkdir(parents=True, exist_ok=True)
        scratch = tempfile.TemporaryDirectory(dir=BUILD)
        self.addCleanup(scratch.cleanup)
        self.out = Path(scratch.name)

    def generate(self, name):
        return subprocess.run(
            [SCHEMA, "generate", "--lang", "js", "--out", self.out, CORPUS / (name + ".schema")],
            capture_output=True, text=True,
        )

    def test_refused_bindings(self):
        for schema, names in [
            ("BadTypes", ["unknown", "never", "symbol", "number", "bigint", "undefined", "eval", "arguments"]),
            ("BadValues", ["undefined", "eval", "arguments"]),
        ]:
            with self.subTest(schema=schema):
                result = self.generate(schema)
                self.assertEqual(result.returncode, 1, result.stderr)
                for name in names:
                    self.assertRegex(result.stderr, rf"\b(?:declaration|type|const) {name}\b")
                self.assertEqual(list(self.out.iterdir()), [])

    def run_consumer(self, name):
        result = self.generate(name)
        self.assertEqual(result.returncode, 0, result.stderr)
        (self.out / "package.json").write_text('{"type":"module"}\n')
        shutil.copy(CORPUS / (name.lower() + ".ts"), self.out / "consumer.ts")
        subprocess.run(
            [os.environ.get("TSC", "tsc"), "--strict", "--target", "ES2022", "--module", "NodeNext",
             "--moduleResolution", "NodeNext", "consumer.ts"], cwd=self.out, check=True,
        )
        subprocess.run([os.environ.get("NODE", "node"), "consumer.js"], cwd=self.out, check=True)

    def test_predefined_types_as_values(self):
        self.run_consumer("Values")

    def test_shadowed_platform_name(self):
        self.run_consumer("Control")


if __name__ == "__main__":
    unittest.main()

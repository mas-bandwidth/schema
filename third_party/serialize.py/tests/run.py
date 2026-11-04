"""The test runner: every tests/test_*.py, under the interpreter that runs it.

usage: <python> -I tests/run.py [test name ...]

-I isolates the run (no PYTHON* environment variable, no user site
directory, no script directory on sys.path), so the package and the tests are
found only where this file puts them: src/ and tests/ of this repository. A run
that discovers no test fails, by name, rather than passing over nothing.
"""

import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path[:0] = [os.path.join(ROOT, "src"), HERE]
sys.dont_write_bytecode = True


def main() -> int:
    loader = unittest.TestLoader()
    if len(sys.argv) > 1:
        suite = loader.loadTestsFromNames(sys.argv[1:])
    else:
        suite = loader.discover(HERE, pattern="test_*.py", top_level_dir=HERE)
    if suite.countTestCases() == 0:
        print("run.py REFUSES: no test was discovered", file=sys.stderr)
        return 1
    print(f"serialize.py tests under Python {sys.version.split()[0]}", flush=True)
    result = unittest.TextTestRunner(verbosity=1, buffer=False).run(suite)
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    sys.exit(main())

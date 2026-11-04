"""The vendored family files are the pin VENDORED names, byte for byte.

STANDARD.md, IMPLEMENTING.md, conformance/ and conformance-controls/ come from
mas-bandwidth/serialize at one commit. Every file VENDORED lists has its
sha256, and no file is present under those roots that the list does not name,
so a corpus edited in place, or a vector added beside it, is red here before
the runner reads it.
"""

from __future__ import annotations

import hashlib
import os
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DIRECTORIES = ("conformance", "conformance-controls")
FILES = ("STANDARD.md", "IMPLEMENTING.md")


def read_pin(text: str) -> tuple[dict[str, str], dict[str, str]]:
    """(header fields, path -> sha256) of a VENDORED file's text."""
    fields: dict[str, str] = {}
    sums: dict[str, str] = {}
    for line in text.splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        words = line.split()
        if words[0] == "sha256":
            if len(words) != 3 or words[2] in sums:
                raise ValueError(f"VENDORED: malformed or repeated line {line!r}")
            sums[words[2]] = words[1]
        elif len(words) == 2 and words[0] in ("repository", "tag", "commit"):
            fields[words[0]] = words[1]
        else:
            raise ValueError(f"VENDORED: unrecognized line {line!r}")
    return fields, sums


def problems(root: str, sums: dict[str, str]) -> list[str]:
    out: list[str] = []
    present = set(f for f in FILES if os.path.exists(os.path.join(root, f)))
    for d in DIRECTORIES:
        if os.path.isdir(os.path.join(root, d)):
            present.update(f"{d}/{f}" for f in os.listdir(os.path.join(root, d)))
    for path, want in sorted(sums.items()):
        if path not in present:
            out.append(f"{path} is missing")
            continue
        with open(os.path.join(root, path), "rb") as f:
            got = hashlib.sha256(f.read()).hexdigest()
        if got != want:
            out.append(f"{path} has sha256 {got}, the pin is {want}")
    out += [f"{path} is present and the pin does not list it" for path in sorted(present - set(sums))]
    return out


class VendoredTest(unittest.TestCase):
    def test_the_vendored_files_are_the_pin(self) -> None:
        with open(os.path.join(ROOT, "VENDORED"), encoding="utf-8") as f:
            fields, sums = read_pin(f.read())
        self.assertEqual(fields.get("repository"), "https://github.com/mas-bandwidth/serialize")
        self.assertEqual(len(fields.get("commit", "")), 40)
        self.assertTrue(fields.get("tag", "").startswith("v"))
        for name in FILES:
            self.assertIn(name, sums)
        self.assertGreater(sum(1 for p in sums if p.startswith("conformance/")), 0)
        self.assertEqual(problems(ROOT, sums), [])

    def test_a_changed_or_unlisted_file_is_refused(self) -> None:
        # the check's reversed witness, over the real tree: a wrong sum and a
        # list missing one file must each be named
        with open(os.path.join(ROOT, "VENDORED"), encoding="utf-8") as f:
            _, sums = read_pin(f.read())
        wrong = dict(sums, **{"STANDARD.md": "0" * 64})
        self.assertTrue(any("STANDARD.md has sha256" in p for p in problems(ROOT, wrong)))
        short = {k: v for k, v in sums.items() if k != "conformance/int.txt"}
        self.assertIn("conformance/int.txt is present and the pin does not list it", problems(ROOT, short))


if __name__ == "__main__":
    unittest.main()

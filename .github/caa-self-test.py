#!/usr/bin/env python3
"""CAA allowlist decision for the schema main tip.

Same rule as contributor-assistant/github-action v2.6.1
(ca4a40a7d1004f18d9960b404b97e5f30a505a08): one identity per commit, then
the allowlist in .github/workflows/cla.yml. The action cannot run here.
It queries a pull request and refuses anything else.

Exits non-zero when the tip would be rejected, or when an unlinked house
name from issue #1799 is no longer covered. A passing tip is not enough:
the tip is a linked login, and the red pulls were not.
"""

import json
import os
import re
import sys
import urllib.request

ALLOWLIST_PATH = os.path.join(".github", "workflows", "cla.yml")
TIP_URL = "https://api.github.com/repos/mas-bandwidth/schema/commits/main"
# github-actions[bot]. The action drops this id and then passes if nobody
# is left (src/graphql.ts).
GITHUB_ACTIONS_ID = 41898282
# Git author names with no GitHub user. The ledger cannot store them.
HOUSE_GIT_NAMES = ("Glenn Fiedler", "Nova Fleet", "nova-merge")


def allowlist_from(text):
    for raw in text.splitlines():
        line = raw.strip()
        if line.startswith("- "):
            line = line[2:].strip()
        if not line or line.startswith("#") or not line.startswith("allowlist:"):
            continue
        value = line.split(":", 1)[1].strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        return value
    raise SystemExit("caa self-test: %s has no allowlist line" % ALLOWLIST_PATH)


def covered(name, allow):
    """checkAllowList.ts: trim, exact equality, or lodash escape with * -> .*
    and an unanchored RegExp.test."""
    if name is None:
        return False
    for pattern in allow.split(","):
        pattern = pattern.strip()
        if pattern == "":
            continue
        if "*" in pattern:
            escaped = re.escape(pattern).replace(r"\*", ".*")
            if re.search(escaped, name):
                return True
        elif pattern == name:
            return True
    return False


def identity(commit):
    """extractUserFromCommit: author user, else committer user, else git author."""
    author_user = commit.get("author") or {}
    committer_user = commit.get("committer") or {}
    git_author = (commit.get("commit") or {}).get("author") or {}
    git_committer = (commit.get("commit") or {}).get("committer") or {}
    if author_user.get("login"):
        return author_user["login"], author_user.get("id")
    if committer_user.get("login"):
        return committer_user["login"], committer_user.get("id")
    if git_author.get("name"):
        return git_author["name"], None
    return git_committer.get("name"), None


def fetch_tip():
    request = urllib.request.Request(
        TIP_URL,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "caa-self-test",
        },
    )
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        request.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def main():
    with open(ALLOWLIST_PATH, encoding="utf-8") as handle:
        allow = allowlist_from(handle.read())
    missing = [name for name in HOUSE_GIT_NAMES if not covered(name, allow)]
    if missing:
        raise SystemExit(
            "caa self-test: allowlist does not cover %s" % ", ".join(missing)
        )
    commit = fetch_tip()
    name, user_id = identity(commit)
    sha = commit.get("sha", "")
    if user_id == GITHUB_ACTIONS_ID:
        print("caa self-test: %s tip %s is github-actions; the action drops it" % (sha, name))
        return
    if not covered(name, allow):
        raise SystemExit(
            "caa self-test: schema main tip %s identity %r is not on the caa allowlist"
            % (sha, name)
        )
    print("caa self-test: schema main tip %s identity %r is covered" % (sha, name))
    for house_name in HOUSE_GIT_NAMES:
        print("caa self-test: %r is covered" % house_name)


if __name__ == "__main__":
    main()

RESULT: a-cut-ignores-the-previous-tags-cancelled-release-run sha=000000000000 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: dev
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
PRIORITY: high
PATHS: pkg/release/ci.go, pkg/release/ci_test.go, pkg/release/release.go, cmd/nova-update/README.md, docs/fixes.sexp, FIXES.md
SHARED: docs/fixes.sexp, FIXES.md
SOURCE: the seat, 2026-10-11 ~20:00Z, cutting nova-tools v1.2.11 from the v1.2.10 commit 2314d437f: the dry run was refused "CI is not green on this commit: build=cancelled, certified=cancelled, release=cancelled" although every test check on the commit is green; the three cancelled checks are release.yml run 38141786159, started by the v1.2.10 tag push on that same commit and cancelled on purpose by the v1.2.10 publish step (the hand cut publishes its own assets). The cut went out under a CI waiver naming exactly that.
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
No dashboard change: the dashboard changes only on the owner's explicit ask, and this card makes none.
Report what was not done.

THE TASK. CLASS: a gate that reads its own earlier output as a failure. nova-update release cut reads every check run on the release commit and refuses the cut when any is not success or skipped. The release.yml workflow runs on every tag push and the publish recipe cancels it (the hand cut publishes its own assets), so a second cut from the same commit, which lockstep demands whenever nova-sprint moves and nova-tools does not, always finds three cancelled checks from the previous tag and needs a CI waiver. The waiver is the wrong tool: it is for red tests, and a cancelled run of the release w

Done is:
(1) Red first, pkg/release/ci_test.go: the release-workflow run of a previous tag is set aside; the same checks from this cut are still red. Today both refuse: keep the failing line.
(2) The cut names the runs it set aside in its RELEASE CUT line.

STEP 1. Enter your worktree with cd $JOB/repo && git log --oneline -1.
STEP 2. Write the tests red first; keep the failing line for the report.
STEP 3. Make them pass in the files PATHS names, and only those.


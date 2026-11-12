#!/usr/bin/env bash
# Real cross-agent handoff with installed, authenticated clients:
#   Codex records a task, decision, rejected approach and failing test, then hands off;
#   Claude Code starts cold, recovers the constraints from memory and implements the fix;
#   OpenCode 2 (free model, isolated HOME) and a fresh Codex session read the result back.
#
# Usage: scripts/real-client-handoff.sh SCRATCH_DIR
#
# Safety: everything lives under SCRATCH_DIR (a disposable Lite memory and Git repo). No client
# configuration file is written: Codex gets the server through -c overrides, Claude through
# --mcp-config with --setting-sources project,local (so user-level hooks, e.g. an UltraLite
# installation's, do not run), OpenCode through a project opencode.json under an isolated HOME.
# If an UltraLite installation exists, its project list is compared before and after and the
# script fails if it changed. The runs use real models (Codex/Claude: your subscription;
# OpenCode: a free model).
set -euo pipefail

SCRATCH=${1:?usage: $0 SCRATCH_DIR}
[ -e "$SCRATCH" ] && { echo "refusing to reuse $SCRATCH"; exit 2; }
for c in git python3 codex claude opencode; do command -v "$c" >/dev/null || { echo "missing $c"; exit 2; }; done
ROOT=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$SCRATCH"
T="$SCRATCH/thoth-lite"
(cd "$ROOT" && go build -o "$T" ./cmd/thoth-lite)
DATA="$SCRATCH/data"
REPO="$SCRATCH/repo"
OCHOME="$SCRATCH/opencode-home"

live_fingerprint() {
  if command -v thothmemory-ultralite >/dev/null; then thothmemory-ultralite projects 2>/dev/null | sha256sum; else echo none; fi
}
LIVE_BEFORE=$(live_fingerprint)

mkdir -p "$REPO" && cd "$REPO"
git init -q -b main && git config user.email t@example.invalid && git config user.name Tester
git remote add origin https://github.com/thoth-lite-test/handoff-demo.git
printf 'def divide(a, b):\n    return a / b\n' > calc.py
cat > test_calc.py <<'EOF'
import unittest
from calc import divide

class T(unittest.TestCase):
    def test_basic(self):
        self.assertEqual(divide(6, 3), 2)

    def test_zero(self):
        self.assertIsNone(divide(1, 0))

if __name__ == "__main__":
    unittest.main()
EOF
git add . && git commit -q -m "calc with failing zero test"
"$T" --data "$DATA" init >/dev/null
"$T" --data "$DATA" instructions --dir "$REPO" --clients codex,claude,opencode --yes >/dev/null
git add . && git commit -q -m "memory protocol"

codex_args=(-C "$REPO" --skip-git-repo-check
  -c "mcp_servers.thoth-lite.command=\"$T\""
  -c "mcp_servers.thoth-lite.args=[\"mcp-stdio\",\"--data\",\"$DATA\",\"--token-file\",\"$DATA/.secrets/codex_token\"]"
  -c 'mcp_servers.thoth-lite.default_tools_approval_mode="approve"')

echo "== 1. Codex records the work and hands off"
codex exec -s workspace-write "${codex_args[@]}" -o "$SCRATCH/1-codex.txt" 'You are agent 1 of a multi-agent handoff test. Use ONLY the thoth-lite MCP tools (memory_*) for memory; do not edit any files. In this repository (origin https://github.com/thoth-lite-test/handoff-demo.git, HEAD from `git rev-parse HEAD`):
1. memory_resolve_project for that remote; if not found, memory_create_project with name "handoff-demo" and that remote.
2. memory_start_session (agent "codex", git_revision = HEAD).
3. memory_create_task titled "Make divide() safe for zero divisors".
4. memory_record_decision titled "divide(a, 0) returns None" with body "Decision: divide must return None when b == 0 and must never raise ZeroDivisionError; callers check for None. Reason: test_calc.test_zero requires None."
5. Run `python3 -m unittest` and memory_record_test_result with locator "python3 -m unittest", outcome "fail" and the decisive error line as body.
6. memory_record_issue titled "Do not use try/except returning 0" with body "Returning 0 for zero divisors was considered and rejected: it hides errors and fails test_zero."
7. memory_finish_session with a handoff: status partially completed, the fix is NOT implemented, next action: implement the decision in calc.py and rerun the tests.
Use unique operation_id values (e.g. codex-step-N). Reply with the project id and the task id.' >"$SCRATCH/1-codex.log" 2>&1
cat "$SCRATCH/1-codex.txt"

echo "== 2. Claude Code continues from memory only"
MCP="{\"mcpServers\":{\"thoth-lite\":{\"type\":\"stdio\",\"command\":\"$T\",\"args\":[\"mcp-stdio\",\"--data\",\"$DATA\",\"--token-file\",\"$DATA/.secrets/claude_token\"]}}}"
claude -p 'You are agent 2 in this repository and have no memory of earlier work. Earlier agents recorded their work in the shared thoth-lite memory (MCP tools memory_*). Continue the project: load the project context and latest handoff from memory, follow every recorded decision and avoid recorded failed approaches, implement the recorded next action, run `python3 -m unittest`, record the test result, and finish your session with an honest handoff. Do not commit. At the end, reply with one line per recorded constraint you followed.' \
  --setting-sources project,local --mcp-config "$MCP" --strict-mcp-config --permission-mode acceptEdits \
  --allowedTools "mcp__thoth-lite__*" "Bash(python3 -m unittest*)" "Bash(git rev-parse*)" "Bash(git remote*)" "Bash(git status*)" "Bash(git diff*)" "Read" "Edit" \
  >"$SCRATCH/2-claude.txt" 2>&1
cat "$SCRATCH/2-claude.txt"

echo "== 3. OpenCode reads the shared state (free model, isolated HOME)"
mkdir -p "$OCHOME"
printf '{"mcp":{"servers":{"thoth-lite":{"type":"local","command":["%s","mcp-stdio","--data","%s","--token-file","%s"]}}}}\n' "$T" "$DATA" "$DATA/.secrets/opencode_token" > "$REPO/opencode.json"
env HOME="$OCHOME" XDG_CONFIG_HOME="$OCHOME/.config" XDG_DATA_HOME="$OCHOME/.local/share" XDG_STATE_HOME="$OCHOME/.local/state" XDG_CACHE_HOME="$OCHOME/.cache" \
  opencode run --standalone --auto --archived -m "${OPENCODE_MODEL:-opencode/deepseek-v4.1-flash-free}" 'You are agent 3 with no prior knowledge. Using only the thoth-lite MCP memory tools, call memory_resolve_project for repository https://github.com/thoth-lite-test/handoff-demo.git and then memory_get_project_context for the returned project id. Then answer in exactly three lines: (1) the current decision about divide(a, 0); (2) the approach that was rejected and why; (3) the outcome of the most recent test result and which agent reported it.' \
  >"$SCRATCH/3-opencode.txt" 2>/dev/null
rm -f "$REPO/opencode.json"
cat "$SCRATCH/3-opencode.txt"

echo "== 4. A fresh Codex session reads the final state"
codex exec -s read-only "${codex_args[@]}" -o "$SCRATCH/4-codex.txt" 'You are a new agent with no memory of earlier sessions. Use only read tools of the thoth-lite MCP server (memory_resolve_project for https://github.com/thoth-lite-test/handoff-demo.git, then memory_get_project_context and memory_get_latest_handoff). Do not write anything. Answer in three lines: the task title and its state; which agents worked on it in order; the latest handoff status and its next action.' >"$SCRATCH/4-codex.log" 2>&1
cat "$SCRATCH/4-codex.txt"

echo "== checks"
fail=0
check() { if eval "$2"; then echo "PASS $1"; else echo "FAIL $1"; fail=1; fi; }
check "calc.py implements the recorded decision" "grep -q 'return None' '$REPO/calc.py' && ! grep -q 'return 0' '$REPO/calc.py'"
check "tests pass after the handoff" "(cd '$REPO' && python3 -m unittest >/dev/null 2>&1)"
P=$("$T" --data "$DATA" projects | head -1 | cut -d' ' -f1)
"$T" --data "$DATA" call get_project_context "{\"project_id\":\"$P\"}" > "$SCRATCH/context.json"
check "codex and claude sessions recorded" "grep -q '\"agent_id\": \"codex\"' '$SCRATCH/context.json' && grep -q '\"agent_id\": \"claude' '$SCRATCH/context.json'"
check "decision recorded" "grep -q 'returns None' '$SCRATCH/context.json'"
check "OpenCode read the decision" "grep -qi 'none' '$SCRATCH/3-opencode.txt'"
check "UltraLite installation untouched" "[ \"\$(live_fingerprint)\" = '$LIVE_BEFORE' ]"
exit $fail

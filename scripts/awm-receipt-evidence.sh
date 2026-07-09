#!/usr/bin/env bash
# awm-receipt-evidence.sh — advisory receipt-evidence report for a git range.
#
# For every file changed between --base and --head, reports whether an AWM run
# (a closed receipt) lists it in files_changed. ADVISORY BY DEFAULT: the exit
# code is always 0 unless --enforce (or AWM_EVIDENCE_ENFORCE=true) opts into
# failing when uncovered files remain. Never seeded into CI by init — adopters
# copy docs/examples/receipt-evidence-workflow.yml deliberately.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: scripts/awm-receipt-evidence.sh [flags]

Flags:
  --base <ref>       Base git ref of the range (default: merge-base with origin/HEAD, else HEAD~1)
  --head <ref>       Head git ref of the range (default: HEAD)
  --project <id>     AWM project id (default: awm's own resolution)
  --limit <n>        Runs to inspect, newest first (default and max: 100)
  --enforce          Exit 1 when uncovered files remain (default: report only)
  --awm-bin <path>   awm binary to invoke (default: awm on PATH; env AWM_BIN)
  -h, --help         Show this help

Environment:
  AWM_EVIDENCE_ENFORCE=true   Same as --enforce
  AWM_BIN                     Same as --awm-bin

Cheat sheet:
  # advisory report for the current branch vs origin
  scripts/awm-receipt-evidence.sh

  # enforce coverage for an explicit PR range
  scripts/awm-receipt-evidence.sh --base "$BASE_SHA" --head "$HEAD_SHA" --enforce
EOF
}

die_usage() {
	echo "awm-receipt-evidence: $1" >&2
	usage >&2
	exit 2
}

base_ref=""
head_ref="HEAD"
project_id=""
limit=100
enforce="${AWM_EVIDENCE_ENFORCE:-false}"
awm_bin="${AWM_BIN:-awm}"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--base)
		[[ $# -ge 2 ]] || die_usage "--base requires a value"
		base_ref="$2"
		shift 2
		;;
	--head)
		[[ $# -ge 2 ]] || die_usage "--head requires a value"
		head_ref="$2"
		shift 2
		;;
	--project)
		[[ $# -ge 2 ]] || die_usage "--project requires a value"
		project_id="$2"
		shift 2
		;;
	--limit)
		[[ $# -ge 2 ]] || die_usage "--limit requires a value"
		limit="$2"
		shift 2
		;;
	--enforce)
		enforce=true
		shift
		;;
	--awm-bin)
		[[ $# -ge 2 ]] || die_usage "--awm-bin requires a value"
		awm_bin="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die_usage "unknown flag $1"
		;;
	esac
done

if [[ -z "$base_ref" ]]; then
	if base_ref=$(git merge-base HEAD origin/HEAD 2>/dev/null); then
		:
	else
		base_ref="HEAD~1"
	fi
fi

changed_files=$(git diff --name-only "$base_ref" "$head_ref" --)
if [[ -z "$changed_files" ]]; then
	echo "awm-receipt-evidence: no files changed in ${base_ref}..${head_ref}"
	exit 0
fi

history_args=(history --entity run --limit "$limit" --format json)
if [[ -n "$project_id" ]]; then
	history_args+=(--project "$project_id")
fi

history_json=$(AWM_LOG_SINK=discard "$awm_bin" "${history_args[@]}")

set +e
report=$(CHANGED_FILES="$changed_files" HISTORY_JSON="$history_json" python3 - <<'PY'
import json
import os
import sys

changed = [line.strip() for line in os.environ["CHANGED_FILES"].splitlines() if line.strip()]
document = json.loads(os.environ["HISTORY_JSON"])
if document.get("ok") is False:
    error = document.get("error", {})
    print(f"awm-receipt-evidence: awm history failed: {error.get('code')}: {error.get('message')}", file=sys.stderr)
    sys.exit(3)
items = document.get("history", {}).get("items", [])

covering = {}
for item in items:
    if item.get("status") not in ("accepted", "accepted_with_warnings"):
        continue
    for path in item.get("files_changed", []) or []:
        covering.setdefault(path, item)

uncovered = 0
for path in changed:
    item = covering.get(path)
    if item is None:
        print(f"UNCOVERED  {path}")
        uncovered += 1
        continue
    actor = item.get("actor") or {}
    who = actor.get("harness", "")
    suffix = f" actor={who}" if who else ""
    print(f"covered    {path}  run={item.get('run_id')} receipt={item.get('receipt_id', '')}{suffix}")

print(f"\nawm-receipt-evidence: {len(changed) - uncovered}/{len(changed)} changed files covered by closed receipts")
sys.exit(1 if uncovered else 0)
PY
)
coverage_rc=$?
set -e

echo "$report"

if [[ $coverage_rc -gt 1 ]]; then
	echo "awm-receipt-evidence: report failed (rc=$coverage_rc)" >&2
	exit "$coverage_rc"
fi
if [[ $coverage_rc -eq 1 && "$enforce" == true ]]; then
	echo "awm-receipt-evidence: ENFORCE enabled and uncovered files remain" >&2
	exit 1
fi
exit 0

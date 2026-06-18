#!/bin/bash
# PreToolUse hook: block edits until an AWM receipt exists and require
# /awm-work once the session has expanded into multi-file changes.
#
# Receipt markers are created automatically by the PostToolUse Bash hook
# (awm-receipt-mark.sh) when a successful awm context call is detected.

set -euo pipefail

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
TARGET_FILE=$(echo "$INPUT" | jq -r '.tool_input.file_path // .tool_input.notebook_path // empty')

if [ -z "$SESSION_ID" ]; then
  exit 0
fi

STATE_DIR="/tmp/.awm-claude-{{project_id}}-${SESSION_ID}"
RECEIPT_MARKER="${STATE_DIR}/receipt"
LEGACY_RECEIPT_MARKER="/tmp/.awm-receipt-{{project_id}}-${SESSION_ID}"
WORK_MARKER="${STATE_DIR}/work"
FILES_TRACKER="${STATE_DIR}/files.txt"

deny() {
  local reason="$1"
  jq -n --arg reason "$reason" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 0
}

if [ ! -f "$RECEIPT_MARKER" ] && [ ! -f "$LEGACY_RECEIPT_MARKER" ]; then
  deny "Edit blocked: no AWM receipt for this session. Run /awm-context <phase> <task text> first."
fi

if [ -f "$WORK_MARKER" ] || [ -z "$TARGET_FILE" ] || [ ! -f "$FILES_TRACKER" ]; then
  exit 0
fi

while IFS= read -r existing_file; do
  [ -n "$existing_file" ] || continue
  if [ "$existing_file" != "$TARGET_FILE" ]; then
    deny "Edit blocked: this session is now multi-file. Run /awm-work <receipt_id-or-plan_key> <tasks-json> before continuing broad edits."
  fi
done < "$FILES_TRACKER"

exit 0

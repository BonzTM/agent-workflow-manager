#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/awm-common.sh"

ensure_jq
INPUT=$(cat)
HOOK_EVENT=$(json_get '.hook_event_name // empty')
SESSION_ID=$(json_get '.session_id // empty')
PROMPT=$(json_get '.prompt // empty')

if [ "$HOOK_EVENT" != "UserPromptSubmit" ] || [ -z "$SESSION_ID" ] || [ -z "$PROMPT" ]; then
  exit 0
fi

prepare_state_dir "$SESSION_ID"
PROMPT_LOWER=$(lowercase "$PROMPT")

if prompt_mentions_context "$PROMPT_LOWER"; then
  mark_state context_prompt_seen
fi
if prompt_mentions_done "$PROMPT_LOWER"; then
  mark_state done_prompt_seen
fi

if ! prompt_looks_like_change_work "$PROMPT_LOWER"; then
  exit 0
fi

if has_state context_prompt_seen; then
  mark_state change_prompt_seen
  exit 0
fi

emit_user_prompt_block "Before implementation-style prompts, run awm context for this task and then retry. This experimental Codex hook can only enforce prompt flow, not verify CLI state directly."

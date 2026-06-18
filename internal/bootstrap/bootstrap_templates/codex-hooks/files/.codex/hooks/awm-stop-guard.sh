#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/awm-common.sh"

ensure_jq
INPUT=$(cat)
HOOK_EVENT=$(json_get '.hook_event_name // empty')
SESSION_ID=$(json_get '.session_id // empty')
STOP_HOOK_ACTIVE=$(json_get '.stop_hook_active // false')

if [ "$HOOK_EVENT" != "Stop" ] || [ -z "$SESSION_ID" ] || [ "$STOP_HOOK_ACTIVE" = "true" ]; then
  exit 0
fi

prepare_state_dir "$SESSION_ID"

if ! has_state change_prompt_seen || has_state done_prompt_seen || has_state stop_warned; then
  exit 0
fi

mark_state stop_warned
emit_stop_block "Stop paused once: this session has seen implementation-style prompts without an explicit awm done closeout marker. If the task is really finished, run awm verify for verify-sensitive work, then awm done, and stop again. If you already closed the task outside the prompt flow, you can ignore this one-time reminder."

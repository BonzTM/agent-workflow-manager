#!/bin/bash

set -euo pipefail

ensure_jq() {
  command -v jq >/dev/null 2>&1 || exit 0
}

json_get() {
  local query="$1"
  printf '%s' "$INPUT" | jq -r "$query"
}

lowercase() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

state_dir_for() {
  printf '/tmp/.awm-codex-{{project_id}}-%s' "$1"
}

prepare_state_dir() {
  local session_id="$1"
  STATE_DIR="$(state_dir_for "$session_id")"
  mkdir -p "$STATE_DIR"
}

state_path() {
  printf '%s/%s' "$STATE_DIR" "$1"
}

mark_state() {
  : > "$(state_path "$1")"
}

has_state() {
  [ -f "$(state_path "$1")" ]
}

append_line() {
  local line="$1"
  if [ -z "${MESSAGE:-}" ]; then
    MESSAGE="$line"
  else
    MESSAGE="${MESSAGE}
$line"
  fi
}

prompt_mentions_context() {
  local prompt="$1"
  case "$prompt" in
    *"awm context"*|*"run context"*|*"ran context"*|*"refresh context"*|*"refreshed context"*|*"got the receipt"*|*"have the receipt"*)
      return 0
      ;;
  esac
  return 1
}

prompt_mentions_done() {
  local prompt="$1"
  case "$prompt" in
    *"awm done"*|*"run done"*|*"ran done"*|*"closed with done"*|*"finish with done"*|*"finished with done"*)
      return 0
      ;;
  esac
  return 1
}

prompt_looks_like_change_work() {
  local prompt="$1"
  case "$prompt" in
    *"implement "*|*"implement:"*|*"build "*|*"build this"*|*"fix "*|*"edit "*|*"update "*|*"change "*|*"refactor "*|*"rename "*|*"remove "*|*"delete "*|*"wire "*|*"scaffold "*|*"add "*|*"patch "*|*"modify "*)
      return 0
      ;;
  esac
  return 1
}

emit_additional_context() {
  local event="$1"
  local context="$2"
  jq -n --arg event "$event" --arg context "$context" '{
    hookSpecificOutput: {
      hookEventName: $event,
      additionalContext: $context
    }
  }'
  exit 0
}

emit_user_prompt_block() {
  local reason="$1"
  jq -n --arg reason "$reason" '{
    decision: "block",
    reason: $reason
  }'
  exit 0
}

emit_stop_block() {
  local reason="$1"
  jq -n --arg reason "$reason" '{
    decision: "block",
    reason: $reason
  }'
  exit 0
}

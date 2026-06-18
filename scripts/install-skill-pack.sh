#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Install awm-broker skill assets for Codex, Claude, and OpenCode.

Usage:
  scripts/install-skill-pack.sh [options]

Options:
  --codex [<path>]        Install Codex skill pack only. Optional path sets Codex home
                           (default: $CODEX_HOME or ~/.codex)
  --claude [<path>]       Install Claude command pack only. Optional path sets target repo
                           (default: current directory)
  --opencode [<path>]     Install OpenCode repo-local companion docs only. Optional
                           path sets target repo (default: current directory)
  -h, --help              Show help

Examples:
  scripts/install-skill-pack.sh
  scripts/install-skill-pack.sh --claude
  scripts/install-skill-pack.sh --claude /path/to/project
  scripts/install-skill-pack.sh --codex
  scripts/install-skill-pack.sh --codex /path/to/codex-home
  scripts/install-skill-pack.sh --opencode
  scripts/install-skill-pack.sh --opencode /path/to/project
  scripts/install-skill-pack.sh --codex --claude /path/to/project
USAGE
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)"
REPO_ROOT=""
if [[ -n "${SCRIPT_DIR}" && -d "${SCRIPT_DIR}/../skills/awm-broker" ]]; then
  REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi

codex_home="${CODEX_HOME:-${HOME}/.codex}"
claude_target="$(pwd)"
opencode_target="$(pwd)"
install_codex=true
install_claude=true
install_opencode=false
selection_explicit=false

select_target() {
  local target="$1"
  if [[ "${selection_explicit}" == false ]]; then
    install_codex=false
    install_claude=false
    install_opencode=false
    selection_explicit=true
  fi
  if [[ "${target}" == "codex" ]]; then
    install_codex=true
  elif [[ "${target}" == "claude" ]]; then
    install_claude=true
  else
    install_opencode=true
  fi
}

install_codex_skill() {
  local codex_skills_dir="$1"
  local codex_target="$2"
  local stage_root stage_target backup_root backup_target

  stage_root="$(mktemp -d "${codex_skills_dir}/.awm-broker.stage.XXXXXX")"
  stage_target="${stage_root}/awm-broker"
  backup_root=""
  backup_target=""

  cp -R "${skill_src}" "${stage_target}"

  if [[ -e "${codex_target}" ]]; then
    backup_root="$(mktemp -d "${codex_skills_dir}/.awm-broker.backup.XXXXXX")"
    backup_target="${backup_root}/awm-broker"
    if ! mv "${codex_target}" "${backup_target}"; then
      rm -rf "${stage_root}" "${backup_root}"
      echo "error: failed to preserve existing Codex skill at ${codex_target}" >&2
      return 1
    fi
  fi

  if mv "${stage_target}" "${codex_target}"; then
    rmdir "${stage_root}" 2>/dev/null || rm -rf "${stage_root}"
    if [[ -n "${backup_root}" ]]; then
      rm -rf "${backup_root}"
    fi
    return 0
  fi

  echo "error: failed to install Codex skill at ${codex_target}" >&2
  rm -rf "${stage_root}"
  if [[ -n "${backup_target}" && ! -e "${codex_target}" ]]; then
    mv "${backup_target}" "${codex_target}" || true
  fi
  if [[ -n "${backup_root}" ]]; then
    rm -rf "${backup_root}"
  fi
  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --codex)
      select_target codex
      if [[ $# -ge 2 && "$2" != -* ]]; then
        codex_home="$2"
        shift 2
      else
        shift
      fi
      ;;
    --codex=*)
      select_target codex
      codex_home="${1#--codex=}"
      shift
      ;;
    --claude)
      select_target claude
      if [[ $# -ge 2 && "$2" != -* ]]; then
        claude_target="$2"
        shift 2
      else
        shift
      fi
      ;;
    --claude=*)
      select_target claude
      claude_target="${1#--claude=}"
      shift
      ;;
    --opencode)
      select_target opencode
      if [[ $# -ge 2 && "$2" != -* ]]; then
        opencode_target="$2"
        shift 2
      else
        shift
      fi
      ;;
    --opencode=*)
      select_target opencode
      opencode_target="${1#--opencode=}"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "${install_codex}" == false && "${install_claude}" == false && "${install_opencode}" == false ]]; then
  echo "error: no install targets selected" >&2
  exit 2
fi

cleanup_tmp() { :; }
if [[ -n "${REPO_ROOT}" ]]; then
  skill_src="${REPO_ROOT}/skills/awm-broker"
else
  echo "Fetching skill pack from GitHub..."
  tmp_dir="$(mktemp -d)"
  cleanup_tmp() { rm -rf "${tmp_dir}"; }
  trap cleanup_tmp EXIT
  git clone --depth 1 --filter=blob:none --sparse \
    https://github.com/bonztm/agent-workflow-manager.git "${tmp_dir}/repo"
  git -C "${tmp_dir}/repo" sparse-checkout set skills/awm-broker
  skill_src="${tmp_dir}/repo/skills/awm-broker"
fi
if [[ ! -d "${skill_src}" ]]; then
  echo "error: skill source directory not found: ${skill_src}" >&2
  exit 1
fi

if [[ "${install_codex}" == true ]]; then
  codex_skills_dir="${codex_home}/skills"
  codex_target="${codex_skills_dir}/awm-broker"
  mkdir -p "${codex_skills_dir}"
  install_codex_skill "${codex_skills_dir}" "${codex_target}"
  echo "Installed Codex skill: ${codex_target}"
  if [[ -d "${codex_target}/codex" ]]; then
    echo "Installed Codex companion docs: ${codex_target}/codex"
  fi
  echo "Optional repo-local Codex companion: awm init --apply-template codex-pack"
  echo "Optional experimental Codex hooks: awm init --apply-template codex-hooks"
 fi

if [[ "${install_claude}" == true ]]; then
  claude_target="$(cd "${claude_target}" && pwd)"
  claude_dir="${claude_target}/.claude"
  claude_commands_dir="${claude_dir}/commands"
  claude_pack_dir="${claude_dir}/awm-broker"

  mkdir -p "${claude_commands_dir}" "${claude_pack_dir}"
  cp "${skill_src}/claude/commands/"*.md "${claude_commands_dir}/"
  cp "${skill_src}/claude/CLAUDE.md" "${claude_pack_dir}/CLAUDE.md"
  cp "${skill_src}/claude/README.md" "${claude_pack_dir}/README.md"

  echo "Installed Claude commands: ${claude_commands_dir}"
  echo "Installed Claude companion docs: ${claude_pack_dir}"
fi

if [[ "${install_opencode}" == true ]]; then
  opencode_target="$(cd "${opencode_target}" && pwd)"
  opencode_pack_dir="${opencode_target}/.opencode/awm-broker"

  mkdir -p "${opencode_pack_dir}"
  cp "${skill_src}/opencode/AGENTS.example.md" "${opencode_pack_dir}/AGENTS.example.md"
  cp "${skill_src}/opencode/README.md" "${opencode_pack_dir}/README.md"

  echo "Installed OpenCode companion docs: ${opencode_pack_dir}"
  echo "OpenCode install is docs-only; use normal awm CLI/MCP access from the repo."
  echo "Optional repo-local OpenCode companion via init: awm init --apply-template opencode-pack"
fi

echo "Done. Restart Codex and/or Claude Code, or reload OpenCode if it caches companion docs, to load the installed assets."

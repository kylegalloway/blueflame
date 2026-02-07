package agent

const watcherTemplateSource = `#!/usr/bin/env bash
# Generated watcher hook for agent {{.AgentID}} (role: {{.Role}})
# This script is registered as a PreToolUse hook in .claude/settings.json.
# It receives tool invocation JSON on stdin and outputs a JSON decision.
#
# Exit codes:
#   0 = allow (with JSON output)
#   2 = block (with JSON output containing reason)

set -euo pipefail

AUDIT_LOG="{{.AuditLogPath}}"
mkdir -p "$(dirname "$AUDIT_LOG")"

# Read the tool invocation from stdin
INPUT=$(cat)

TOOL=$(echo "$INPUT" | jq -r '.tool_name // empty')
# For file operations, extract the path
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // .tool_input.path // empty')
# For Bash, extract the command
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

log_decision() {
    local decision="$1"
    local rule="$2"
    local details="${3:-}"
    echo "{\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"agent_id\":\"{{.AgentID}}\",\"tool\":\"$TOOL\",\"target\":\"${FILE_PATH:-$COMMAND}\",\"decision\":\"$decision\",\"rule\":\"$rule\",\"details\":\"$details\"}" >> "$AUDIT_LOG"
}

block() {
    local reason="$1"
    local rule="$2"
    log_decision "block" "$rule" "$reason"
    echo "{\"decision\":\"block\",\"reason\":\"$reason\"}"
    exit 2
}

allow() {
    local rule="${1:-allowed}"
    log_decision "allow" "$rule"
    echo "{\"decision\":\"allow\"}"
    exit 0
}

# --- Tool Allowlist/Blocklist ---
{{range .BlockedTools}}
if [ "$TOOL" = "{{.}}" ]; then
    block "Tool {{.}} is blocked for this agent" "tool_blocked"
fi
{{end}}

TOOL_ALLOWED=false
{{range .AllowedTools}}
if [ "$TOOL" = "{{.}}" ]; then
    TOOL_ALLOWED=true
fi
{{end}}
if [ "$TOOL_ALLOWED" = "false" ] && [ -n "$TOOL" ]; then
    block "Tool $TOOL is not in the allowlist" "tool_not_allowed"
fi

# --- Path Checks (for file-modifying tools) ---
if [ -n "$FILE_PATH" ]; then
    # Check blocked paths
    {{range .BlockedPaths}}
    case "$FILE_PATH" in
        {{.}}) block "Path $FILE_PATH matches blocked pattern '{{.}}'" "blocked_path" ;;
    esac
    {{end}}

    # Check file scope (task file_locks)
    {{if .FileLocks}}
    FILE_IN_SCOPE=false
    {{range .FileLocks}}
    case "$FILE_PATH" in
        {{.}}*) FILE_IN_SCOPE=true ;;
    esac
    {{end}}
    if [ "$FILE_IN_SCOPE" = "false" ] && [ "$TOOL" != "Read" ] && [ "$TOOL" != "Glob" ] && [ "$TOOL" != "Grep" ]; then
        block "Path $FILE_PATH is outside task file_locks" "outside_file_scope"
    fi
    {{end}}
fi

# --- Bash Command Checks ---
if [ "$TOOL" = "Bash" ] && [ -n "$COMMAND" ]; then
    {{if eq .Role "validator"}}
    # Validators: restricted to diagnostic commands only
    CMD_ALLOWED=false
    {{range .DiagnosticCommands}}
    case "$COMMAND" in
        "{{.}}"*) CMD_ALLOWED=true ;;
    esac
    {{end}}
    if [ "$CMD_ALLOWED" = "false" ]; then
        block "Validator Bash restricted to diagnostic commands only" "validator_bash_restricted"
    fi
    {{else}}
    # Workers/other: check against allowed commands
    CMD_ALLOWED=false
    {{range .AllowedCommands}}
    case "$COMMAND" in
        "{{.}}"*) CMD_ALLOWED=true ;;
    esac
    {{end}}
    if [ "$CMD_ALLOWED" = "false" ]; then
        block "Command not in allowlist: $COMMAND" "bash_not_allowed"
    fi

    # Check blocked patterns
    {{range .BlockedPatterns}}
    if echo "$COMMAND" | grep -qE '{{.}}'; then
        block "Command matches blocked pattern" "bash_blocked_pattern"
    fi
    {{end}}
    {{end}}
fi

# All checks passed
allow "all_checks_passed"
`

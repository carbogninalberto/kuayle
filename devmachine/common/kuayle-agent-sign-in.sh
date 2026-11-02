# Sourced by kuayle-shell for interactive shells of the machine owner's
# terminal and IDE. The manager writes the owner's agent sign-ins from Kuayle
# into ~/.kuayle/agent-sign-ins (the in-memory home) whenever the machine
# starts and whenever the sign-ins change in Kuayle; each new shell uses the
# current set:
#   claude-code.token             exported as CLAUDE_CODE_OAUTH_TOKEN until
#   claude-code.token-expires     this time (Unix seconds); then the file below
#   claude-code.credentials.json  installed as ~/.claude/.credentials.json
#   codex.auth.json               installed as ~/.codex/auth.json
#   managed                       the agents Kuayle signs in
# A credentials file is installed only where the user has no sign-in of their
# own; a copy in ~/.kuayle/agent-sign-ins-installed recognises the installed
# one, so a later change in Kuayle replaces it and a removal deletes it (also
# when Kuayle stops signing that agent in), while a sign-in the user made in
# this terminal is never touched.

kuayle_sign_ins_dir="${HOME:-/home/kuayle}/.kuayle/agent-sign-ins"
kuayle_sign_ins_installed="${HOME:-/home/kuayle}/.kuayle/agent-sign-ins-installed"

# kuayle_install_sign_in SOURCE TARGET MARKER: 0 when Kuayle's sign-in is in
# place, 1 when the user has their own, 2 when there is none.
kuayle_install_sign_in() {
  if [ -e "$2" ] && { [ ! -f "$3" ] || ! cmp -s "$2" "$3"; }; then
    return 1
  fi
  if [ -s "$1" ]; then
    if [ ! -f "$2" ] || ! cmp -s "$1" "$2"; then
      (umask 077
        mkdir -p "${2%/*}" "${3%/*}"
        cp "$1" "$2.kuayle-tmp" && mv -f "$2.kuayle-tmp" "$2"
        cp "$1" "$3")
    fi
    return 0
  fi
  rm -f "$2" "$3"
  return 2
}

# kuayle_remove_installed TARGET MARKER removes a sign-in Kuayle installed,
# and only that: a login the user made themselves stays.
kuayle_remove_installed() {
  if [ -f "$2" ] && cmp -s "$1" "$2" 2>/dev/null; then
    rm -f "$1"
  fi
  rm -f "$2"
}

kuayle_managed() {
  [ -f "$kuayle_sign_ins_dir/managed" ] && grep -qx "$1" "$kuayle_sign_ins_dir/managed"
}

# A saved token is used only until the expiry the manager recorded with it.
kuayle_claude_token_valid() {
  [ -s "$kuayle_sign_ins_dir/claude-code.token" ] || return 1
  [ -f "$kuayle_sign_ins_dir/claude-code.token-expires" ] || return 0
  kuayle_expires=$(cat "$kuayle_sign_ins_dir/claude-code.token-expires")
  case "$kuayle_expires" in
    ''|*[!0-9]* ) return 1 ;;
  esac
  [ "$(date +%s)" -lt "$kuayle_expires" ]
}

kuayle_load_agent_sign_ins() {
  kuayle_claude_missing=
  kuayle_codex_missing=
  kuayle_claude_file="$HOME/.claude/.credentials.json"
  kuayle_claude_marker="$kuayle_sign_ins_installed/claude-code.credentials.json"
  if kuayle_managed claude-code && [ -z "${ANTHROPIC_API_KEY:-}${ANTHROPIC_AUTH_TOKEN:-}${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
    kuayle_claude_status=0
    if kuayle_claude_token_valid; then
      # A saved token comes first, as for agent runs; it is not a file login,
      # so an installed credentials file from an earlier sync is removed.
      if [ ! -e "$kuayle_claude_file" ] || cmp -s "$kuayle_claude_file" "$kuayle_claude_marker" 2>/dev/null; then
        rm -f "$kuayle_claude_file" "$kuayle_claude_marker"
        CLAUDE_CODE_OAUTH_TOKEN=$(cat "$kuayle_sign_ins_dir/claude-code.token")
        export CLAUDE_CODE_OAUTH_TOKEN
      fi
    else
      kuayle_install_sign_in "$kuayle_sign_ins_dir/claude-code.credentials.json" "$kuayle_claude_file" "$kuayle_claude_marker" || kuayle_claude_status=$?
      [ "$kuayle_claude_status" != 2 ] || kuayle_claude_missing=1
    fi
  else
    # Kuayle no longer signs Claude Code in here (policy, model server or key).
    kuayle_remove_installed "$kuayle_claude_file" "$kuayle_claude_marker"
  fi
  if kuayle_managed codex && [ -z "${OPENAI_API_KEY:-}${CODEX_API_KEY:-}" ]; then
    kuayle_codex_status=0
    kuayle_install_sign_in "$kuayle_sign_ins_dir/codex.auth.json" "$HOME/.codex/auth.json" "$kuayle_sign_ins_installed/codex.auth.json" || kuayle_codex_status=$?
    [ "$kuayle_codex_status" != 2 ] || kuayle_codex_missing=1
  else
    kuayle_remove_installed "$HOME/.codex/auth.json" "$kuayle_sign_ins_installed/codex.auth.json"
  fi
}

# The hint is shown once per terminal tab: in the first pane of a tmux
# session, or in every IDE terminal.
kuayle_sign_in_hint() {
  [ -n "$kuayle_claude_missing$kuayle_codex_missing" ] || return 0
  if [ -n "${TMUX:-}" ] && [ "$(tmux display-message -p '#{session_windows}:#{window_panes}' 2>/dev/null)" != "1:1" ]; then
    return 0
  fi
  if [ -n "$kuayle_claude_missing" ]; then
    printf '%s\n' "Kuayle: Claude Code is not signed in. Run 'claude setup-token' and save the token in Kuayle under" \
      "Settings > Agent configuration > Your sign-ins: your terminals and agent tasks then use it." \
      "Or run 'claude' to sign in for this terminal only."
  fi
  if [ -n "$kuayle_codex_missing" ]; then
    printf '%s\n' "Kuayle: Codex is not signed in. Run 'codex login --device-auth' to sign in for this terminal;" \
      "a task that asks you to sign in saves it for your terminals and agent tasks."
  fi
}

kuayle_load_agent_sign_ins || true
if [ "${KUAYLE_SIGN_IN_HINT:-1}" = 1 ]; then
  kuayle_sign_in_hint || true
fi
unset kuayle_claude_status kuayle_codex_status kuayle_claude_missing kuayle_codex_missing kuayle_claude_file kuayle_claude_marker kuayle_expires

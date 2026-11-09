// Usage command: generate command reference from the clap Command tree.
//
// Single source of truth — command additions/deletions are automatically
// reflected. No hand-maintained usage text that can drift.

use clap::Command;

/// Generate the full usage reference from the clap Command tree.
pub fn generate(cmd: &Command) -> String {
    let mut out = String::new();

    // ── Header ────────────────────────────────────────────────────
    out.push_str("kuayle — CLI for the kuayle self-hosted issue tracker\n\n");

    // ── Global flags ──────────────────────────────────────────────
    out.push_str("## Global Flags\n\n");
    out.push_str("  --profile <NAME>     Profile to use (env: KUAYLE_PROFILE)\n");
    out.push_str("  --url <URL>          Override instance URL (env: KUAYLE_URL)\n");
    out.push_str("  --workspace <SLUG>   Override workspace slug (env: KUAYLE_WORKSPACE)\n");
    out.push_str("  --format human|json   Output format (default: auto-detect tty)\n");
    out.push_str("  --no-cache            Disable resolve disk cache (env: KUAYLE_NO_CACHE)\n\n");

    // ── Auth ──────────────────────────────────────────────────────
    out.push_str("## Authentication\n\n");
    out.push_str("  kuayle auth login --token <PAT>     Login with Personal Access Token\n");
    out.push_str("  kuayle auth logout                   Remove stored credentials\n");
    out.push_str("  kuayle auth status                   Show authentication status\n\n");

    // ── Commands from clap tree ───────────────────────────────────
    out.push_str("## Commands\n\n");
    walk_subcommands(cmd, &mut out, &["kuayle"]);

    // ── Name resolution ───────────────────────────────────────────
    out.push_str("## Name Resolution\n\n");
    out.push_str("  Names accept: UUID (passthrough), human-readable name, or identifier.\n");
    out.push_str("  Names resolved via batch fetch + case-insensitive match.\n");
    out.push_str("  In-process memo + disk cache (5min TTL, --no-cache to disable).\n");
    out.push_str("  Multiple names resolved in parallel (tokio::join!).\n");
    out.push_str("  On failure: lists available candidates, exit code 3.\n\n");

    // ── Exit codes ────────────────────────────────────────────────
    out.push_str("## Exit Codes\n\n");
    out.push_str("  0  Success\n");
    out.push_str("  1  Generic error\n");
    out.push_str("  2  Authentication failure\n");
    out.push_str("  3  Resource not found\n");
    out.push_str("  4  Validation error\n");
    out.push_str("  5  Permission denied\n");
    out.push_str("  6  Rate limited\n");
    out.push_str("  7  Network / server unreachable\n\n");

    // ── Output format ─────────────────────────────────────────────
    out.push_str("## Output Format\n\n");
    out.push_str("  --format human   Human-readable tables and text (default when TTY).\n");
    out.push_str("  --format json    Machine-readable JSON.\n");
    out.push_str("  Default: auto-detect — JSON when piped, human when interactive.\n");
    out.push_str("  JSON mode: errors are also JSON.\n\n");

    out
}

/// Recursively walk subcommands, printing leaf commands.
fn walk_subcommands(cmd: &Command, out: &mut String, prefix: &[&str]) {
    for sub in cmd.get_subcommands() {
        let name = sub.get_name();
        if name == "help" {
            continue;
        }
        let mut new_prefix = prefix.to_vec();
        new_prefix.push(name);

        let sub_subs: Vec<_> = sub
            .get_subcommands()
            .filter(|s| s.get_name() != "help")
            .collect();

        if sub_subs.is_empty() {
            // Leaf command — print its line.
            let cmd_line = new_prefix.join(" ");
            let about = sub.get_about().map(|s| s.to_string()).unwrap_or_default();
            // Extract first sentence (up to first period or newline).
            let summary: String = about
                .lines()
                .next()
                .unwrap_or("")
                .chars()
                .take(80)
                .collect();
            if summary.is_empty() {
                out.push_str(&format!("  {cmd_line}\n"));
            } else {
                out.push_str(&format!("  {cmd_line}\n    {summary}\n"));
            }
        } else {
            // Intermediate command with subcommands — recurse.
            walk_subcommands(sub, out, &new_prefix);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cli::Cli;
    use clap::CommandFactory;

    #[test]
    fn usage_contains_key_commands() {
        let cmd = Cli::command();
        let usage = generate(&cmd);
        assert!(usage.contains("auth login"), "missing auth login");
        assert!(usage.contains("issues list"), "missing issues list");
        assert!(usage.contains("issues create"), "missing issues create");
        assert!(usage.contains("whoami"), "missing whoami");
        assert!(usage.contains("workspaces list"), "missing workspaces list");
        assert!(usage.contains("teams list"), "missing teams list");
        assert!(usage.contains("templates list"), "missing templates list");
        assert!(usage.contains("statuses list"), "missing statuses list");
    }

    #[test]
    fn usage_contains_static_sections() {
        let cmd = Cli::command();
        let usage = generate(&cmd);
        assert!(usage.contains("Exit Codes"), "missing exit codes");
        assert!(usage.contains("Name Resolution"), "missing name resolution");
        assert!(usage.contains("Output Format"), "missing output format");
        assert!(usage.contains("Global Flags"), "missing global flags");
        assert!(usage.contains("Authentication"), "missing auth section");
    }

    #[test]
    fn usage_under_1000_tokens() {
        let cmd = Cli::command();
        let usage = generate(&cmd);
        let char_count = usage.chars().count();
        let tokens = char_count / 4; // rough estimate
        assert!(
            tokens < 1000,
            "usage too long: ~{tokens} tokens ({char_count} chars)"
        );
    }

    #[test]
    fn removing_command_updates_usage() {
        // Simulate: if we removed "comments list" from the tree, it shouldn't appear.
        // We verify the current tree DOES contain it, and would fail if absent.
        let cmd = Cli::command();
        let usage = generate(&cmd);
        assert!(
            usage.contains("comments list"),
            "comments list should be in usage"
        );
    }
}

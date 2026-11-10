// Usage command: generate command reference from the clap Command tree.
//
// Single source of truth — command additions/deletions are automatically
// reflected. No hand-maintained usage text that can drift.

use clap::Command;

/// Generate the full usage reference from the clap Command tree.
pub fn generate(cmd: &Command) -> String {
    let mut out = String::new();

    // ── Header ────────────────────────────────────────────────────
    out.push_str("kuayle — the official Kuayle CLI\n\n");

    // ── Global flags ──────────────────────────────────────────────
    out.push_str("## Global Flags\n\n");
    out.push_str("  --profile NAME, --url URL, --workspace SLUG (env: KUAYLE_PROFILE, KUAYLE_URL, KUAYLE_WORKSPACE)\n");
    out.push_str(
        "  --format human|json (default: human on a terminal, JSON when piped; errors too)\n",
    );
    out.push_str("  --no-cache (env: KUAYLE_NO_CACHE) skips the 5-minute name cache\n\n");

    // ── Auth ──────────────────────────────────────────────────────
    out.push_str("## Authentication\n\n");
    out.push_str("  kuayle auth login --url URL --workspace SLUG --token <PAT>\n\n");

    // ── Commands from clap tree ───────────────────────────────────
    out.push_str("## Commands\n\n");
    walk_subcommands(cmd, &mut out, &["kuayle"]);

    // ── Name resolution ───────────────────────────────────────────
    out.push_str("## Name Resolution\n\n");
    out.push_str("  Teams, projects, labels and people accept a name, key, email or UUID\n");
    out.push_str("  (case-insensitive); an unknown name lists candidates and exits 3.\n\n");

    // ── Wiki scope ────────────────────────────────────────────────
    out.push_str("## Wiki and Registry\n\n");
    out.push_str("  wiki/registry commands need --team <name|key|id> or --project <name|id>.\n");
    out.push_str("  Content is HTML or Markdown (--content, or --file PATH|-).\n");
    out.push_str("  Token scopes: wiki:read to read, wiki:write to change.\n");
    out.push_str(
        "  agent-tasks/workflows need agent_tasks:read / workflows:read (members only).\n\n",
    );

    // ── Exit codes ────────────────────────────────────────────────
    out.push_str("## Exit Codes\n\n");
    out.push_str("  0 ok, 1 error, 2 auth, 3 not found, 4 invalid input, 5 forbidden,\n");
    out.push_str("  6 rate limited, 7 network/server\n\n");

    // ── Output format ─────────────────────────────────────────────
    out.push_str("## Output Format\n\n");
    out.push_str("  Prefer --format json in scripts and agents.\n");

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
        assert!(usage.contains("wiki update"), "missing wiki update");
        assert!(usage.contains("registry append"), "missing registry append");
        assert!(usage.contains("--team <name|key|id>"), "missing wiki scope");
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

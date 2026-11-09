// Wiki and registry commands for team and project wikis.
//
// Every call goes through the REST API with the token's scopes:
// reads need wiki:read, writes wiki:write. Teams and projects are resolved
// by name, key or UUID with the shared Resolver.

use std::io::Read;

use kuayle_sdk::error::KuayleError;
use kuayle_sdk::resources::wiki::{Wiki, WikiScope};
use kuayle_sdk::types::wiki::{
    CreateWikiPageRequest, CreateWikiTreePageRequest, GenerateOverviewRequest, MoveWikiPageRequest,
    UpdateWikiPageRequest, UpdateWikiSettingsRequest, WikiPage,
};
use serde::Serialize;

use crate::cli::{Cli, RegistryAction, WikiAction, WikiContentArgs, WikiScopeArgs};
use crate::output::{self, is_json_output, Format};
use crate::resolve::{ResolveKind, Resolver};

struct Ctx {
    wiki: Wiki,
    is_json: bool,
}

fn fail(err: KuayleError, is_json: bool) -> ! {
    output::print_error(&err, is_json);
    std::process::exit(err.exit_code());
}

fn format(is_json: bool) -> Format {
    if is_json {
        Format::Json
    } else {
        Format::Human
    }
}

fn print_json<T: Serialize>(value: &T) {
    println!(
        "{}",
        serde_json::to_string_pretty(value).unwrap_or_default()
    );
}

async fn setup(cli: &Cli, scope: &WikiScopeArgs) -> Ctx {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = crate::commands::workspace_slug(cli);
    let resolver = Resolver::new(client.clone(), &ws, cli.no_cache);
    let resolved = match (&scope.team, &scope.project) {
        (Some(team), _) => resolver
            .resolve(ResolveKind::Teams, team)
            .await
            .map(WikiScope::Team),
        (None, Some(project)) => resolver
            .resolve(ResolveKind::Projects, project)
            .await
            .map(WikiScope::Project),
        // clap's argument group requires exactly one of the two.
        (None, None) => Err("pass --team or --project".to_string()),
    };
    let scope = match resolved {
        Ok(s) => s,
        Err(e) => output::print_string_error(&e, 3, is_json),
    };
    Ctx {
        wiki: Wiki::new(&client, &ws, &scope),
        is_json,
    }
}

/// The content from --content or --file ("-" for stdin), if either is given.
fn read_content(body: &WikiContentArgs, is_json: bool) -> Option<String> {
    if let Some(content) = &body.content {
        return Some(content.clone());
    }
    let path = body.file.as_ref()?;
    let result = if path == "-" {
        let mut buf = String::new();
        std::io::stdin().read_to_string(&mut buf).map(|_| buf)
    } else {
        std::fs::read_to_string(path)
    };
    match result {
        Ok(text) => Some(text),
        Err(e) => output::print_string_error(&format!("read {path}: {e}"), 4, is_json),
    }
}

fn print_page(page: &WikiPage, is_json: bool) {
    if is_json {
        print_json(page);
        return;
    }
    let s = &page.summary;
    println!("Title:    {}", s.title);
    println!("ID:       {}", s.id);
    println!("Version:  {}", s.version);
    if let Some(day) = &s.registry_date {
        println!("Registry: {day}");
    }
    println!(
        "Updated:  {}{}",
        s.updated_at.format("%Y-%m-%d %H:%M UTC"),
        s.updated_by_name
            .as_ref()
            .map(|n| format!(" by {n}"))
            .unwrap_or_default()
    );
    println!();
    println!("{}", page.content);
}

pub async fn handle(action: &WikiAction, cli: &Cli) {
    match action {
        WikiAction::List { scope, registry } => {
            let ctx = setup(cli, scope).await;
            let mut pages = ctx
                .wiki
                .list()
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if !registry {
                pages.retain(|p| p.registry_date.is_none());
            }
            output::print_table(
                format(ctx.is_json),
                &pages,
                &["ID", "TITLE", "VERSION", "UPDATED"],
                |p| {
                    vec![
                        p.id.clone(),
                        p.title.clone(),
                        p.version.to_string(),
                        p.updated_at.format("%Y-%m-%d").to_string(),
                    ]
                },
            );
        }
        WikiAction::Tree { scope } => {
            let ctx = setup(cli, scope).await;
            let nodes = ctx
                .wiki
                .tree()
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&nodes);
                return;
            }
            // Titles come from the page list; the tree carries only placement.
            let pages = ctx
                .wiki
                .list()
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            let title = |id: &str| {
                pages
                    .iter()
                    .find(|p| p.id == id)
                    .map(|p| p.title.clone())
                    .unwrap_or_else(|| "(untitled)".into())
            };
            print!("{}", render_tree(&nodes, &title));
        }
        WikiAction::Read { page, scope } => {
            let ctx = setup(cli, scope).await;
            let page = ctx
                .wiki
                .get(page)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            print_page(&page, ctx.is_json);
        }
        WikiAction::Create {
            scope,
            title,
            body,
            parent,
            folder,
        } => {
            let ctx = setup(cli, scope).await;
            let content = read_content(body, ctx.is_json).unwrap_or_default();
            let result = if parent.is_some() || *folder {
                let req = CreateWikiTreePageRequest {
                    title: title.clone(),
                    content,
                    kind: if *folder { "folder" } else { "page" }.to_string(),
                    parent_id: parent.clone(),
                };
                ctx.wiki.create_in_tree(&req).await
            } else {
                let req = CreateWikiPageRequest {
                    title: title.clone(),
                    content,
                };
                ctx.wiki.create(&req).await
            };
            let page = result.unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&page);
            } else {
                println!("✓ Created \"{}\" ({})", page.summary.title, page.summary.id);
            }
        }
        WikiAction::Update {
            page,
            scope,
            title,
            body,
            version,
        } => {
            let ctx = setup(cli, scope).await;
            let content = read_content(body, ctx.is_json);
            if title.is_none() && content.is_none() {
                output::print_string_error(
                    "nothing to update: pass --title, --content or --file",
                    4,
                    ctx.is_json,
                );
            }
            // Without --version, base the edit on the page as it is now. The
            // server still rejects it if someone saves in between.
            let version = match version {
                Some(v) => *v,
                None => {
                    ctx.wiki
                        .get(page)
                        .await
                        .unwrap_or_else(|e| fail(e, ctx.is_json))
                        .summary
                        .version
                }
            };
            let req = UpdateWikiPageRequest {
                title: title.clone(),
                content,
                version,
            };
            let saved = ctx
                .wiki
                .update(page, &req)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&saved);
            } else {
                println!(
                    "✓ Updated \"{}\" (version {})",
                    saved.summary.title, saved.summary.version
                );
            }
        }
        WikiAction::Delete { page, scope } => {
            let ctx = setup(cli, scope).await;
            ctx.wiki
                .delete(page)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                println!(r#"{{"deleted":true}}"#);
            } else {
                println!("✓ Deleted wiki page {page}");
            }
        }
        WikiAction::Move {
            page,
            scope,
            parent,
            index,
        } => {
            let ctx = setup(cli, scope).await;
            let req = MoveWikiPageRequest {
                parent_id: parent.clone(),
                index: *index,
            };
            let nodes = ctx
                .wiki
                .move_page(page, &req)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&nodes);
            } else {
                println!("✓ Moved wiki page {page}");
            }
        }
        WikiAction::History { page, scope } => {
            let ctx = setup(cli, scope).await;
            let revisions = ctx
                .wiki
                .revisions(page)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            output::print_table(
                format(ctx.is_json),
                &revisions,
                &["VERSION", "EDITED", "BY", "TITLE"],
                |r| {
                    vec![
                        r.version.to_string(),
                        r.updated_at.format("%Y-%m-%d %H:%M").to_string(),
                        r.edited_by_name.clone().unwrap_or_else(|| "-".into()),
                        r.new_title.clone(),
                    ]
                },
            );
        }
        WikiAction::Restore {
            page,
            scope,
            version,
            before,
        } => {
            let ctx = setup(cli, scope).await;
            let saved = ctx
                .wiki
                .restore(page, *version, *before)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&saved);
            } else {
                println!(
                    "✓ Restored \"{}\" to {} {version} (now version {})",
                    saved.summary.title,
                    if *before {
                        "the state before revision"
                    } else {
                        "revision"
                    },
                    saved.summary.version
                );
            }
        }
    }
}

pub async fn handle_registry(action: &RegistryAction, cli: &Cli) {
    match action {
        RegistryAction::Settings {
            scope,
            enable,
            disable,
        } => {
            let ctx = setup(cli, scope).await;
            let result = if *enable || *disable {
                let req = UpdateWikiSettingsRequest {
                    registry_enabled: Some(*enable),
                };
                ctx.wiki.update_settings(&req).await
            } else {
                ctx.wiki.settings().await
            };
            let settings = result.unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&settings);
            } else {
                println!(
                    "Registry: {}",
                    if settings.registry_enabled {
                        "on"
                    } else {
                        "off"
                    }
                );
            }
        }
        RegistryAction::Append { scope, text } => {
            let ctx = setup(cli, scope).await;
            let page = ctx
                .wiki
                .append_registry(text)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&page);
            } else {
                println!(
                    "✓ Logged to {}",
                    page.summary
                        .registry_date
                        .as_deref()
                        .unwrap_or(&page.summary.title)
                );
            }
        }
        RegistryAction::Feed {
            scope,
            before,
            limit,
        } => {
            let ctx = setup(cli, scope).await;
            let feed = ctx
                .wiki
                .registry_feed(before.as_deref(), *limit)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&feed);
                return;
            }
            if feed.days.is_empty() {
                println!("No registry entries.");
            }
            for day in &feed.days {
                println!("## {}\n{}\n", day.date, day.content);
            }
            if let Some(next) = &feed.next_before {
                println!("… older days: --before {next}");
            }
        }
        RegistryAction::Overview {
            scope,
            before,
            limit,
        } => {
            let ctx = setup(cli, scope).await;
            let list = ctx
                .wiki
                .overviews(before.as_deref(), *limit)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&list);
                return;
            }
            if list.overviews.is_empty() {
                println!("No overviews.");
            }
            for o in &list.overviews {
                println!("## {} – {}\n{}\n", o.period_start, o.period_end, o.content);
            }
            if let Some(next) = &list.next_before {
                println!("… older overviews: --before {next}");
            }
        }
        RegistryAction::GenerateOverview { scope, from, to } => {
            let ctx = setup(cli, scope).await;
            let req = GenerateOverviewRequest {
                period_start: from.clone(),
                period_end: to.clone(),
            };
            let overview = ctx
                .wiki
                .generate_overview(&req)
                .await
                .unwrap_or_else(|e| fail(e, ctx.is_json));
            if ctx.is_json {
                print_json(&overview);
            } else {
                println!(
                    "## {} – {}\n{}",
                    overview.period_start, overview.period_end, overview.content
                );
            }
        }
    }
}

/// Indented outline of the tree, children ordered by position.
fn render_tree(
    nodes: &[kuayle_sdk::types::wiki::WikiTreeNode],
    title: &dyn Fn(&str) -> String,
) -> String {
    fn walk(
        nodes: &[kuayle_sdk::types::wiki::WikiTreeNode],
        parent: Option<&str>,
        depth: usize,
        title: &dyn Fn(&str) -> String,
        out: &mut String,
    ) {
        let mut children: Vec<_> = nodes
            .iter()
            .filter(|n| n.parent_id.as_deref() == parent)
            .collect();
        children.sort_by_key(|n| n.position);
        for node in children {
            let marker = if node.kind == "folder" { "▸ " } else { "" };
            out.push_str(&format!(
                "{}{marker}{}  ({})\n",
                "  ".repeat(depth),
                title(&node.id),
                node.id
            ));
            walk(nodes, Some(&node.id), depth + 1, title, out);
        }
    }
    let mut out = String::new();
    walk(nodes, None, 0, title, &mut out);
    if out.is_empty() {
        out.push_str("No pages.\n");
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use kuayle_sdk::types::wiki::WikiTreeNode;

    fn node(id: &str, parent: Option<&str>, position: i64, kind: &str) -> WikiTreeNode {
        WikiTreeNode {
            id: id.into(),
            parent_id: parent.map(Into::into),
            position,
            kind: kind.into(),
        }
    }

    #[test]
    fn tree_nests_children_in_position_order() {
        let nodes = vec![
            node("b", None, 1, "page"),
            node("f", None, 0, "folder"),
            node("c2", Some("f"), 1, "page"),
            node("c1", Some("f"), 0, "page"),
        ];
        let out = render_tree(&nodes, &|id| id.to_uppercase());
        assert_eq!(out, "▸ F  (f)\n  C1  (c1)\n  C2  (c2)\nB  (b)\n");
    }
}

// Template commands — fully engine-driven CRUD showcase.
// 模板命令 — 全量引擎驱动 CRUD 示范。
//
// All five CRUD operations are delegated to the engine;
// only CLI flag parsing (+ Resolver for names if needed) stays here.
// 全部五种 CRUD 操作委托给引擎；仅 CLI flag 解析留在此处。

use kuayle_sdk::types::template::{CreateTemplateRequest, UpdateTemplateRequest};

use crate::cli::{Cli, TemplateAction};
use crate::engine;
use crate::output::{self, is_json_output};
use crate::registry::{self, RESOURCES};

/// Look up the templates ResourceSpec.
/// 查找 templates ResourceSpec。
fn spec() -> &'static registry::ResourceSpec {
    RESOURCES.iter().find(|r| r.name == "templates").unwrap()
}

pub async fn handle(action: &TemplateAction, cli: &Cli) {
    match action {
        TemplateAction::List => cmd_list(cli).await,
        TemplateAction::Read { id } => cmd_read(cli, id).await,
        TemplateAction::Create {
            name,
            title,
            description,
        } => cmd_create(cli, name, title, description.as_deref()).await,
        TemplateAction::Update {
            id,
            name,
            title,
            description,
        } => {
            cmd_update(
                cli,
                id,
                name.as_deref(),
                title.as_deref(),
                description.as_deref(),
            )
            .await
        }
        TemplateAction::Delete { id } => cmd_delete(cli, id).await,
    }
}

async fn setup(cli: &Cli) -> (kuayle_sdk::client::Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    let ws = cli.workspace.as_deref().unwrap_or("acme").to_string();
    (client, ws, is_json)
}

async fn cmd_list(cli: &Cli) {
    let (client, ws, is_json) = setup(cli).await;
    engine::execute_list(spec(), &spec().build_path(&ws), &client, is_json, false).await;
}

async fn cmd_read(cli: &Cli, id: &str) {
    let (client, ws, is_json) = setup(cli).await;
    engine::execute_read(spec(), &spec().build_item_path(&ws, id), &client, is_json).await;
}

async fn cmd_create(cli: &Cli, name: &str, title: &str, description: Option<&str>) {
    let (client, ws, is_json) = setup(cli).await;
    let body = CreateTemplateRequest {
        name: name.to_string(),
        title: Some(title.to_string()),
        description: description.map(|d| d.to_string()),
        team_id: None,
    };
    engine::execute_create(spec(), &spec().build_path(&ws), &client, &body, is_json).await;
}

async fn cmd_update(
    cli: &Cli,
    id: &str,
    name: Option<&str>,
    title: Option<&str>,
    description: Option<&str>,
) {
    let (client, ws, is_json) = setup(cli).await;
    let body = UpdateTemplateRequest {
        name: name.map(|s| s.to_string()),
        title: title.map(|s| s.to_string()),
        description: description.map(|s| s.to_string()),
        team_id: None,
    };
    engine::execute_update(
        spec(),
        &spec().build_item_path(&ws, id),
        &client,
        &body,
        is_json,
    )
    .await;
}

async fn cmd_delete(cli: &Cli, id: &str) {
    let (client, ws, is_json) = setup(cli).await;
    engine::execute_delete(spec(), &spec().build_item_path(&ws, id), &client, is_json).await;
}

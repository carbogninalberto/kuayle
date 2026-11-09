// View commands — engine-driven list/read (read-only with PAT).
use crate::cli::{Cli, ViewAction};
use crate::engine;
use crate::output::{self, is_json_output};
use crate::registry::RESOURCES;
fn spec() -> &'static crate::registry::ResourceSpec {
    RESOURCES.iter().find(|r| r.name == "views").unwrap()
}
pub async fn handle(action: &ViewAction, cli: &Cli) {
    match action {
        ViewAction::List => cmd_list(cli).await,
        ViewAction::Read { id } => cmd_read(cli, id).await,
    }
}
async fn setup(cli: &Cli) -> (kuayle_sdk::client::Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    (
        client,
        cli.workspace.as_deref().unwrap_or("acme").to_string(),
        is_json,
    )
}
async fn cmd_list(cli: &Cli) {
    let (c, w, j) = setup(cli).await;
    engine::execute_list(spec(), &spec().build_path(&w), &c, j, false).await;
}
async fn cmd_read(cli: &Cli, id: &str) {
    let (c, w, j) = setup(cli).await;
    engine::execute_read(spec(), &spec().build_item_path(&w, id), &c, j).await;
}

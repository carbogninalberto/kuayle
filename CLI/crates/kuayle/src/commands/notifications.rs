// Notification commands — engine-driven list (read-only with PAT, user-scoped).
use crate::cli::{Cli, NotificationAction};
use crate::engine;
use crate::output::{self, is_json_output};
use crate::registry::RESOURCES;
fn spec() -> &'static crate::registry::ResourceSpec {
    RESOURCES
        .iter()
        .find(|r| r.name == "notifications")
        .unwrap()
}
pub async fn handle(action: &NotificationAction, cli: &Cli) {
    match action {
        NotificationAction::List => cmd_list(cli).await,
    }
}
async fn setup(cli: &Cli) -> (kuayle_sdk::client::Client, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    (client, is_json)
}
async fn cmd_list(cli: &Cli) {
    let (client, is_json) = setup(cli).await;
    engine::execute_list(spec(), spec().path, &client, is_json, false).await;
}

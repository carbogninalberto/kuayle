// Asset commands.
//
// POST /api/workspaces/{ws}/upload       multipart "file"; needs issue:create
// GET  /api/workspaces/{ws}/assets/{id}  streams the file; needs assets:read
//
// The API has no asset listing or metadata endpoint, so there is no
// `assets list` or `assets read`.

use std::io::Write;

use kuayle_sdk::error::KuayleError;

use crate::cli::{AssetAction, Cli};
use crate::output::{self, is_json_output};

pub async fn handle(action: &AssetAction, cli: &Cli) {
    match action {
        AssetAction::Upload { file } => cmd_upload(cli, file).await,
        AssetAction::Download { id, output } => cmd_download(cli, id, output.as_deref()).await,
    }
}

async fn setup(cli: &Cli) -> (kuayle_sdk::client::Client, String, bool) {
    let is_json = is_json_output(cli);
    let (client, _url) = match crate::commands::resolve_client(cli).await {
        Ok(c) => c,
        Err(e) => output::print_string_error(&e, 2, is_json),
    };
    (client, crate::commands::workspace_slug(cli), is_json)
}

/// Join an API path onto the instance URL, as the SDK client does.
fn endpoint(client: &kuayle_sdk::client::Client, path: &str, is_json: bool) -> url::Url {
    client
        .base_url()
        .join(path)
        .unwrap_or_else(|e| output::print_string_error(&format!("invalid URL: {e}"), 4, is_json))
}

async fn token(client: &kuayle_sdk::client::Client) -> String {
    client.session().read().await.bearer_token().to_string()
}

/// Map a non-2xx response to the CLI's error output and exit code.
async fn fail_response(resp: reqwest::Response, is_json: bool) -> ! {
    let status = resp.status().as_u16();
    let text = resp.text().await.unwrap_or_default();
    let body = serde_json::from_str(&text).unwrap_or_else(|_| kuayle_sdk::error::ErrorBody {
        error: kuayle_sdk::error::ErrorPayload {
            code: "UNKNOWN".into(),
            message: text,
            details: vec![],
        },
    });
    let err = KuayleError::from_response(status, body);
    output::print_error(&err, is_json);
    std::process::exit(err.exit_code());
}

async fn cmd_upload(cli: &Cli, file: &str) {
    let (client, ws, is_json) = setup(cli).await;
    let url = endpoint(&client, &format!("/api/workspaces/{ws}/upload"), is_json);
    let file_bytes = match std::fs::read(file) {
        Ok(b) => b,
        Err(e) => output::print_string_error(&format!("read {file}: {e}"), 4, is_json),
    };
    let filename = std::path::Path::new(file)
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("file")
        .to_string();
    let form = reqwest::multipart::Form::new().part(
        "file",
        reqwest::multipart::Part::bytes(file_bytes).file_name(filename.clone()),
    );
    let resp = reqwest::Client::new()
        .post(url)
        .bearer_auth(token(&client).await)
        .multipart(form)
        .send()
        .await
        .unwrap_or_else(|e| output::print_string_error(&format!("upload: {e}"), 7, is_json));
    if !resp.status().is_success() {
        fail_response(resp, is_json).await;
    }
    let body: serde_json::Value = resp.json().await.unwrap_or_default();
    if is_json {
        println!(
            "{}",
            serde_json::to_string_pretty(&body).unwrap_or_default()
        );
    } else {
        println!(
            "✓ Uploaded {filename}\nID:  {}\nURL: {}",
            body["asset_id"].as_str().unwrap_or("-"),
            body["url"].as_str().unwrap_or("-")
        );
    }
}

/// Where a download goes: the given path, or ./asset-<id> in the current
/// directory. The ID must be a plain identifier so the default name can never
/// point outside the current directory.
fn default_output(id: &str) -> Option<String> {
    let plain = !id.is_empty() && id.chars().all(|c| c.is_ascii_alphanumeric() || c == '-');
    plain.then(|| format!("asset-{id}"))
}

async fn cmd_download(cli: &Cli, id: &str, output: Option<&str>) {
    let (client, ws, is_json) = setup(cli).await;
    let out_path = match output {
        Some(p) => p.to_string(),
        None => default_output(id)
            .unwrap_or_else(|| output::print_string_error("asset ID must be a UUID", 4, is_json)),
    };
    let url = endpoint(
        &client,
        &format!("/api/workspaces/{ws}/assets/{id}"),
        is_json,
    );
    let resp = reqwest::Client::new()
        .get(url)
        .bearer_auth(token(&client).await)
        .send()
        .await
        .unwrap_or_else(|e| output::print_string_error(&format!("download: {e}"), 7, is_json));
    if !resp.status().is_success() {
        fail_response(resp, is_json).await;
    }
    let bytes = resp
        .bytes()
        .await
        .unwrap_or_else(|e| output::print_string_error(&format!("download: {e}"), 7, is_json));
    if out_path == "-" {
        let _ = std::io::stdout().write_all(&bytes);
        return;
    }
    if let Err(e) = std::fs::write(&out_path, &bytes) {
        output::print_string_error(&format!("write {out_path}: {e}"), 1, is_json);
    }
    if is_json {
        println!(
            "{}",
            serde_json::json!({"path": out_path, "bytes": bytes.len()})
        );
    } else {
        println!("✓ Downloaded to {out_path} ({} bytes)", bytes.len());
    }
}

#[cfg(test)]
mod tests {
    use super::default_output;

    #[test]
    fn default_name_never_leaves_the_current_directory() {
        assert_eq!(
            default_output("0a1b2c3d-0000-4000-8000-000000000000").as_deref(),
            Some("asset-0a1b2c3d-0000-4000-8000-000000000000")
        );
        assert_eq!(default_output("../../.bashrc"), None);
        assert_eq!(default_output("a/b"), None);
        assert_eq!(default_output(""), None);
    }
}

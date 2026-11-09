// Asset commands — list, read, upload, download.
use crate::cli::{AssetAction, Cli};
use crate::engine;
use crate::output::{self, is_json_output};
use crate::registry::RESOURCES;
fn spec() -> &'static crate::registry::ResourceSpec {
    RESOURCES.iter().find(|r| r.name == "assets").unwrap()
}
pub async fn handle(action: &AssetAction, cli: &Cli) {
    match action {
        AssetAction::List => cmd_list(cli).await,
        AssetAction::Read { id } => cmd_read(cli, id).await,
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

/// Upload a file via multipart POST /api/workspaces/{ws}/upload.
/// 通过 multipart POST /api/workspaces/{ws}/upload 上传文件。
async fn cmd_upload(cli: &Cli, file: &str) {
    let (client, ws, is_json) = setup(cli).await;
    let token = {
        let guard = client.session().read().await;
        guard.bearer_token().to_string()
    };
    let url = format!("{}/api/workspaces/{ws}/upload", client.base_url());

    let file_bytes = match std::fs::read(file) {
        Ok(b) => b,
        Err(e) => output::print_string_error(&format!("read file: {e}"), 1, is_json),
    };
    let filename = std::path::Path::new(file)
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("file");

    let part = reqwest::multipart::Part::bytes(file_bytes).file_name(filename.to_string());
    let form = reqwest::multipart::Form::new().part("file", part);

    let http = reqwest::Client::new();
    let resp = match http
        .post(&url)
        .header("Authorization", format!("Bearer {token}"))
        .multipart(form)
        .send()
        .await
    {
        Ok(r) => r,
        Err(e) => {
            eprintln!("Upload error: {e}");
            std::process::exit(7);
        }
    };

    let status = resp.status();
    let body = resp.text().await.unwrap_or_default();
    if status.is_success() {
        if is_json {
            println!("{body}");
        } else {
            println!("✓ Uploaded {filename}\n{body}");
        }
    } else {
        eprintln!("Upload failed ({}): {body}", status.as_u16());
        std::process::exit(1);
    }
}

/// Download an asset via GET /api/workspaces/{ws}/assets/{id}.
/// 通过 GET /api/workspaces/{ws}/assets/{id} 下载资源。
async fn cmd_download(cli: &Cli, id: &str, output: Option<&str>) {
    let (client, ws, is_json) = setup(cli).await;
    let resp: serde_json::Value = match client
        .get(&format!("/api/workspaces/{ws}/assets/{id}"))
        .await
    {
        Ok(v) => v,
        Err(e) => {
            output::print_error(&e, is_json);
            std::process::exit(e.exit_code());
        }
    };
    let asset_url = resp["url"].as_str().unwrap_or("");
    if asset_url.is_empty() {
        output::print_string_error("No URL in asset response", 1, is_json);
    }

    let token = {
        let g = client.session().read().await;
        g.bearer_token().to_string()
    };
    let full_url = format!("{}{}", client.base_url(), asset_url);
    let http = reqwest::Client::new();
    let download = match http
        .get(&full_url)
        .header("Authorization", format!("Bearer {token}"))
        .send()
        .await
    {
        Ok(r) => r,
        Err(e) => {
            eprintln!("Download error: {e}");
            std::process::exit(7);
        }
    };
    let bytes = download.bytes().await.unwrap_or_default();
    let out_path = output.unwrap_or_else(|| resp["filename"].as_str().unwrap_or("asset"));
    match std::fs::write(out_path, &bytes) {
        Ok(_) => println!("✓ Downloaded to {out_path} ({} bytes)", bytes.len()),
        Err(e) => output::print_string_error(&format!("write: {e}"), 1, is_json),
    }
}

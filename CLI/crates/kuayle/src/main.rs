// kuayle CLI entry point.
// kuayle CLI 入口点。
mod cli;
mod commands;
mod completion;
mod config;
mod creds;
mod engine;
mod output;
mod registry;
mod resolve;
mod update;
mod usage;

use clap::{CommandFactory, Parser};

#[tokio::main]
async fn main() {
    let cli = cli::Cli::parse();

    // Check for updates on startup (cached, 24h TTL).
    // 启动时检查更新（缓存，24 小时 TTL）。
    if let Some(ver) = cli::Cli::command().get_version() {
        update::check_version(ver);
    }

    match &cli.command {
        cli::Command::Auth { action } => {
            commands::auth_cmd::handle(action, &cli).await;
        }
        cli::Command::Whoami => {
            commands::auth_cmd::handle_whoami(&cli).await;
        }
        cli::Command::Workspaces { action } => {
            commands::workspaces::handle(action, &cli).await;
        }
        cli::Command::Issues { action } => {
            commands::issues::handle(action, &cli).await;
        }
        cli::Command::Comments { action } => {
            commands::comments::handle(action, &cli).await;
        }
        cli::Command::Relations { action } => {
            commands::relations::handle(action, &cli).await;
        }
        cli::Command::Labels { action } => {
            commands::labels::handle(action, &cli).await;
        }
        cli::Command::Teams { action } => {
            commands::teams::handle(action, &cli).await;
        }
        cli::Command::Statuses { action } => {
            commands::statuses::handle(action, &cli).await;
        }
        cli::Command::Projects { action } => {
            commands::projects::handle(action, &cli).await;
        }
        cli::Command::Cycles { action } => {
            commands::cycles::handle(action, &cli).await;
        }
        cli::Command::Templates { action } => {
            commands::templates::handle(action, &cli).await;
        }
        cli::Command::Views { action } => {
            commands::views::handle(action, &cli).await;
        }
        cli::Command::Members { action } => {
            commands::members::handle(action, &cli).await;
        }
        cli::Command::Favorites { action } => {
            commands::favorites::handle(action, &cli).await;
        }
        cli::Command::Notifications { action } => {
            commands::notifications::handle(action, &cli).await;
        }
        cli::Command::Assets { action } => {
            commands::assets::handle(action, &cli).await;
        }
        cli::Command::Usage => {
            println!("{}", usage::generate(&cli::Cli::command()));
        }
        cli::Command::Completion { shell } => match completion::generate_completion(shell) {
            Ok(script) => println!("{script}"),
            Err(e) => {
                eprintln!("{e}");
                std::process::exit(1);
            }
        },
        cli::Command::SelfUpdate => {
            if let Some(ver) = cli::Cli::command().get_version() {
                update::self_update(ver).await.unwrap_or_else(|e| {
                    eprintln!("{e}");
                    std::process::exit(1);
                });
            }
        }
    }
}

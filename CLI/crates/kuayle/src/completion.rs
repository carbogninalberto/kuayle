// Shell completion generation via clap_complete.
// 通过 clap_complete 生成 shell 补全脚本。

use crate::cli::Cli;
use clap::CommandFactory;
use clap_complete::{
    generate,
    shells::{Bash, Fish, Zsh},
};

/// Generate shell completion script for the given shell.
/// 为指定 shell 生成补全脚本。
///
/// Supported shells: bash, zsh, fish.
/// 支持的 shell：bash、zsh、fish。
pub fn generate_completion(shell: &str) -> Result<String, String> {
    let mut cmd = Cli::command();
    let mut buf = Vec::new();
    match shell {
        "bash" => generate(Bash, &mut cmd, "kuayle", &mut buf),
        "zsh" => generate(Zsh, &mut cmd, "kuayle", &mut buf),
        "fish" => generate(Fish, &mut cmd, "kuayle", &mut buf),
        _ => {
            return Err(format!(
                "unsupported shell: {shell}. Supported: bash, zsh, fish"
            ))
        }
    }
    String::from_utf8(buf).map_err(|e| format!("utf8: {e}"))
}

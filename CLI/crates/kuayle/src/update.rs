// Self-update from the official Kuayle releases, with SHA-256 verification.
//
// CLI releases are tagged `cli-v<version>` on github.com/carbogninalberto/kuayle
// and carry one archive per target plus `checksums.txt` (sha256sum format).
// The download location is fixed at compile time: there is no flag or
// environment variable that points the updater anywhere else.
//
// The archive is verified against checksums.txt before anything is
// extracted; a missing entry or a mismatch fails closed and the installed
// binary is left untouched.
//
// Only builds with the `binary-release` feature (the published binaries)
// replace themselves. Builds from source are told to update with cargo.
#![cfg_attr(not(feature = "binary-release"), allow(dead_code))]

use std::path::PathBuf;
use std::time::{Duration, SystemTime};

use serde::Deserialize;
use sha2::{Digest, Sha256};

const CACHE_TTL: Duration = Duration::from_secs(24 * 3600);

/// The only repository releases are downloaded from.
pub const OFFICIAL_REPO: &str = "carbogninalberto/kuayle";
/// Tag prefix that separates CLI releases from Kuayle server releases.
pub const TAG_PREFIX: &str = "cli-v";
/// The target this binary was built for, e.g. `aarch64-apple-darwin`.
pub const BUILD_TARGET: &str = env!("KUAYLE_BUILD_TARGET");

/// Where releases are looked up and downloaded from.
#[derive(Debug, Clone)]
pub struct Source {
    /// GitHub REST API root, e.g. `https://api.github.com`.
    pub api: String,
    /// Release downloads root, e.g. `https://github.com`.
    pub download: String,
    /// `owner/name`.
    pub repo: String,
}

impl Source {
    pub fn official() -> Self {
        Source {
            api: "https://api.github.com".into(),
            download: "https://github.com".into(),
            repo: OFFICIAL_REPO.into(),
        }
    }

    fn releases_url(&self) -> String {
        format!("{}/repos/{}/releases?per_page=100", self.api, self.repo)
    }

    fn asset_url(&self, tag: &str, name: &str) -> String {
        format!(
            "{}/{}/releases/download/{tag}/{name}",
            self.download, self.repo
        )
    }
}

/// Check for updates and print a hint if a newer version is available.
/// The check result is cached for 24 hours in ~/.config/kuayle/version-check.
pub fn check_version(current: &str) {
    if current == "0.0.0" {
        return; // dev build
    }

    let cache_path = match version_cache_path() {
        Some(p) => p,
        None => return,
    };

    if let Ok(meta) = std::fs::metadata(&cache_path) {
        if let Ok(mtime) = meta.modified() {
            if let Ok(age) = SystemTime::now().duration_since(mtime) {
                if age < CACHE_TTL {
                    return;
                }
            }
        }
    }

    let _ = std::fs::write(&cache_path, current);
}

fn version_cache_path() -> Option<PathBuf> {
    dirs::config_dir().map(|d| d.join("kuayle").join("version-check"))
}

#[derive(Debug, Clone, Deserialize)]
pub struct Release {
    pub tag_name: String,
    #[serde(default)]
    pub draft: bool,
    #[serde(default)]
    pub prerelease: bool,
}

/// Parse `1.2.3` into comparable numbers. Pre-release suffixes are rejected.
fn parse_version(v: &str) -> Option<(u64, u64, u64)> {
    let mut parts = v.trim().trim_start_matches('v').split('.');
    let major = parts.next()?.parse().ok()?;
    let minor = parts.next()?.parse().ok()?;
    let patch = parts.next()?.parse().ok()?;
    if parts.next().is_some() {
        return None;
    }
    Some((major, minor, patch))
}

/// The newest published, stable CLI release; server releases are ignored.
pub fn latest_cli_release(releases: &[Release]) -> Option<(String, (u64, u64, u64))> {
    releases
        .iter()
        .filter(|r| !r.draft && !r.prerelease)
        .filter_map(|r| {
            let version = parse_version(r.tag_name.strip_prefix(TAG_PREFIX)?)?;
            Some((r.tag_name.clone(), version))
        })
        .max_by_key(|(_, version)| *version)
}

/// The release archive name for a target.
pub fn archive_name(target: &str) -> String {
    if target.contains("windows") {
        format!("kuayle-{target}.zip")
    } else {
        format!("kuayle-{target}.tar.gz")
    }
}

/// The expected SHA-256 (lowercase hex) of `name` in a sha256sum listing.
pub fn expected_checksum(checksums: &str, name: &str) -> Option<String> {
    checksums.lines().find_map(|line| {
        let mut fields = line.split_whitespace();
        let hash = fields.next()?;
        // sha256sum marks binary mode with a leading '*'; paths may carry a
        // directory from the release job.
        let file = fields.next()?.trim_start_matches('*');
        let file = file.rsplit('/').next()?;
        let valid = hash.len() == 64 && hash.chars().all(|c| c.is_ascii_hexdigit());
        (file == name && valid).then(|| hash.to_ascii_lowercase())
    })
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);
    digest.iter().map(|b| format!("{b:02x}")).collect()
}

/// Fail closed unless `bytes` match the checksum listed for `name`.
pub fn verify_archive(bytes: &[u8], checksums: &str, name: &str) -> Result<(), String> {
    let expected = expected_checksum(checksums, name)
        .ok_or_else(|| format!("checksums.txt has no entry for {name}; refusing to install"))?;
    let actual = sha256_hex(bytes);
    if actual != expected {
        return Err(format!(
            "checksum mismatch for {name} (expected {expected}, got {actual}); refusing to install"
        ));
    }
    Ok(())
}

/// The `kuayle` binary inside a verified release archive.
pub fn extract_binary(archive: &[u8], name: &str) -> Result<Vec<u8>, String> {
    if name.ends_with(".zip") {
        return extract_zip(archive);
    }
    let decoder = flate2::read::GzDecoder::new(archive);
    let mut tar = tar::Archive::new(decoder);
    let entries = tar.entries().map_err(|e| format!("read archive: {e}"))?;
    for entry in entries {
        let mut entry = entry.map_err(|e| format!("read archive: {e}"))?;
        let path = entry.path().map_err(|e| format!("read archive: {e}"))?;
        if path.file_name().and_then(|n| n.to_str()) == Some("kuayle")
            && entry.header().entry_type().is_file()
        {
            let mut out = Vec::new();
            std::io::Read::read_to_end(&mut entry, &mut out)
                .map_err(|e| format!("read archive: {e}"))?;
            return Ok(out);
        }
    }
    Err("archive does not contain the kuayle binary".into())
}

#[cfg(windows)]
fn extract_zip(archive: &[u8]) -> Result<Vec<u8>, String> {
    let mut zip = zip::ZipArchive::new(std::io::Cursor::new(archive))
        .map_err(|e| format!("read archive: {e}"))?;
    let mut file = zip
        .by_name("kuayle.exe")
        .map_err(|_| "archive does not contain kuayle.exe".to_string())?;
    let mut out = Vec::new();
    std::io::Read::read_to_end(&mut file, &mut out).map_err(|e| format!("read archive: {e}"))?;
    Ok(out)
}

#[cfg(not(windows))]
fn extract_zip(_archive: &[u8]) -> Result<Vec<u8>, String> {
    Err("zip archives are only used on Windows".into())
}

async fn fetch(client: &reqwest::Client, url: &str) -> Result<Vec<u8>, String> {
    let resp = client
        .get(url)
        .header(reqwest::header::USER_AGENT, "kuayle-cli")
        .send()
        .await
        .map_err(|e| format!("download {url}: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("download {url}: HTTP {}", resp.status()));
    }
    resp.bytes()
        .await
        .map(|b| b.to_vec())
        .map_err(|e| format!("download {url}: {e}"))
}

/// What an update check found.
#[derive(Debug, PartialEq)]
pub enum Plan {
    UpToDate { current: String },
    Update { tag: String, version: String },
}

/// Find the newest CLI release and compare it with `current`.
pub async fn plan(source: &Source, current: &str) -> Result<Plan, String> {
    let client = reqwest::Client::new();
    let body = fetch(&client, &source.releases_url()).await?;
    let releases: Vec<Release> =
        serde_json::from_slice(&body).map_err(|e| format!("parse releases: {e}"))?;
    let (tag, latest) =
        latest_cli_release(&releases).ok_or("no published CLI release found".to_string())?;
    let current_v = parse_version(current).unwrap_or((0, 0, 0));
    if latest <= current_v {
        return Ok(Plan::UpToDate {
            current: current.to_string(),
        });
    }
    let version = format!("{}.{}.{}", latest.0, latest.1, latest.2);
    Ok(Plan::Update { tag, version })
}

/// Download the archive for `target` from release `tag`, verify it against
/// the release's checksums.txt and return the binary inside it.
pub async fn download_verified(
    source: &Source,
    tag: &str,
    target: &str,
) -> Result<Vec<u8>, String> {
    let client = reqwest::Client::new();
    let name = archive_name(target);
    let checksums = fetch(&client, &source.asset_url(tag, "checksums.txt")).await?;
    let checksums = String::from_utf8(checksums).map_err(|_| "checksums.txt is not text")?;
    let archive = fetch(&client, &source.asset_url(tag, &name)).await?;
    verify_archive(&archive, &checksums, &name)?;
    extract_binary(&archive, &name)
}

/// Replace the running binary with `binary`.
fn replace_current(binary: &[u8]) -> Result<(), String> {
    let exe = std::env::current_exe().map_err(|e| format!("locate current binary: {e}"))?;
    let dir = exe.parent().ok_or("current binary has no directory")?;
    let staged = dir.join(format!(".kuayle-update-{}", std::process::id()));
    std::fs::write(&staged, binary).map_err(|e| format!("write {}: {e}", staged.display()))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&staged, std::fs::Permissions::from_mode(0o755))
            .map_err(|e| format!("chmod: {e}"))?;
    }
    let result = self_replace::self_replace(&staged).map_err(|e| format!("replace binary: {e}"));
    let _ = std::fs::remove_file(&staged);
    result
}

/// Download, verify and install the latest release.
#[cfg(feature = "binary-release")]
pub async fn self_update(current: &str) -> Result<(), String> {
    let source = Source::official();
    match plan(&source, current).await? {
        Plan::UpToDate { current } => {
            println!("Already up to date ({current}).");
            Ok(())
        }
        Plan::Update { tag, version } => {
            println!("Updating kuayle {current} → {version} ({BUILD_TARGET})…");
            let binary = download_verified(&source, &tag, BUILD_TARGET).await?;
            println!("✓ SHA-256 verified against {tag}/checksums.txt");
            replace_current(&binary)?;
            println!("✓ kuayle updated to {version}");
            Ok(())
        }
    }
}

#[cfg(not(feature = "binary-release"))]
pub async fn self_update(_current: &str) -> Result<(), String> {
    Err("this kuayle was built from source; update it with \
         'cargo install --git https://github.com/carbogninalberto/kuayle kuayle', \
         or install a release binary with CLI/install.sh"
        .to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rel(tag: &str, draft: bool, prerelease: bool) -> Release {
        Release {
            tag_name: tag.into(),
            draft,
            prerelease,
        }
    }

    #[test]
    fn picks_newest_stable_cli_release_and_ignores_server_tags() {
        let releases = vec![
            rel("v0.2.40", false, false),
            rel("cli-v0.3.0", true, false),
            rel("cli-v0.2.10", false, true),
            rel("cli-v0.2.9", false, false),
            rel("cli-v0.2.1", false, false),
            rel("cli-vbad", false, false),
        ];
        assert_eq!(
            latest_cli_release(&releases),
            Some(("cli-v0.2.9".into(), (0, 2, 9)))
        );
        assert_eq!(latest_cli_release(&[rel("v1.0.0", false, false)]), None);
    }

    #[test]
    fn archive_names_match_the_release_workflow() {
        assert_eq!(
            archive_name("x86_64-unknown-linux-musl"),
            "kuayle-x86_64-unknown-linux-musl.tar.gz"
        );
        assert_eq!(
            archive_name("x86_64-pc-windows-msvc"),
            "kuayle-x86_64-pc-windows-msvc.zip"
        );
        assert!(!BUILD_TARGET.is_empty());
    }

    #[test]
    fn checksum_lookup_handles_sha256sum_formats() {
        let a = "a".repeat(64);
        let b = "B".repeat(64);
        let listing = format!(
            "{a}  dist/kuayle-x86_64-unknown-linux-musl.tar.gz\n{b} *kuayle-aarch64-apple-darwin.tar.gz\nnot a line\n"
        );
        assert_eq!(
            expected_checksum(&listing, "kuayle-x86_64-unknown-linux-musl.tar.gz"),
            Some(a)
        );
        assert_eq!(
            expected_checksum(&listing, "kuayle-aarch64-apple-darwin.tar.gz"),
            Some("b".repeat(64))
        );
        assert_eq!(expected_checksum(&listing, "kuayle-other.tar.gz"), None);
        assert_eq!(
            expected_checksum("short  kuayle.tar.gz", "kuayle.tar.gz"),
            None
        );
    }

    #[test]
    fn verification_fails_closed() {
        let bytes = b"archive";
        let good = format!("{}  kuayle.tar.gz\n", sha256_hex(bytes));
        assert!(verify_archive(bytes, &good, "kuayle.tar.gz").is_ok());
        let err = verify_archive(b"tampered", &good, "kuayle.tar.gz").unwrap_err();
        assert!(err.contains("checksum mismatch"));
        let err = verify_archive(bytes, "", "kuayle.tar.gz").unwrap_err();
        assert!(err.contains("no entry"));
    }

    pub(crate) fn tarball(binary: &[u8]) -> Vec<u8> {
        let mut builder = tar::Builder::new(flate2::write::GzEncoder::new(
            Vec::new(),
            flate2::Compression::default(),
        ));
        let mut header = tar::Header::new_gnu();
        header.set_size(binary.len() as u64);
        header.set_mode(0o755);
        header.set_cksum();
        builder.append_data(&mut header, "kuayle", binary).unwrap();
        builder.into_inner().unwrap().finish().unwrap()
    }

    #[test]
    fn extracts_the_binary_from_a_tarball() {
        let archive = tarball(b"#!binary");
        assert_eq!(
            extract_binary(&archive, "kuayle-x.tar.gz").unwrap(),
            b"#!binary"
        );
        assert!(extract_binary(b"not gzip", "kuayle-x.tar.gz").is_err());
    }

    #[tokio::test]
    async fn downloads_verify_before_extracting() {
        use wiremock::matchers::{method, path};
        use wiremock::{Mock, MockServer, ResponseTemplate};

        let server = MockServer::start().await;
        let source = Source {
            api: server.uri(),
            download: server.uri(),
            repo: OFFICIAL_REPO.into(),
        };
        let target = "x86_64-unknown-linux-musl";
        let archive = tarball(b"new kuayle");
        Mock::given(method("GET"))
            .and(path(format!("/repos/{OFFICIAL_REPO}/releases")))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!([
                {"tag_name": "v9.9.9", "draft": false, "prerelease": false},
                {"tag_name": "cli-v0.4.0", "draft": false, "prerelease": false}
            ])))
            .mount(&server)
            .await;
        let base = format!("/{OFFICIAL_REPO}/releases/download/cli-v0.4.0");
        Mock::given(method("GET"))
            .and(path(format!("{base}/{}", archive_name(target))))
            .respond_with(ResponseTemplate::new(200).set_body_bytes(archive.clone()))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path(format!("{base}/checksums.txt")))
            .respond_with(ResponseTemplate::new(200).set_body_string(format!(
                "{}  {}\n",
                sha256_hex(&archive),
                archive_name(target)
            )))
            .up_to_n_times(1)
            .mount(&server)
            .await;

        assert_eq!(
            plan(&source, "0.4.0").await.unwrap(),
            Plan::UpToDate {
                current: "0.4.0".into()
            }
        );
        let Plan::Update { tag, version } = plan(&source, "0.3.2").await.unwrap() else {
            panic!("expected an update");
        };
        assert_eq!((tag.as_str(), version.as_str()), ("cli-v0.4.0", "0.4.0"));
        assert_eq!(
            download_verified(&source, &tag, target).await.unwrap(),
            b"new kuayle"
        );

        // A tampered checksum list makes the same download fail closed.
        Mock::given(method("GET"))
            .and(path(format!("{base}/checksums.txt")))
            .respond_with(ResponseTemplate::new(200).set_body_string(format!(
                "{}  {}\n",
                "0".repeat(64),
                archive_name(target)
            )))
            .mount(&server)
            .await;
        let err = download_verified(&source, &tag, target).await.unwrap_err();
        assert!(err.contains("checksum mismatch"), "{err}");
    }
}

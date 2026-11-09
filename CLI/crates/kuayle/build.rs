// Record the target triple so `kuayle self-update` downloads the matching
// release archive.
fn main() {
    let target = std::env::var("TARGET").unwrap_or_default();
    println!("cargo:rustc-env=KUAYLE_BUILD_TARGET={target}");
}

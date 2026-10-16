fn embed_runtime_bootstrap() {
    const PATH_ENV: &str = "DEC_RUNTIME_BOOTSTRAP";
    println!("cargo:rerun-if-env-changed={PATH_ENV}");
    let Some(path) = std::env::var_os(PATH_ENV) else {
        return;
    };
    println!(
        "cargo:rerun-if-changed={}",
        std::path::Path::new(&path).display()
    );
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("{PATH_ENV} cannot be read: {error}"));
    let value: serde_json::Value = serde_json::from_str(&text)
        .unwrap_or_else(|error| panic!("{PATH_ENV} is not JSON: {error}"));
    assert_eq!(
        value.get("schema").and_then(|item| item.as_str()),
        Some("relkit.runtime-bootstrap/1"),
        "{PATH_ENV} is not a Relkit runtime bootstrap"
    );
    assert_eq!(
        value
            .pointer("/profile/product")
            .and_then(|item| item.as_str()),
        Some("dec"),
        "{PATH_ENV} is for the wrong product"
    );
    let compact = serde_json::to_string(&value)
        .unwrap_or_else(|error| panic!("{PATH_ENV} cannot be normalized: {error}"));
    println!("cargo:rustc-env=DEC_RUNTIME_BOOTSTRAP_JSON={compact}");
}

fn main() {
    let protoc = protoc_bin_vendored::protoc_bin_path().expect("vendored protoc");
    std::env::set_var("PROTOC", protoc);
    let proto = "../../schema/service/v1/service.proto";
    let include = "../../schema";
    tonic_build::configure()
        .build_server(false)
        .compile_protos(&[proto], &[include])
        .expect("compile Dec service proto");
    embed_runtime_bootstrap();
    tauri_build::build();
}

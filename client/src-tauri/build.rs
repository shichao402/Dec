fn main() {
    let protoc = protoc_bin_vendored::protoc_bin_path().expect("vendored protoc");
    std::env::set_var("PROTOC", protoc);
    let proto = "../../schema/service/v1/service.proto";
    let include = "../../schema";
    tonic_build::configure()
        .build_server(false)
        .compile_protos(&[proto], &[include])
        .expect("compile Dec service proto");
    tauri_build::build();
}

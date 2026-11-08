{ pkgs, lib, config, inputs, ... }:

{
  languages.go = {
    enable = true;
    # go.mod asks for Go 1.26, which templ v0.3.1070 requires.
    package = pkgs.go_1_26;
  };

  # Extra arguments go to the binary, e.g. `serve -listen 127.0.0.1:9000`.
  # beets-api must be running too: `devenv up` (or `api`) in ../beets-api.
  scripts.serve.exec = ''
    cd "$DEVENV_ROOT"
    go generate ./... && exec go run . "$@"
  '';
  processes.web.exec = "serve";

  # Regenerates templ files and restarts the server when a .templ or .go file changes.
  scripts.serve-watch.exec = ''
    cd "$DEVENV_ROOT"
    exec go tool templ generate --watch --cmd "go run ."
  '';

  enterTest = ''
    go generate ./...
    go vet ./...
    go test ./...
  '';

  cachix.enable = false;
}

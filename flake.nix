{
  description = "gojo – a TUI for jj (Jujutsu VCS), in Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      version = "0.1.0";
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = fn: nixpkgs.lib.genAttrs systems (system: fn nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs:
        {
          default = pkgs.buildGoModule {
            pname = "gojo";
            inherit version;

            src = self;

            # proxyVendor uses `go mod download` (full module zips) instead of
            # `go mod vendor`. Vendoring prunes to the build platform's
            # imports, so its hash differs across GOOS/GOARCH; proxyVendor
            # yields one vendorHash that's stable across all `systems` above.
            proxyVendor = true;

            # Hash of the downloaded Go modules. When go.sum changes, run
            # `nix build` once and replace this with the "got:" value.
            # Stable across systems thanks to proxyVendor above.
            vendorHash = "sha256-Ln1ztajyLuXmMJet53FOAfFIMi0t1Qmp/9oAK2TAo+0=";

            # gojo shells out to `jj` at runtime; keep it on PATH.
            nativeBuildInputs = [ pkgs.makeWrapper ];
            postInstall = ''
              wrapProgram $out/bin/gojo --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.jujutsu ]}
              ln -s gojo $out/bin/gj
            '';

            meta = with pkgs.lib; {
              description = "Fullscreen terminal UI for jj (Jujutsu VCS)";
              mainProgram = "gojo";
              license = licenses.mit;
            };
          };
        }
      );

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = "${self.packages.${pkgs.system}.default}/bin/gojo";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            go-tools
            jujutsu
          ];

          shellHook = ''
            echo "gojo dev shell – $(go version)"
          '';
        };
      });
    };
}

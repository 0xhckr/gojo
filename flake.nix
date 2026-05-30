{
  description = "gojo – a TUI for jj (Jujutsu VCS)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = fn: nixpkgs.lib.genAttrs systems (system: fn nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "gojo";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-bm+ZuY/tVTUWasnisfRMDffMZHsE/30U9ednC13u2IE=";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            jujutsu
          ];

          shellHook = ''
            echo "gojo dev shell – go $(go version | awk '{print $3}')"
          '';
        };
      });
    };
}

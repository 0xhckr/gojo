{
  description = "gojo – a TUI for jj (Jujutsu VCS)";

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
        let
          # Fixed-output derivation: fetches bun deps with network access.
          # Hash is verified, so this is safe. Update when bun.lock changes.
          bunDeps = pkgs.stdenv.mkDerivation {
            name = "gojo-${version}-bun-deps";
            src = self;

            nativeBuildInputs = [ pkgs.bun ];

            dontBuild = true;
            dontFixup = true;

            installPhase = ''
              export HOME=$TMPDIR
              bun install --frozen-lockfile --no-save
              cp -r node_modules $out
            '';

            outputHashAlgo = "sha256";
            outputHashMode = "recursive";
            outputHash = "sha256-xbBgYVWdM12f3I7FQPB+6mGvRm5FGoNStPC2iYp4kOA=";
          };
        in
        {
          default = pkgs.stdenv.mkDerivation {
            pname = "gojo";
            inherit version;

            src = self;

            nativeBuildInputs = [ pkgs.bun ];

            configurePhase = ''
              # Copy pre-fetched node_modules so bun build can resolve deps
              cp -r ${bunDeps} node_modules
              chmod -R u+w node_modules
            '';

            buildPhase = ''
              export HOME=$TMPDIR
              bun build --compile src/main.tsx --outfile gojo
            '';

            dontStrip = true;

            installPhase = ''
              mkdir -p $out/bin
              install -m755 gojo $out/bin/gojo
            '';
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
            bun
            nodejs_24
            pnpm
            typescript
            jujutsu
          ];

          shellHook = ''
            echo "gojo dev shell – bun $(bun --version)"
          '';
        };
      });
    };
}

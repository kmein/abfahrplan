{
  description = "Compact BVG-style departure timetables from a GTFS feed";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      overlays.default = final: prev: {
        abfahrplan = final.callPackage ./nix/package.nix { };
      };

      packages = forAllSystems (pkgs: rec {
        abfahrplan = pkgs.callPackage ./nix/package.nix { };
        default = abfahrplan;
      });

      nixosModules.default = {
        imports = [ ./nix/module.nix ];
        nixpkgs.overlays = [ self.overlays.default ];
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.typst
            pkgs.fira-sans
            pkgs.pmtiles # for cutting a basemap extract
          ];
          # the same fonts the packaged binary pins, so a locally built PDF
          # matches a deployed one
          ABFAHRPLAN_FONT_PATH = "${pkgs.fira-sans}/share/fonts";
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}

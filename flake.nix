{
  description = "Pelton, a privacy-focused cross-platform desktop email client";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
      version =
        if self ? shortRev then
          "2026.4+${self.shortRev}"
        else
          "2026.4+dirty${toString (self.lastModifiedDate or "unknown")}";
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          pelton = pkgs.callPackage ./packaging/nix/package.nix {
            src = self;
            inherit version;
          };
          default = self.packages.${system}.pelton;
        }
      );

      apps = forAllSystems (system: {
        pelton = {
          type = "app";
          program = "${self.packages.${system}.pelton}/bin/pelton";
        };
        default = self.apps.${system}.pelton;
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            inputsFrom = [ self.packages.${system}.pelton ];
            packages = with pkgs; [
              go
              gopls
              pnpm_10
              nodejs_22
              wails
            ];
          };
        }
      );
    };
}

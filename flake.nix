{
  description = "Build your own Framewise: the course, its tester and a dev shell per language";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs, ... }:
    let
      lib = nixpkgs.lib;
      forAllSystems = lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
      # One file per language in nix/: { pkgs }: { packages = [ ... ]; env = { ... }; }.
      # build-your-own-y's tools/add-language writes them; ./test.sh <lang> runs in that shell.
      languages = map (lib.removeSuffix ".nix") (
        builtins.attrNames (lib.filterAttrs (n: t: t == "regular" && lib.hasSuffix ".nix" n) (builtins.readDir ./nix))
      );
    in
    {
      packages = forAllSystems (system: {
        tester = nixpkgs.legacyPackages.${system}.buildGoModule {
          pname = "framewise-tester";
          version = self.shortRev or "dirty";
          src = ./tester;
          vendorHash = "sha256-Qimqsyrgew/5eRFhFQ0ckBY/eUJK1A/elB2939iuu+g=";
          subPackages = [ "cmd/tester" ];
          # Its unit tests are `make -C tester test`; ./test.sh is the check here.
          doCheck = false;
          env.CGO_ENABLED = 0;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # Read by test.sh. The course and the tester's source are this repo.
          testerEnv.FRAMEWISE_TESTER_BIN = "${self.packages.${system}.tester}/bin/tester";
          shellFor =
            lang:
            let
              l = import ./nix/${lang}.nix { inherit pkgs; };
            in
            pkgs.mkShell (
              {
                packages = [
                  pkgs.git
                  pkgs.bash
                ] ++ l.packages;
              }
              // (l.env or { })
              // testerEnv
              # test.sh re-enters the right shell unless it is already in it.
              // { FRAMEWISE_SHELL = lang; }
            );
          shells = lib.genAttrs languages shellFor;
        in
        shells
        // {
          # Working on the course: every language, plus Go for the tester and bun for course-sdk.
          default = pkgs.mkShell (
            {
              inputsFrom = builtins.attrValues shells;
              packages = with pkgs; [
                go
                gopls
                bun
                gnumake
              ];
              CGO_ENABLED = "0";
            }
            // testerEnv
          );
        }
      );
    };
}

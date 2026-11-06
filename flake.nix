{
  description = "CodeCrafters build-your-own-framewise (python) dev environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    course = {
      url = "git+https://github.com/GuyLevavi/build-your-own-framewise";
      flake = false;
    };
    tester = {
      url = "git+https://github.com/GuyLevavi/framewise-tester";
      flake = false;
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      course,
      tester,
      ...
    }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
    in
    {
      packages = forAllSystems (system: {
        framewise-tester = nixpkgs.legacyPackages.${system}.buildGoModule {
          pname = "framewise-tester";
          version = tester.shortRev or "dirty";
          src = tester;
          vendorHash = "sha256-RDudxrn9Q+vaUMhg1tMuy+p3ug380Q5lXU1r3e9ko3Y=";
          subPackages = [ "cmd/tester" ];
          # The tester's own test suite needs the reference toolchains; ./test.sh is the check here.
          doCheck = false;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              git
              bash
              uv
              python3
            ];
            UV_PYTHON_DOWNLOADS = "never";
            UV_PYTHON = "${pkgs.python3}/bin/python3";
            # Read by test.sh.
            FRAMEWISE_COURSE_DIR = "${course}";
            FRAMEWISE_TESTER_BIN = "${self.packages.${system}.framewise-tester}/bin/tester";
            FRAMEWISE_TESTER_SRC = "${tester}";
          };
        }
      );
    };
}

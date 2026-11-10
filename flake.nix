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
          vendorHash = "sha256-Qimqsyrgew/5eRFhFQ0ckBY/eUJK1A/elB2939iuu+g=";
          subPackages = [ "cmd/tester" ];
          # The tester's own test suite needs the reference toolchains; ./test.sh is the check here.
          doCheck = false;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # The course's detector, built from its source in the course input.
          fwdetect =
            ps:
            ps.buildPythonPackage {
              pname = "fwdetect";
              version = "0.1.0";
              src = "${course}/packages/fwdetect";
              pyproject = true;
              build-system = [ ps.hatchling ];
              dependencies = [ ps.numpy ];
              doCheck = false;
            };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              git
              bash
              # The dependencies of pyproject.toml, from nixpkgs: opencv-python's wheels
              # don't run on NixOS. your_program.sh runs this Python directly.
              (python3.withPackages (
                ps: with ps; [
                  (fwdetect ps)
                  alembic
                  fastapi
                  numpy
                  opencv4
                  prometheus-client
                  psycopg
                  pytest
                  python-multipart
                  pyyaml
                  sqlalchemy
                  uvicorn
                ]
              ))
              uv
              ruff
              py-spy
              # psql, and a local server if you want one (the tester can also start its own
              # with podman, or use FRAMEWISE_TEST_DATABASE_URL).
              postgresql
            ];
            PYTHONDONTWRITEBYTECODE = "1";
            # Read by test.sh.
            FRAMEWISE_COURSE_DIR = "${course}";
            FRAMEWISE_TESTER_BIN = "${self.packages.${system}.framewise-tester}/bin/tester";
            FRAMEWISE_TESTER_SRC = "${tester}";
          };
        }
      );
    };
}

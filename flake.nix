{
  description = "Build your own Framewise: the course, its tester and a dev shell per language";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs, ... }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
    in
    {
      packages = forAllSystems (system: {
        tester = nixpkgs.legacyPackages.${system}.buildGoModule {
          pname = "framewise-tester";
          version = self.shortRev or "dirty";
          src = ./tester;
          vendorHash = "sha256-Qimqsyrgew/5eRFhFQ0ckBY/eUJK1A/elB2939iuu+g=";
          subPackages = [ "cmd/tester" ];
          # Its unit tests run in CI (.gitlab-ci.yml); ./test.sh is the check here.
          doCheck = false;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # The course's detector, built from packages/fwdetect.
          fwdetect =
            ps:
            ps.buildPythonPackage {
              pname = "fwdetect";
              version = "0.1.0";
              src = ./packages/fwdetect;
              pyproject = true;
              build-system = [ ps.hatchling ];
              dependencies = [ ps.numpy ];
              doCheck = false;
            };
          # attempts/python: what ./test.sh python runs in.
          python = pkgs.mkShell {
            packages = with pkgs; [
              git
              bash
              # The dependencies of pyproject.toml, from nixpkgs: opencv-python's wheels
              # don't run on NixOS. your_program.sh runs this Python when FRAMEWISE_NIX_PYTHON is set.
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
              # The delivery stages (ci1 to hm2) render the chart with helm. They also need podman,
              # which comes from the system: rootless podman needs setuid helpers nix can't provide.
              kubernetes-helm
              # psql, and a local server if you want one (the tester can also start its own
              # with podman, or use FRAMEWISE_TEST_DATABASE_URL).
              postgresql
            ];
            PYTHONDONTWRITEBYTECODE = "1";
            FRAMEWISE_NIX_PYTHON = "1";
            # Read by test.sh.
            FRAMEWISE_TESTER_BIN = "${self.packages.${system}.tester}/bin/tester";
          };
        in
        {
          inherit python;
          # Working on the course: the python shell plus Go for the tester and course-sdk's tools.
          default = pkgs.mkShell {
            inputsFrom = [ python ];
            packages = with pkgs; [
              go
              gopls
              bun
              gnumake
            ];
            inherit (python) PYTHONDONTWRITEBYTECODE FRAMEWISE_NIX_PYTHON FRAMEWISE_TESTER_BIN;
            CGO_ENABLED = "0";
          };
        }
      );
    };
}

# The dev shell for attempts/python (./test.sh python, nix develop .#python).
{ pkgs }:
let
  # The course's detector, built from packages/fwdetect.
  fwdetect =
    ps:
    ps.buildPythonPackage {
      pname = "fwdetect";
      version = "0.1.0";
      src = ../packages/fwdetect;
      pyproject = true;
      build-system = [ ps.hatchling ];
      dependencies = [ ps.numpy ];
      doCheck = false;
    };
in
{
  packages = with pkgs; [
    # The dependencies of pyproject.toml, from nixpkgs: opencv-python's wheels don't run on
    # NixOS. your_program.sh runs this Python when FRAMEWISE_NIX_PYTHON is set.
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
  env = {
    PYTHONDONTWRITEBYTECODE = "1";
    FRAMEWISE_NIX_PYTHON = "1";
  };
}

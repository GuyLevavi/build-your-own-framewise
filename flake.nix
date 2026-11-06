{
  description = "Build your own Framewise: course, tester and course-sdk tooling";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in {
      devShells = forAll (pkgs: {
        # Python runs in containers (tools/run_tests.sh), so the shell only needs the tooling.
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.bun pkgs.gnumake pkgs.git pkgs.podman ];
          CGO_ENABLED = "0";
        };
      });
    };
}

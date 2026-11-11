#!/usr/bin/env bash
# Regenerate what is derived from the sources, without course-sdk or a network:
#   compiled_starters/python   from starter_templates/{all,python}/code
#   solutions/python/*/code    the compiled starter's files around each solution's own files
#                              (app/, and from ci1 on tests/, .gitlab-ci.yml, Containerfile,
#                              .containerignore and deploy/)
#   solutions/python/*/diff    each stage's changes to those files against the previous stage
#
#   tools/compile.sh
set -euo pipefail
course_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$course_dir"

starter=compiled_starters/python
rm -rf "$starter"
mkdir -p "$starter"
cp -a starter_templates/all/code/. "$starter/"
cp -a starter_templates/python/code/. "$starter/"
rm -rf "$starter/.codecrafters"
cp -a starter_templates/python/code/.codecrafters "$starter/"
{
  sed -n '1,/^set -e/p' starter_templates/python/code/.codecrafters/run.sh |
    sed 's/This script is used to run your program on CodeCrafters/Use this script to run your program LOCALLY./'
  echo
  echo '# In the nix dev shell (./test.sh python), Python and the dependencies come from nixpkgs:'
  echo "# the opencv wheel uv would install doesn't run on NixOS."
  echo 'if [ -n "${FRAMEWISE_NIX_PYTHON:-}" ]; then'
  echo '  exec python -m app.main "$@"'
  echo 'fi'
  grep '^exec ' starter_templates/python/code/.codecrafters/run.sh
} >"$starter/your_program.sh"
chmod +x "$starter/your_program.sh"
for f in "$starter/README.md" "$starter/codecrafters.yml"; do
  sed -i \
    -e 's/{{language_name}}/Python/g' \
    -e 's/{{ user_editable_file }}/app\/main.py/g' \
    -e 's/{{buildpack_with_version}}/python-3.12/g' "$f"
done

# What the learner writes; everything else in a solution comes from the starter.
own=(app tests .gitlab-ci.yml Containerfile .containerignore deploy)
keep=()
for name in "${own[@]}"; do keep+=(! -name "$name"); done

previous="$starter"
for solution in solutions/python/*/; do
  code="$solution/code"
  find "$code" -mindepth 1 -maxdepth 1 "${keep[@]}" -exec rm -rf {} +
  (cd "$starter" && find . -mindepth 1 -maxdepth 1 ! -name app -exec cp -a {} "$course_dir/$code/" \;)
  rm -rf "$solution/diff"
  mkdir -p "$solution/diff"
  (cd "$code" && for name in "${own[@]}"; do if [[ -e "$name" ]]; then find "$name" -type f ! -name '*.pyc' ! -path '*/__pycache__/*'; fi; done | sort) | while read -r file; do
    if ! cmp -s "$previous/$file" "$code/$file"; then
      mkdir -p "$solution/diff/$(dirname "$file")"
      before="$previous/$file"
      [[ -e "$before" ]] || before=/dev/null
      diff -u --label "a/$file" --label "b/$file" "$before" "$code/$file" \
        >"$solution/diff/$file.diff" 2>/dev/null || true
    fi
  done
  previous="$code"
done
echo "compiled $starter and $(ls -d solutions/python/*/ | wc -l) solutions"

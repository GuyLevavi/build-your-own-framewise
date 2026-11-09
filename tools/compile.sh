#!/usr/bin/env bash
# Regenerate what is derived from the sources, without course-sdk or a network:
#   compiled_starters/python   from starter_templates/{all,python}/code
#   solutions/python/*/code    the compiled starter's files around each solution's own app/
#   solutions/python/*/diff    each stage's app/ changes against the previous stage
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
  grep '^exec ' starter_templates/python/code/.codecrafters/run.sh
} >"$starter/your_program.sh"
chmod +x "$starter/your_program.sh"
for f in "$starter/README.md" "$starter/codecrafters.yml"; do
  sed -i \
    -e 's/{{language_name}}/Python/g' \
    -e 's/{{ user_editable_file }}/app\/main.py/g' \
    -e 's/{{buildpack_with_version}}/python-3.12/g' "$f"
done

previous="$starter"
for solution in solutions/python/*/; do
  code="$solution/code"
  # Everything but the learner's own code comes from the starter.
  find "$code" -mindepth 1 -maxdepth 1 ! -name app ! -name tests -exec rm -rf {} +
  (cd "$starter" && find . -mindepth 1 -maxdepth 1 ! -name app -exec cp -a {} "$course_dir/$code/" \;)
  rm -rf "$solution/diff"
  mkdir -p "$solution/diff"
  (cd "$code" && find app -type f -name '*.py' -o -type f -name '*.mako' | sort) | while read -r file; do
    if ! cmp -s "$previous/$file" "$code/$file"; then
      mkdir -p "$solution/diff/$(dirname "$file")"
      diff -u --label "a/$file" --label "b/$file" "$previous/$file" "$code/$file" \
        >"$solution/diff/$file.diff" 2>/dev/null || true
    fi
  done
  previous="$code"
done
echo "compiled $starter and $(ls -d solutions/python/*/ | wc -l) solutions"

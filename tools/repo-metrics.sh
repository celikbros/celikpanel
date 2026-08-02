#!/usr/bin/env bash
set -euo pipefail

# Reproducible source-tree measurements used by ROADMAP and AUTOPSY.
# Product Go code is deliberately limited to cmd/ and internal/; development
# tools do not move the product-size baseline. Deleted index entries are
# ignored, while non-ignored untracked remediation files are included.

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

declare -a go_source=()
declare -a go_tests=()
declare -a panel_source=()
declare -a agent_source=()
declare -a typescript_source=()
declare -a migrations=()

while IFS= read -r -d '' path; do
    [[ -f "$path" ]] || continue

    if [[ "$path" == cmd/* || "$path" == internal/* ]]; then
        if [[ "$path" == *_test.go ]]; then
            go_tests+=("$path")
        elif [[ "$path" == *.go ]]; then
            go_source+=("$path")
            [[ "$path" == cmd/panel/* ]] && panel_source+=("$path")
            [[ "$path" == cmd/agent/* ]] && agent_source+=("$path")
        fi
    fi

    if [[ "$path" == web/src/* ]] &&
        [[ "$path" == *.ts || "$path" == *.tsx ]]; then
        typescript_source+=("$path")
    fi

    if [[ "$path" == internal/db/migrations/*.sql ]]; then
        migrations+=("$path")
    fi
done < <(git ls-files -z --cached --others --exclude-standard)

line_count() {
    local total=0 path count
    for path in "$@"; do
        count="$(awk 'END { print NR + 0 }' "$path")"
        total=$((total + count))
    done
    printf '%d' "$total"
}

matching_line_count() {
    local expression="$1"
    shift
    local total=0 path count
    for path in "$@"; do
        count="$(grep -Ec "$expression" "$path" || true)"
        total=$((total + count))
    done
    printf '%d' "$total"
}

printf 'product_go_source_files=%d\n' "${#go_source[@]}"
printf 'product_go_source_lines=%s\n' "$(line_count "${go_source[@]}")"
printf 'go_test_files=%d\n' "${#go_tests[@]}"
printf 'panel_go_source_files=%d\n' "${#panel_source[@]}"
printf 'panel_go_source_lines=%s\n' "$(line_count "${panel_source[@]}")"
printf 'agent_exec_command_sites=%s\n' \
    "$(matching_line_count '\bexec\.Command(Context)?[[:space:]]*\(' "${agent_source[@]}")"
printf 'typescript_source_files=%d\n' "${#typescript_source[@]}"
printf 'typescript_source_lines=%s\n' "$(line_count "${typescript_source[@]}")"
printf 'raw_fetch_sites=%s\n' \
    "$(matching_line_count '\bfetch[[:space:]]*\(' "${typescript_source[@]}")"
printf 'migration_files=%d\n' "${#migrations[@]}"
printf 'api_route_prefix_registrations=%s\n' \
    "$(grep -Ec 'http\.HandleFunc\("/api/' cmd/panel/main.go || true)"

#!/usr/bin/env bash
set -euo pipefail

export LC_ALL=C

die() {
  printf 'release sequence policy contract failed: %s\n' "$*" >&2
  exit 1
}

expect_rejected() {
  if "$@" >/dev/null 2>&1; then
    die "unexpectedly accepted: $*"
  fi
}

canonical_positive_int64() {
  local value=${1-}

  [[ "$value" =~ ^[1-9][0-9]*$ ]] || return 1
  case ${#value} in
    1|2|3|4|5|6|7|8|9|10|11|12|13|14|15|16|17|18) ;;
    19)
      [[ "$value" < 9223372036854775807 || "$value" == 9223372036854775807 ]] || return 1
      ;;
    *) return 1 ;;
  esac
}

fixed_width_sequence() {
  local value=${1-}

  canonical_positive_int64 "$value" || return 1
  printf '%19s\n' "$value" | tr ' ' '0'
}

sequence_gt() {
  local left_fixed right_fixed

  left_fixed=$(fixed_width_sequence "${1-}") || return 1
  right_fixed=$(fixed_width_sequence "${2-}") || return 1
  [[ "$left_fixed" > "$right_fixed" ]]
}

canonical_release_version() {
  local version=${1-}
  local prerelease identifier
  local -a identifiers=()

  [[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]] || return 1
  prerelease=${version#*-}
  if [[ "$prerelease" != "$version" ]]; then
    IFS=. read -r -a identifiers <<< "$prerelease"
    for identifier in "${identifiers[@]}"; do
      if [[ "$identifier" =~ ^[0-9]+$ && "$identifier" != 0 && "$identifier" == 0* ]]; then
        return 1
      fi
    done
  fi
}

validate_policy_file() {
  local path=${1-}
  local version current previous previous_version previous_commit terminal_byte newline_style='' index
  local -a lines=()

  [[ -f "$path" && ! -L "$path" ]] || return 1
  mapfile -t lines < "$path" || return 1
  [[ ${#lines[@]} == 6 ]] || return 1
  terminal_byte=$(tail -c 1 -- "$path" | od -An -t u1 | tr -d '[:space:]') || return 1
  [[ "$terminal_byte" == 10 ]] || return 1
  for index in "${!lines[@]}"; do
    if [[ "${lines[$index]}" == *$'\r' ]]; then
      [[ "$newline_style" != lf ]] || return 1
      newline_style=crlf
      lines[$index]=${lines[$index]%$'\r'}
    else
      [[ "$newline_style" != crlf ]] || return 1
      newline_style=lf
    fi
  done
  [[ "${lines[0]}" == 'format=celikpanel-release-sequence-policy-v1' ]] || return 1
  [[ "${lines[1]}" == version=* ]] || return 1
  [[ "${lines[2]}" == current=* ]] || return 1
  [[ "${lines[3]}" == previous=* ]] || return 1
  [[ "${lines[4]}" == previous_version=* ]] || return 1
  [[ "${lines[5]}" == previous_commit=* ]] || return 1

  version=${lines[1]#version=}
  current=${lines[2]#current=}
  previous=${lines[3]#previous=}
  previous_version=${lines[4]#previous_version=}
  previous_commit=${lines[5]#previous_commit=}

  canonical_release_version "$version" || return 1
  canonical_positive_int64 "$current" || return 1
  canonical_positive_int64 "$previous" || return 1
  sequence_gt "$current" "$previous" || return 1
  canonical_release_version "$previous_version" || return 1
  [[ "$previous_commit" =~ ^[0-9a-f]{40}$ ]] || return 1

  POLICY_VERSION=$version
  POLICY_CURRENT=$current
  POLICY_PREVIOUS=$previous
  POLICY_PREVIOUS_VERSION=$previous_version
  POLICY_PREVIOUS_COMMIT=$previous_commit
}

assert_exact_assignment() {
  local file=$1 name=$2 value=$3 count

  [[ -f "$file" && ! -L "$file" ]] || die "unsafe or missing assignment file: $file"
  count=$(grep -c "^${name}=" "$file" || true)
  [[ "$count" == 1 ]] || die "$name must occur exactly once in $file"
  grep -Fxq "${name}=${value}" "$file" || die "$name does not match the tracked policy"
}

check_ci_identity() {
  local policy_version=$1 policy_current=$2
  local ref_type=${CELIKPANEL_CI_REF_TYPE-}
  local ref_name=${CELIKPANEL_CI_REF_NAME-}
  local ci_sequence=${CELIKPANEL_CI_RELEASE_SEQUENCE-}

  if [[ -z "$ref_type" ]]; then
    [[ -z "$ref_name" && -z "$ci_sequence" ]] || die 'partial CI release identity was provided'
    return 0
  fi

  [[ "$ref_type" == branch || "$ref_type" == tag ]] || die "unsupported CI ref type: $ref_type"
  [[ -n "$ref_name" ]] || die 'CI ref name is empty'
  if [[ -n "$ci_sequence" ]]; then
    canonical_positive_int64 "$ci_sequence" || die 'CI release sequence is not a canonical positive INT64'
  elif [[ "$ref_type" == tag ]]; then
    die 'CI release sequence is required for a tag'
  fi

  if [[ "$ref_type" == tag ]]; then
    [[ "$ref_name" == "$policy_version" ]] || die 'CI tag does not match policy version'
    [[ "$ci_sequence" == "$policy_current" ]] || die 'CI release sequence does not match policy current'
  fi
}

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/.." && pwd -P)
policy_file="$script_dir/release-sequence-policy"
bootstrap_file="$repo_root/download-portal/get.sh"

canonical_positive_int64 1 || die 'positive INT64 minimum was rejected'
canonical_positive_int64 9223372036854775807 || die 'INT64 maximum was rejected'
expect_rejected canonical_positive_int64 0
expect_rejected canonical_positive_int64 01
expect_rejected canonical_positive_int64 +1
expect_rejected canonical_positive_int64 ' 1'
expect_rejected canonical_positive_int64 '1 '
expect_rejected canonical_positive_int64 9223372036854775808
expect_rejected canonical_positive_int64 10000000000000000000
sequence_gt 42 41 || die 'fixed-width sequence comparison rejected an increase'
sequence_gt 9223372036854775807 9223372036854775806 || die 'fixed-width comparison failed at INT64 maximum'
expect_rejected sequence_gt 41 41
expect_rejected sequence_gt 40 41
fixed_width=$(fixed_width_sequence 42) || die 'could not normalize a valid sequence'
[[ ${#fixed_width} == 19 && "$fixed_width" == *42 ]] || die 'sequence normalization is not fixed-width'

validate_policy_file "$policy_file" || die 'tracked policy is not canonical or strictly increasing'
policy_version=$POLICY_VERSION
policy_current=$POLICY_CURRENT
policy_previous=$POLICY_PREVIOUS
policy_previous_version=$POLICY_PREVIOUS_VERSION
policy_previous_commit=$POLICY_PREVIOUS_COMMIT

[[ "$policy_version" == v0.1.0-alpha.46 ]] || die 'tracked version must be v0.1.0-alpha.46'
[[ "$policy_current" == 46 ]] || die 'tracked current sequence must be 46'
[[ "$policy_previous" == 45 ]] || die 'tracked previous sequence must be 45'
[[ "$policy_previous_version" == v0.1.0-alpha.45 ]] || die 'tracked previous version must be v0.1.0-alpha.45'
[[ "$policy_previous_commit" == 95d976a1fbefce087dafabe017a7e304c9b398ab ]] \
  || die 'tracked previous commit must be the immutable Alpha45 release commit'

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/celikpanel-release-policy.XXXXXXXX")
cleanup() {
  rm -rf -- "$fixture_dir"
}
trap cleanup EXIT HUP INT TERM

sed 's/^current=46$/current=046/' "$policy_file" > "$fixture_dir/noncanonical"
expect_rejected validate_policy_file "$fixture_dir/noncanonical"
sed 's/^current=46$/current=45/' "$policy_file" > "$fixture_dir/not-increasing"
expect_rejected validate_policy_file "$fixture_dir/not-increasing"
sed 's/^current=46$/current=9223372036854775808/' "$policy_file" > "$fixture_dir/overflow"
expect_rejected validate_policy_file "$fixture_dir/overflow"
cp -- "$policy_file" "$fixture_dir/extra-field"
printf '%s\n' 'unexpected=true' >> "$fixture_dir/extra-field"
expect_rejected validate_policy_file "$fixture_dir/extra-field"

assert_exact_assignment "$bootstrap_file" bootstrap_release_version "$policy_version"
assert_exact_assignment "$bootstrap_file" bootstrap_release_sequence "$policy_current"
check_ci_identity "$policy_version" "$policy_current"

git -C "$repo_root" rev-parse --verify --quiet \
  "refs/tags/${policy_previous_version}^{commit}" >/dev/null || \
  die "historical tag is not available: $policy_previous_version"
historical_commit=$(git -C "$repo_root" rev-parse \
  "refs/tags/${policy_previous_version}^{commit}") || \
  die "historical tag commit cannot be resolved: $policy_previous_version"
[[ "$historical_commit" == "$policy_previous_commit" ]] || \
  die 'historical tag does not point to the pinned previous release commit'
historical_bootstrap=$(git -C "$repo_root" show \
  "${policy_previous_version}:download-portal/get.sh") || \
  die "historical tag has no download bootstrap: $policy_previous_version"

historical_sequence_count=$(printf '%s\n' "$historical_bootstrap" | \
  grep -c '^bootstrap_release_sequence=' || true)
case "$historical_sequence_count" in
  0)
    history_result="policy previous fallback (${policy_previous_version} has no bootstrap sequence constant)"
    ;;
  1)
    historical_sequence=$(printf '%s\n' "$historical_bootstrap" | \
      sed -n 's/^bootstrap_release_sequence=//p')
    canonical_positive_int64 "$historical_sequence" || \
      die 'historical bootstrap sequence is not a canonical positive INT64'
    [[ "$historical_sequence" == "$policy_previous" ]] || \
      die 'historical bootstrap sequence does not match policy previous'
    sequence_gt "$policy_current" "$historical_sequence" || \
      die 'current bootstrap sequence does not advance historical sequence'
    history_result="historical bootstrap sequence ${historical_sequence}"
    ;;
  *)
    die 'historical bootstrap sequence assignment is ambiguous'
    ;;
esac

historical_version_count=$(printf '%s\n' "$historical_bootstrap" | \
  grep -c '^bootstrap_release_version=' || true)
case "$historical_version_count" in
  0) ;;
  1)
    historical_version=$(printf '%s\n' "$historical_bootstrap" | \
      sed -n 's/^bootstrap_release_version=//p')
    [[ "$historical_version" == "$policy_previous_version" ]] || \
      die 'historical bootstrap version does not match policy previous version'
    ;;
  *)
    die 'historical bootstrap version assignment is ambiguous'
    ;;
esac

printf 'release sequence policy contract passed: %s -> %s; %s\n' \
  "$policy_previous" "$policy_current" "$history_result"

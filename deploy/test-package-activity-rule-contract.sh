#!/usr/bin/env bash
# One package-activity rule (upd8 F1/F2, 2026-10-01). Whether the host package
# manager is busy is decided only by the Agent (cmd/agent
# service_mutation_lock_linux.go: listed process names, PackageKit's daemon only
# with transaction evidence, apt/dpkg/rpm/pacman locks). The shell scripts ask
# the Agent (--check-*-idle) and must not keep a second process-name list that
# could disagree with it, for example one that counts an idle packagekitd.
# The updater recognises the Agent's refusal by its exact sentence.
# Paket etkinliği kuralı tek yerdedir: Agent. Betikler Agent'a sorar; ikinci bir
# süreç adı listesi tutmaz. Güncelleyici Agent'ın reddini tam cümlesinden tanır.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

scripts=()
for path in "$repo_root"/*.sh "$repo_root"/deploy/*.sh; do
    case ${path##*/} in test-*) continue ;; esac
    scripts+=("$path")
done
[[ ${#scripts[@]} -ge 5 ]] || fail "product scripts not found"
for path in "${scripts[@]}"; do
    if grep -nEi 'packagekit|pkcon|pgrep|pidof|/proc/locks' -- "$path"; then
        fail "${path#"$repo_root"/} decides package activity itself; ask the Agent instead"
    fi
done

agent_rule=$repo_root/cmd/agent/service_mutation_lock_linux.go
agent_idle=$repo_root/cmd/agent/service_mutation_idle.go
grep -Fq 'const packageKitDaemonComm = "packagekitd"' "$agent_rule" \
    || fail "the Agent no longer names PackageKit's daemon in its one rule"
grep -Fq '"%w: the host package manager is active"' "$agent_idle" \
    || fail "the Agent's package-manager refusal sentence changed"
grep -Fq "== *': the host package manager is active'* ]]; then" "$repo_root/update.sh" \
    || fail "update.sh no longer recognises the Agent's package-manager refusal"
grep -Fq 'code=package_manager_busy' "$repo_root/update.sh" \
    || fail "update.sh no longer reports package_manager_busy"

echo "package activity rule contract: ok"

#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL="$ROOT/install.sh"

die() {
    printf 'install localization contract failed: %s\n' "$*" >&2
    exit 1
}

require_literal() {
    local literal=$1
    grep -Fq -- "$literal" "$INSTALL" ||
        die "install.sh is missing: $literal"
}

reject_literal() {
    local literal=$1
    if grep -Fq -- "$literal" "$INSTALL"; then
        die "install.sh still contains the old single-language message: $literal"
    fi
}

bash -n "$INSTALL"

require_literal 'ERROR / HATA:'
require_literal 'step() { c '\''1;36'\'' "==> $(bilingual "$@")"; }'
require_literal 'ok() { c '\''32'\'' "    ✓ $(bilingual "$@")"; }'
require_literal 'warn() { c '\''33'\'' "    $(bilingual "$@")"; }'

require_literal 'step "Small prerequisites (curl, tar, xz, nftables, iproute2)" \'
require_literal '"Küçük ön gereksinimler (curl, tar, xz, nftables, iproute2)"'
require_literal 'step "Automatic security patches (unattended-upgrades)" \'
require_literal '"Otomatik güvenlik yamaları (unattended-upgrades)"'
require_literal 'step "Creating the first administrator" "İlk yönetici oluşturuluyor"'
require_literal 'step "Starting the panel" "Panel başlatılıyor"'
require_literal 'c '\''1;32'\'' "CelikPanel was installed successfully. / CelikPanel başarıyla kuruldu."'
require_literal 'echo "    Services / Servisler: systemctl status celikpanel-agent celikpanel-panel"'
require_literal 'echo "    Logs / Günlükler: journalctl -u celikpanel-panel -f"'

reject_literal 'step "Küçük ön gereksinimler (curl, tar, xz, nftables, iproute2)"'
reject_literal 'step "İlk yönetici oluşturuluyor"'
reject_literal 'step "Panel başlatılıyor"'
reject_literal 'c '\''1;32'\'' "CelikPanel kuruldu."'

printf 'install localization contract passed\n'

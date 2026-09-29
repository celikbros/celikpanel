#!/usr/bin/env bash
# Offline tests only: no guest, no network beyond loopback, no product binary.
set -euo pipefail
cd "$(dirname "$0")/.."
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest -v test_redaction_evidence test_panel_api test_guidance test_dns_probe \
    test_sequence test_guests

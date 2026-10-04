#!/bin/bash
grep -E 'rc=|source_head|^baseline |^good |^defective |^startcheck |^realstart |native_evidence|dry-run lab' /var/tmp/cp-upd13-run/logs/prove-all.out | cut -c1-230

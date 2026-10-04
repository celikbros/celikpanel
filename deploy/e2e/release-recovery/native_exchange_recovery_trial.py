#!/usr/bin/env python3
"""Fixed registered-lab database exchange then recovery reboot; unchanged product bytes.

Uses the common native controller, with its own intent, tracer and proof chain.
No arbitrary process, command, path or installed-host mode is exposed.
"""
from pathlib import Path
import importlib.util
import subprocess
import sys

_spec = importlib.util.spec_from_file_location('native_exchange_recovery_controller', Path(__file__).with_name('native_wal_trial.py'))
controller = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = controller
_spec.loader.exec_module(controller)


def main(argv=None):
    return controller.main(argv, boundary='database-exchange-recovery-reboot')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, TimeoutError, subprocess.SubprocessError) as exc:
        print('native exchange recovery controller refused: ' + type(exc).__name__ + ': ' + str(exc), file=sys.stderr)
        sys.exit(2)

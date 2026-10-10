#!/bin/bash
# set8: the new offline tests against the working tree, without writing bytecode into the repository.
export PYTHONDONTWRITEBYTECODE=1
cd "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery" && python3 -m unittest test_set8_trial 2>&1 | tail -n 8
python3 -I -c "import ast,sys; [ast.parse(open(f).read()) for f in sys.argv[1:]]; print('parse ok')" set8_trial.py guest_set8_native.py

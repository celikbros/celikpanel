#!/bin/bash
ls /proc/sys/fs/binfmt_misc/ 2>/dev/null | head
which powershell.exe 2>/dev/null || ls /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe
timeout 40 /mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -ExecutionPolicy Bypass -File 'C:\Users\alice\AppData\Local\Temp\claude\c--CELIKBROS-PROJECTS-celikpanel\ab34f56e-94b5-4834-a9ea-4e8dbe057e7f\scratchpad\upd13\cdrive.ps1' -Label interop-test; echo rc=$?

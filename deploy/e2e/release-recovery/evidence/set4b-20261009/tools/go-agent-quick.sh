#!/bin/sh
# set4b: quick check of the Agent and Panel packages after the Stop correction (not the final run).
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
GO=/opt/celikpanel-s2/go/bin/go
/opt/celikpanel-s2/go/bin/gofmt -l cmd/agent cmd/panel internal/transport 2>&1 | head
$GO vet ./cmd/agent/ ./cmd/panel/ ./internal/transport/ 2>&1 | head -30
$GO test ./cmd/agent/ -count=1 -run 'Postfix|ServicesPage|MailService|ServiceAction|Stop' 2>&1 | tail -40
$GO test ./cmd/panel/ -count=1 -run 'ServiceAction|Import|Cpmove|Note' 2>&1 | tail -15

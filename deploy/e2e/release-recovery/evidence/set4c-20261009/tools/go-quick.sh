#!/bin/sh
# set4c: quick check of the Agent package after the postconf value rule (not the final run).
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
GO=/opt/celikpanel-s2/go/bin/go
/opt/celikpanel-s2/go/bin/gofmt -l cmd/agent 2>&1 | head
$GO vet ./cmd/agent/ 2>&1 | head -20
$GO test ./cmd/agent/ -count=1 -run 'Postconf|MailTLS|MailHost|Postfix|ServicesPage|MailService|Alias' 2>&1 | tail -30

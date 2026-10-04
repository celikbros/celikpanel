package dnsunitidentity

import (
	"strings"
	"testing"
)

const loadedNamedTarget = "Id=named.service\nNames=bind9.service named.service\nLoadState=loaded\nUnitFileState=enabled\n" +
	"FragmentPath=/usr/lib/systemd/system/named.service\nDropInPaths=\nSourcePath=\nTransient=no\n" +
	"ExecStart={ path=/usr/sbin/named ; argv[]=/usr/sbin/named -f $OPTIONS ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n"

// The masked reading is what systemctl reported for the package guard's
// persistent mask in the 2026-09-29 owner-inverse evidence: no ExecStart,
// fragment /etc/systemd/system/<unit>.
const maskedNamedTarget = "Id=named.service\nNames=named.service\nLoadState=masked\nUnitFileState=masked\n" +
	"FragmentPath=/etc/systemd/system/named.service\nDropInPaths=\nSourcePath=\nTransient=no\n"

const absentNamedTarget = "Id=named.service\nNames=named.service\nLoadState=not-found\nUnitFileState=\n" +
	"FragmentPath=\nDropInPaths=\nSourcePath=\nTransient=no\n"

func TestParseTargetObservationRepresentsNeverStartedTargets(t *testing.T) {
	loaded, err := ParseTargetObservation(loadedNamedTarget)
	if err != nil || loaded.State != TargetLoaded || loaded.Identity.ExecStartPath != "/usr/sbin/named" ||
		ValidateAPTBINDVendorNamedIdentity(loaded.Identity, true) != nil {
		t.Fatalf("loaded target lost its strict identity: %+v %v", loaded, err)
	}
	masked, err := ParseTargetObservation(maskedNamedTarget)
	if err != nil || masked.State != TargetPersistentMask || masked.ID != "named.service" ||
		masked.FragmentPath != "/etc/systemd/system/named.service" {
		t.Fatalf("guard mask not represented: %+v %v", masked, err)
	}
	// systemd may omit empty properties; absence and emptiness are the same.
	if got, err := ParseTargetObservation("Id=named.service\nNames=named.service\nLoadState=masked\nUnitFileState=masked\nFragmentPath=/etc/systemd/system/named.service\n"); err != nil || got.State != TargetPersistentMask {
		t.Fatalf("mask without empty properties refused: %+v %v", got, err)
	}
	absent, err := ParseTargetObservation(absentNamedTarget)
	if err != nil || absent.State != TargetAbsent || absent.ID != "named.service" {
		t.Fatalf("absent target not represented: %+v %v", absent, err)
	}
	// The strict reader still refuses the masked reading: its callers are
	// unchanged.
	if _, err := Parse(strings.Join(identityLinesOf(maskedNamedTarget), "\n")); err == nil ||
		!strings.Contains(err.Error(), "incomplete DNS unit identity") {
		t.Fatalf("strict identity reader accepted a masked unit: %v", err)
	}
}

func TestParseTargetObservationRefusesUnsafeOrAmbiguousStates(t *testing.T) {
	for name, output := range map[string]string{
		"runtime mask": strings.Replace(strings.Replace(maskedNamedTarget, "UnitFileState=masked", "UnitFileState=masked-runtime", 1),
			"/etc/systemd/system/", "/run/systemd/system/", 1),
		"runtime fragment":       strings.Replace(maskedNamedTarget, "/etc/systemd/system/", "/run/systemd/system/", 1),
		"mask with command":      maskedNamedTarget + "ExecStart={ path=/usr/sbin/named ; argv[]=/usr/sbin/named ; ignore_errors=no }\n",
		"mask with drop-in":      strings.Replace(maskedNamedTarget, "DropInPaths=", "DropInPaths=/etc/systemd/system/named.service.d/x.conf", 1),
		"transient mask":         strings.Replace(maskedNamedTarget, "Transient=no", "Transient=yes", 1),
		"mask alias names":       strings.Replace(maskedNamedTarget, "Names=named.service", "Names=bind9.service named.service", 1),
		"absent with fragment":   strings.Replace(absentNamedTarget, "FragmentPath=", "FragmentPath=/usr/lib/systemd/system/named.service", 1),
		"absent unit file state": strings.Replace(absentNamedTarget, "UnitFileState=", "UnitFileState=disabled", 1),
		"bad setting":            strings.Replace(maskedNamedTarget, "LoadState=masked", "LoadState=bad-setting", 1),
		"loaded incomplete":      strings.Replace(loadedNamedTarget, "SourcePath=\n", "", 1),
		"no load state":          strings.Replace(maskedNamedTarget, "LoadState=masked\n", "", 1),
		"duplicate":              maskedNamedTarget + "LoadState=masked\n",
		"unexpected property":    maskedNamedTarget + "MainPID=0\n",
		"malformed":              maskedNamedTarget + "garbage\n",
	} {
		if got, err := ParseTargetObservation(output); err == nil {
			t.Fatalf("%s accepted: %+v", name, got)
		}
	}
}

func identityLinesOf(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line != "" && !strings.HasPrefix(line, "LoadState=") && !strings.HasPrefix(line, "UnitFileState=") {
			lines = append(lines, line)
		}
	}
	return lines
}

package mailtlsconfig

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsartifact"
)

const fixtureCert = "/etc/ssl/celikpanel/_mail/default-cert.pem"
const fixtureKey = "/etc/ssl/celikpanel/_mail/default-key.pem"
const fixtureMap = "/etc/postfix/celikpanel_sni"

func fixture(t *testing.T, kind string) *mailtlsartifact.Plan {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "mailtlsartifact", "testdata", "alpha81-"+kind+".json"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := mailtlsartifact.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func golden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func observation(plan *mailtlsartifact.Plan, modern bool) Observation {
	v := Observation{Postfix: make(map[string]string), Dovecot: []byte(Dovecot(modern, fixtureCert, fixtureKey, plan.SNI))}
	for _, s := range PostfixSettings(plan.Myhostname, fixtureCert, fixtureKey) {
		v.Postfix[s[0]] = s[1] + "\n"
	}
	v.Postfix["tls_server_sni_maps"] = ""
	if len(plan.SNI) > 0 {
		v.Postfix["tls_server_sni_maps"] = "lmdb:" + fixtureMap
		v.PostfixSNI = PostfixSNI(plan.SNI)
	}
	return v
}
func TestActualAlpha81ProducerBytes(t *testing.T) {
	for _, kind := range []string{"empty", "sni"} {
		plan := fixture(t, kind)
		for _, d := range []struct {
			name   string
			modern bool
		}{{"23", false}, {"24", true}} {
			want := golden(t, "alpha81-dovecot-"+d.name+"-"+kind+".conf")
			if got := Dovecot(d.modern, fixtureCert, fixtureKey, plan.SNI); got != string(want) {
				t.Fatalf("Dovecot %s/%s differs from actual Alpha81 producer", d.name, kind)
			}
			observed := observation(plan, d.modern)
			observed.Dovecot = want
			observed.PostfixSNI = golden(t, "alpha81-postfix-"+kind+".map")
			if err := Verify(plan, fixtureCert, fixtureKey, fixtureMap, d.modern, observed); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(PostfixSNI(plan.SNI), golden(t, "alpha81-postfix-"+kind+".map")) {
			t.Fatal("Postfix SNI differs from actual Alpha81 readback contract")
		}
	}
	plan := fixture(t, "empty")
	settings := append(PostfixSettings(plan.Myhostname, fixtureCert, fixtureKey), [2]string{"tls_server_sni_maps", ""})
	got, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(got, '\n'), golden(t, "alpha81-postfix-settings.json")) {
		t.Fatalf("actual Alpha81 Postfix producer differs: %s", got)
	}
}
func TestOwnerChangesAndUnknownObservationsRefused(t *testing.T) {
	for _, kind := range []string{"missing-setting", "changed-setting", "missing-sni-setting", "changed-map-path", "changed-map-type", "changed-map-source", "changed-dovecot", "other-dialect", "changed-host", "invalid-plan"} {
		t.Run(kind, func(t *testing.T) {
			plan := fixture(t, "sni")
			observed := observation(plan, true)
			switch kind {
			case "missing-setting":
				delete(observed.Postfix, "myhostname")
			case "changed-setting":
				observed.Postfix["smtpd_tls_key_file"] = "/owner/private/key"
			case "missing-sni-setting":
				delete(observed.Postfix, "tls_server_sni_maps")
			case "changed-map-path":
				observed.Postfix["tls_server_sni_maps"] = "lmdb:/owner/map"
			case "changed-map-type":
				observed.Postfix["tls_server_sni_maps"] = "texthash:" + fixtureMap
			case "changed-map-source":
				observed.PostfixSNI = append(observed.PostfixSNI, []byte("owner-added.example.test /owner/key /owner/cert\n")...)
			case "changed-dovecot":
				observed.Dovecot = append(observed.Dovecot, []byte("# owner change\n")...)
			case "other-dialect":
				observed.Dovecot = []byte(Dovecot(false, fixtureCert, fixtureKey, plan.SNI))
			case "changed-host":
				plan.Myhostname = "owner.example.test"
			case "invalid-plan":
				plan.Version++
			}
			before, _ := json.Marshal(observed)
			beforePlan, _ := json.Marshal(plan)
			err := Verify(plan, fixtureCert, fixtureKey, fixtureMap, true, observed)
			if err == nil {
				t.Fatal("owner/unknown evidence admitted")
			}
			if strings.Contains(err.Error(), "/owner/private") {
				t.Fatal("owner value leaked in guidance")
			}
			after, _ := json.Marshal(observed)
			afterPlan, _ := json.Marshal(plan)
			if !bytes.Equal(before, after) || !bytes.Equal(beforePlan, afterPlan) {
				t.Fatal("verification normalized evidence")
			}
		})
	}
}
func TestMapBackendsAndEmptyPlan(t *testing.T) {
	plan := fixture(t, "sni")
	for _, kind := range []string{"lmdb", "hash", "btree"} {
		observed := observation(plan, true)
		observed.Postfix["tls_server_sni_maps"] = kind + ":" + fixtureMap
		if err := Verify(plan, fixtureCert, fixtureKey, fixtureMap, true, observed); err != nil {
			t.Fatal(err)
		}
	}
	plan = fixture(t, "empty")
	observed := observation(plan, true)
	observed.Postfix["tls_server_sni_maps"] = "lmdb:" + fixtureMap
	if Verify(plan, fixtureCert, fixtureKey, fixtureMap, true, observed) == nil {
		t.Fatal("owner SNI map erased by fallback-only intent")
	}
	if Verify(nil, fixtureCert, fixtureKey, fixtureMap, true, observed) == nil {
		t.Fatal("missing accepted plan admitted")
	}
}
func TestRenderDoesNotMutateAcceptedPlan(t *testing.T) {
	plan := fixture(t, "sni")
	before, err := mailtlsartifact.Encode(plan)
	if err != nil {
		t.Fatal(err)
	}
	_ = PostfixSNI(plan.SNI)
	_ = Dovecot(true, fixtureCert, fixtureKey, plan.SNI)
	after, err := mailtlsartifact.Encode(plan)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("renderer mutated plan: %v", err)
	}
}

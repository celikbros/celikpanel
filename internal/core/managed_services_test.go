package core

import (
	"reflect"
	"testing"
)

// RequirementsMissing decides whether a dependent tool can be installed, so a
// wrong answer either blocks a valid install or lets a broken one through.
// A group requirement ("web-server") is met by ANY installed member.
func TestRequirementsMissing(t *testing.T) {
	pma := GetManagedServiceByID("phpmyadmin")
	if pma == nil {
		t.Fatal("phpmyadmin missing from catalogue")
	}

	cases := []struct {
		name      string
		installed map[string]bool
		want      []string
	}{
		{"nothing installed", map[string]bool{}, []string{"mariadb", "web-server", "php-fpm"}},
		{"parent only", map[string]bool{"mariadb": true}, []string{"web-server", "php-fpm"}},
		{"web via nginx (group)", map[string]bool{"mariadb": true, "nginx": true}, []string{"php-fpm"}},
		{"web via apache (group)", map[string]bool{"mariadb": true, "apache": true}, []string{"php-fpm"}},
		{"all satisfied", map[string]bool{"mariadb": true, "nginx": true, "php-fpm": true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RequirementsMissing(pma, c.installed)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("missing = %v, want %v", got, c.want)
			}
		})
	}

	// A service with no Requires is always installable.
	if got := RequirementsMissing(GetManagedServiceByID("nginx"), map[string]bool{}); got != nil {
		t.Errorf("nginx has no requirements, got %v", got)
	}
}

// Kind decides how a row is drawn and operated (D-010), so an entry added
// without one renders wrong SILENTLY: no start/stop, a status the panel cannot
// justify. The old code guessed this from `len(SystemNames) == 0` and could not
// be forgotten; an explicit field can, so the guard moves here.
//
// Kind, satırın nasıl çizilip işletileceğini belirler (D-010); Kind'siz eklenen
// bir kalem SESSİZCE yanlış çizilir: başlat/durdur yok, panelin
// gerekçelendiremediği bir durum. Eski kod bunu `len(SystemNames) == 0`'dan
// tahmin ederdi ve unutulamazdı; açık bir alan unutulabilir, bekçi buraya taşınır.
func TestEveryServiceDeclaresKind(t *testing.T) {
	for _, s := range ManagedServices {
		switch s.Kind {
		case KindService, KindRuntime, KindTool:
		case "":
			t.Errorf("%s: Kind is empty — classify it as service, runtime or tool (D-010)", s.ID)
		default:
			t.Errorf("%s: unknown Kind %q", s.ID, s.Kind)
		}
	}
}

// A service/runtime is defined by having a daemon, and the panel reads its
// state from SystemNames. Without one the row can never report "running", so
// it would sit at "stopped" forever and raise a permanent false alarm on the
// dashboard. (The reverse is deliberately NOT asserted: a tool is allowed to
// grow a unit one day. Freezing that direction would restore the very
// conflation D-010 deleted.)
//
// service/runtime, daemon'a sahip olmakla tanımlanır ve panel durumunu
// SystemNames'ten okur. Biri yoksa satır asla "çalışıyor" diyemez, sonsuza dek
// "durdu"da kalır ve panoda kalıcı yanlış alarm üretir. (Tersi bilerek
// doğrulanmaz: bir tool ileride unit kazanabilir. O yönü dondurmak, D-010'un
// sildiği karıştırmayı geri getirirdi.)
func TestDaemonKindsHaveUnits(t *testing.T) {
	for _, s := range ManagedServices {
		if s.Kind == KindTool {
			continue
		}
		if len(s.SystemNames) == 0 {
			t.Errorf("%s: Kind %q has no SystemNames — it could never report running", s.ID, s.Kind)
		}
	}
}

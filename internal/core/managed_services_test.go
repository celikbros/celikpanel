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

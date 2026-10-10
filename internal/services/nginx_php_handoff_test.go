package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// The file Debian's and Ubuntu's nginx packages ship as
// /etc/nginx/snippets/fastcgi-php.conf (nginx-common), byte for byte: the text
// the set3 run of 2026-10-09 placed on an Arch guest by hand, after which a PHP
// site could be created there (evidence set3-20261012, rid3-arch/run-b). The
// PHP vhost used to include it; it now writes its directives out, so that the
// vhost names no file a distribution may not have.
const debianFastCGIPHPSnippet = "# regex to split $uri to $fastcgi_script_name and $fastcgi_path\n" +
	"fastcgi_split_path_info ^(.+?\\.php)(/.*)$;\n\n" +
	"# Check that the PHP script exists before passing it\n" +
	"try_files $fastcgi_script_name =404;\n\n" +
	"# Bypass the fact that try_files resets $fastcgi_path_info\n" +
	"# see: http://trac.nginx.org/nginx/ticket/321\n" +
	"set $path_info $fastcgi_path_info;\n" +
	"fastcgi_param PATH_INFO $path_info;\n\n" +
	"fastcgi_index index.php;\n" +
	"include fastcgi.conf;\n"

const debianFastCGIPHPSnippetSHA256 = "a9dd98bf9631d727f0a846a9c7f4fe6193468a714c782df26d5cc9a7756411f2"

// The PHP location as every vhost was generated before 12 Oct 2026.
const phpLocationWithTheInclude = "include snippets/fastcgi-php.conf;\n" +
	"fastcgi_pass unix:%s;\n" +
	"fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n" +
	"include fastcgi_params;\n"

// nginxDirectives is a configuration text as nginx reads it: comments and
// empty lines are not configuration.
func nginxDirectives(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// phpLocations returns the body of every `location ~ \.php$` block of a
// rendered vhost.
func phpLocations(t *testing.T, vhost string) []string {
	t.Helper()
	const open = "location ~ \\.php$ {\n"
	var bodies []string
	rest := vhost
	for {
		start := strings.Index(rest, open)
		if start < 0 {
			return bodies
		}
		rest = rest[start+len(open):]
		end := strings.Index(rest, "\n    }")
		if end < 0 {
			t.Fatalf("a PHP location is not closed:\n%s", rest)
		}
		bodies = append(bodies, rest[:end+1])
		rest = rest[end:]
	}
}

func renderTestVhost(t *testing.T, data VhostData) string {
	t.Helper()
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	data.ACMEChallengeRoot = testACMEChallengeRoot
	out, err := ng.Render(data)
	if err != nil {
		t.Fatalf("render %+v: %v", data, err)
	}
	return out
}

// A site generated with `include snippets/fastcgi-php.conf;` and the same site
// rendered now are one configuration: nginx reads an include as the included
// file's text in its place, so the old location with Debian's file put in for
// the include must be, directive for directive and in order, the location that
// is rendered now. Same PATH_INFO handling, same try_files guard, same
// fastcgi.conf, and nothing added.
func TestPHPVhostIsTheDebianSnippetWrittenOut(t *testing.T) {
	sum := sha256.Sum256([]byte(debianFastCGIPHPSnippet))
	if got := hex.EncodeToString(sum[:]); got != debianFastCGIPHPSnippetSHA256 {
		t.Fatalf("the pinned text is not the measured file: sha256 %s", got)
	}
	const socket = "/var/run/php/php8.4-fpm-site7.sock"
	before := strings.Replace(
		fmt.Sprintf(phpLocationWithTheInclude, socket),
		"include snippets/fastcgi-php.conf;\n", debianFastCGIPHPSnippet, 1)
	want := nginxDirectives(before)
	if len(want) != 9 {
		t.Fatalf("the expanded location has %d directives, want 9: %q", len(want), want)
	}

	cert, key := "/etc/letsencrypt/live/ornek.com/fullchain.pem", "/etc/letsencrypt/live/ornek.com/privkey.pem"
	for name, data := range map[string]VhostData{
		"http":            {SSLType: "none"},
		"https":           {SSLType: "custom", SSLCert: cert, SSLKey: key},
		"https, forced":   {SSLType: "custom", SSLCert: cert, SSLKey: key, ForceHTTPS: true},
		"www redirect":    {SSLType: "none", RedirectWWW: true},
		"default project": {SSLType: "none", ProjectType: ""},
	} {
		if name != "default project" {
			data.ProjectType = "php"
		}
		data.SiteID, data.Domain, data.DocumentRoot, data.PHPSocket = 7, "ornek.com", "/var/www/celikpanel/subscriptions/1/sites/7/public_html", socket
		bodies := phpLocations(t, renderTestVhost(t, data))
		wantBlocks := 1
		if data.SSLType == "custom" && !data.ForceHTTPS {
			wantBlocks = 2 // the HTTP and the HTTPS server both serve the site
		}
		if len(bodies) != wantBlocks {
			t.Fatalf("%s: %d PHP locations, want %d", name, len(bodies), wantBlocks)
		}
		for _, body := range bodies {
			got := nginxDirectives(body)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("%s: the PHP location is not the old one with the include expanded\n got: %q\nwant: %q", name, got, want)
			}
		}
	}
}

// No rendered vhost of any project type names a file under `snippets/`, and
// the only files a vhost includes are nginx's own `fastcgi.conf` and
// `fastcgi_params`, which every nginx package installs in /etc/nginx (Arch has
// no /etc/nginx/snippets at all: measured, set3 finding P5b).
func TestNoVhostNamesADistributionSnippet(t *testing.T) {
	cert, key := "/etc/ssl/ornek.pem", "/etc/ssl/ornek.key"
	shapes := []VhostData{
		{ProjectType: "php", PHPSocket: "/run/php-fpm/php8.5-fpm-site2.sock"},
		{ProjectType: "static"},
		{ProjectType: "node", AppPort: 3000},
		{ProjectType: "proxy", ForwardTo: "http://127.0.0.1:8080"},
		{ProjectType: "forwarding", ForwardTo: "https://example.org", ForwardCode: 302},
	}
	for _, shape := range shapes {
		for _, tls := range []bool{false, true} {
			for _, www := range []bool{false, true} {
				data := shape
				data.SiteID, data.Domain, data.DocumentRoot = 2, "ornek.com", "/var/www/celikpanel/subscriptions/1/sites/2/public_html"
				data.SSLType, data.RedirectWWW = "none", www
				data.ACMEChallengeNames = []string{"mail.ornek.com"}
				if tls {
					data.SSLType, data.SSLCert, data.SSLKey, data.HSTSEnabled, data.HSTSMaxAge = "custom", cert, key, true, 300
				}
				vhost := renderTestVhost(t, data)
				if strings.Contains(vhost, "snippets/") {
					t.Errorf("%s (tls %v, www %v): the vhost names a file under snippets/", shape.ProjectType, tls, www)
				}
				for _, directive := range nginxDirectives(vhost) {
					if !strings.HasPrefix(directive, "include ") {
						continue
					}
					if directive != "include fastcgi.conf;" && directive != "include fastcgi_params;" {
						t.Errorf("%s (tls %v, www %v): the vhost includes a file that is not nginx's own: %q", shape.ProjectType, tls, www, directive)
					}
					if shape.ProjectType != "php" {
						t.Errorf("%s: a vhost that runs no PHP includes %q", shape.ProjectType, directive)
					}
				}
			}
		}
	}
}

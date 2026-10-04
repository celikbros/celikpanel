package main

import (
	"log"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

// Setup review proves, read-only, that the web server and the site users can
// walk to the hosting base before the owner reaches the point of creating a
// site (native finding P3, 1 Oct 2026). A directory above
// /var/www/celikpanel that already exists and blocks them is the owner's:
// setup never changes it. Review names it as a blocker with its mode and
// owner, the server owner runs the command the wizard shows, and reviews a
// revised plan. A missing directory is no blocker: the Agent creates it at
// 0755 root:root when the first site is created.
//
// Kurulum incelemesi, sahip site oluşturma noktasına gelmeden önce web
// sunucusunun ve site kullanıcılarının barındırma köküne yürüyebildiğini salt
// okuyarak kanıtlar (yerel bulgu P3). /var/www/celikpanel'in üstünde var olan
// ve onları engelleyen dizin sahibindir: kurulum onu asla değiştirmez.
// İnceleme onu kipi ve sahibiyle bir engel olarak adlandırır; sunucu sahibi
// sihirbazın gösterdiği komutu çalıştırır ve gözden geçirilmiş bir planı
// inceler. Eksik dizin engel değildir: Agent onu ilk sitede 0755 root:root
// oluşturur.

const serverSetupHostingRootBlocker = "server_setup_hosting_root_not_traversable"

// serverSetupHostingRootProbe is replaced in tests.
var serverSetupHostingRootProbe = hostingpath.ProbeHostingRoot

// serverSetupHostsSites is true for the profiles that host sites; they are
// the ones that already prepare native cron.
func serverSetupHostsSites(draft serverSetupDraft) bool {
	return serverSetupNeedsNativeCron(draft)
}

// serverSetupHostingRootBlockerCode returns
// "server_setup_hosting_root_not_traversable:<mode>:<owner>:<group>:<directory>"
// for a blocking directory, or "" when nothing blocks. The directory is last
// so the wizard can read it whole. An inspection error is not a verified
// block: it is logged, and site creation repeats the proof on the Agent.
func serverSetupHostingRootBlockerCode() string {
	block, err := serverSetupHostingRootProbe()
	if err != nil {
		log.Printf("[setup] hosting root traversal could not be inspected: %v", err)
		return ""
	}
	if block == nil {
		return ""
	}
	return strings.Join([]string{
		serverSetupHostingRootBlocker, block.ModeText(), block.Owner, block.Group, block.Directory,
	}, ":")
}

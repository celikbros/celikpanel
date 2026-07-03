package main

import (
	"fmt"
	"os"
	"os/exec"
)

// ConfigurePowerDNSRequest contains DB connection info
type ConfigurePowerDNSRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
}

// ConfigurePowerDNS writes the config and restarts the service
// We use sudo for all operations to ensure it works even if Agent is not running as root
func (a *Agent) ConfigurePowerDNS(req *ConfigurePowerDNSRequest, resp *bool) error {
	config := fmt.Sprintf(`#################################
# POWERDNS CONFIGURATION
# Managed by CelikPanel
#################################

launch=gpgsql

gpgsql-host=%s
gpgsql-port=%d
gpgsql-user=%s
gpgsql-password=%s
gpgsql-dbname=%s
gpgsql-dnssec=yes

# We use single quotes (standard libpq format) for options value to ensure search_path is parsed correctly
gpgsql-extra-connection-parameters=options='-c search_path=pdns'

webserver=no
api=no
`, req.Host, req.Port, req.User, req.Password, req.DBName)

	confPath := "/etc/powerdns/pdns.d/celikpanel.conf"
	
	// Write to temp file first to avoid shell quoting issues with sudo
	tmpFile, err := os.CreateTemp("", "pdns-config-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up temp file on function exit

	if _, err := tmpFile.WriteString(config); err != nil {
		return fmt.Errorf("failed to write to temp file: %v", err)
	}
	tmpFile.Close() // Close before moving

	// Move temp file to destination using sudo
	if out, err := exec.Command("sudo", "mv", tmpFile.Name(), confPath).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to move config file: %s: %s", err, string(out))
	}

	// Set permissions to root:pdns 640 so PowerDNS can read it but others cannot
	// We assume 'pdns' group exists (standard in packages). If not, it might fail chown, 
	// in which case admin should check logs.
	if err := exec.Command("sudo", "chown", "root:pdns", confPath).Run(); err != nil {
		// Fallback to root:root if pdns group doesn't exist?
		// But log it.
		fmt.Printf("Warning: failed to chown root:pdns: %v\n", err)
	}
	exec.Command("sudo", "chmod", "640", confPath).Run()

	// Remove bind backend if exists
	exec.Command("sudo", "rm", "-f", "/etc/powerdns/pdns.d/bind.conf").Run()

	// Fix Port 53
	resolvedConf := "/etc/systemd/resolved.conf"
	exec.Command("sudo", "sed", "-i", "s/#DNSStubListener=yes/DNSStubListener=no/", resolvedConf).Run()
	exec.Command("sudo", "sed", "-i", "s/DNSStubListener=yes/DNSStubListener=no/", resolvedConf).Run()
	
	// Restart resolved
	if out, err := exec.Command("sudo", "systemctl", "restart", "systemd-resolved").CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to restart resolved: %s\n", string(out))
	}
	
	// Fix resolv.conf
	exec.Command("sudo", "rm", "-f", "/etc/resolv.conf").Run()
	exec.Command("sudo", "ln", "-sf", "/run/systemd/resolve/resolv.conf", "/etc/resolv.conf").Run()

	// Restart PowerDNS
	if output, err := exec.Command("sudo", "systemctl", "restart", "pdns").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart pdns: %s", string(output))
	}

	*resp = true
	return nil
}

// SyncDNSZone creates or updates a zone in PDNS
// Useful when adding a domain panel-side
type SyncDNSZoneRequest struct {
	DomainName string `json:"domain_name"`
	Type       string `json:"type"` // NATIVE, MASTER, SLAVE
}

func (a *Agent) SyncDNSZone(req *SyncDNSZoneRequest, resp *bool) error {
	// This might not be needed if Panel writes directly to DB.
	// But restarting/reloading might be needed?
	// PowerDNS with DB backend changes are usually immediate, assuming caching settings.
	// We can flush cache.
	
	exec.Command("pdns_control", "purge", req.DomainName).Run()
	*resp = true
	return nil
}

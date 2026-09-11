package main

import (
	"context"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func TestRemoteDNSNamespaceReservationIsSymmetricForLocalDomainsAndAliases(t *testing.T) {
	f := newRemoteDNSHTTPFixture(t)
	connection := f.connect(t)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.44")
	f.domain(t, "victim.example.test", nil)
	if err := f.origin.ensureRemoteDomainDNS(context.Background(), "victim.example.test"); err != nil {
		t.Fatal(err)
	}
	sub := seedSetupDNSOwner(t, f.receiver)
	local := &core.Domain{SubscriptionID: sub, Name: "safe.local.test", Status: "active", DNSManagement: setupDNSModeLocal}
	repo := repositories.NewPostgresDomainRepository(f.receiver.db.GetDB())
	if err := repo.Create(context.Background(), local); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"victim.example.test", "child.victim.example.test", "example.test"} {
		domain := &core.Domain{SubscriptionID: sub, Name: name, Status: "active", DNSManagement: setupDNSModeLocal}
		if err := repo.Create(context.Background(), domain); err == nil {
			t.Fatalf("local domain adopted remote namespace %s", name)
		}
		if _, err := f.receiver.db.GetDB().Exec(`INSERT INTO domain_aliases(domain_id,alias) VALUES(?,?)`, local.ID, name); err == nil {
			t.Fatalf("local alias adopted remote namespace %s", name)
		}
		if _, err := f.receiver.db.GetDB().Exec(`UPDATE domains SET name=? WHERE id=?`, name, local.ID); err == nil {
			t.Fatalf("local rename adopted remote namespace %s", name)
		}
		if _, err := f.receiver.db.GetDB().Exec(`INSERT INTO pdns_domains(name,type) VALUES(?,'MASTER')`, name); err == nil {
			t.Fatalf("local DNS zone adopted remote namespace %s", name)
		}
	}
	if _, err := f.receiver.db.GetDB().Exec(`INSERT INTO domain_aliases(domain_id,alias) VALUES(?,'reserved.other.test')`, local.ID); err != nil {
		t.Fatal(err)
	}
	payload, err := canonicalRemoteDNSPublication(remoteDNSPublication{Domain: "reserved.other.test", Generation: 1, Records: []DNSRecord{{Name: "reserved.other.test", Type: "A", Content: "192.0.2.44", TTL: 300}}})
	if err != nil {
		t.Fatal(err)
	}
	var receipt remoteDNSPublicationReceipt
	if err = remoteDNSExchange(context.Background(), connection.Endpoint, "/api/v1/dns/remote/receiver/publish", connection.ID+"."+connection.credential, payload, &receipt); err == nil {
		t.Fatal("receiver grant adopted a local alias namespace")
	}
	if _, err = f.receiver.db.GetDB().Exec(`INSERT INTO remote_dns_zone_ownership(zone_name,client_id) VALUES('reserved.other.test',?)`, connection.ID); err == nil {
		t.Fatal("direct ownership insert bypassed local hostname reservation")
	}
	var count int
	if err = f.receiver.db.GetDB().QueryRow(`SELECT COUNT(*) FROM domains WHERE id=? AND name='safe.local.test'`, local.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("rejected namespace mutation changed existing local domain")
	}
}

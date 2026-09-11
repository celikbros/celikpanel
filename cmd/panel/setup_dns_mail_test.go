package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

type setupMailResolver struct {
	mx        []*net.MX
	addresses []string
	err       error
}

func (r setupMailResolver) LookupMX(context.Context, string) ([]*net.MX, error) { return r.mx, r.err }
func (r setupMailResolver) LookupHost(context.Context, string) ([]string, error) {
	return r.addresses, r.err
}

func TestSetupDNSExternalMailRequiresPublishedMXAndExactMailAddresses(t *testing.T) {
	const domain = "customer.example.test"
	good := setupMailResolver{mx: []*net.MX{{Host: "mail." + domain + ".", Pref: 10}}, addresses: []string{"192.0.2.2"}}
	for _, tc := range []struct {
		name     string
		resolver setupMailResolver
		want     string
	}{
		{"exact", good, "ok"},
		{"missing_mx", setupMailResolver{}, "fail"},
		{"provider_mx", setupMailResolver{mx: []*net.MX{{Host: "mail.provider.test.", Pref: 10}}, addresses: good.addresses}, "fail"},
		{"mixed_primary_mx", setupMailResolver{mx: append(good.mx, &net.MX{Host: "mail.provider.test.", Pref: 10}), addresses: good.addresses}, "fail"},
		{"wrong_address", setupMailResolver{mx: good.mx, addresses: []string{"192.0.2.3"}}, "fail"},
		{"extra_unserved_ipv6", setupMailResolver{mx: good.mx, addresses: []string{"192.0.2.2", "2001:db8::1"}}, "fail"},
		{"resolver_down", setupMailResolver{err: errors.New("temporary failure")}, "unknown"},
		{"nxdomain", setupMailResolver{err: &net.DNSError{IsNotFound: true}}, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := externalMailDNSHealth(context.Background(), domain, "192.0.2.2", "", tc.resolver)
			if result.Status != tc.want {
				t.Fatalf("health=%+v", result)
			}
		})
	}
}

func TestSetupDNSExternalMailInstructionsContainDeliveryAndSigningRecords(t *testing.T) {
	auth := mailAuthStatus{
		SPF:   mailAuthRecord{Name: "customer.example.test", Recommended: "v=spf1 a mx ~all"},
		DKIM:  mailAuthRecord{Name: "celik._domainkey.customer.example.test", Recommended: "v=DKIM1; k=rsa; p=public-key"},
		DMARC: mailAuthRecord{Name: "_dmarc.customer.example.test", Recommended: "v=DMARC1; p=none"},
	}
	records := externalMailDNSRecords("customer.example.test", "192.0.2.2", "", auth)
	if len(records) != 5 {
		t.Fatalf("records=%+v", records)
	}
	if records[0].Type != "MX" || records[0].Content != "mail.customer.example.test" || records[0].Prio != 10 {
		t.Fatalf("mail exchange=%+v", records[0])
	}
	if records[1].Type != "A" || records[1].Name != records[0].Content || records[1].Content != "192.0.2.2" {
		t.Fatalf("mail address=%+v", records[1])
	}
	for _, record := range records {
		if record.Type == "SOA" || record.Type == "NS" {
			t.Fatal("invented provider authority")
		}
	}
	auth.DKIM.Recommended = ""
	if got := externalMailDNSRecords("customer.example.test", "192.0.2.2", "", auth); len(got) != 4 {
		t.Fatal("DKIM instruction must wait for a real generated key")
	}
}

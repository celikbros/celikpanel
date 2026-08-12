package transport

import "testing"

func TestIssuePanelCertificateV2WireContractIncludesEveryEffectiveField(t *testing.T) {
	request := IssuePanelCertificateV2Request{
		MutationRequestID: "request", MutationOwnerID: "owner",
		Domain: "panel.example.test", Email: "admin@example.test",
		TLSDir: "/var/lib/celikpanel/tls", ExpectedBuildCommit: "abcdef",
	}
	if request.MutationRequestID == "" || request.MutationOwnerID == "" ||
		request.Domain == "" || request.Email == "" || request.TLSDir == "" ||
		request.ExpectedBuildCommit == "" {
		t.Fatalf("V2 request lost an effective field: %#v", request)
	}
	var response IssuePanelCertificateV2Response
	response.Issued = true
	if !response.Issued {
		t.Fatal("V2 response lost issuance result")
	}
}

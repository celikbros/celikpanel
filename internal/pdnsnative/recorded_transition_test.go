package pdnsnative

import "testing"

func TestRecordedFreshPrimaryRejectsSameShapeOwnerEdit(t *testing.T) {
	staged, running := measuredFreshPair(t)
	catalog := "catalog-c000020a.celikpanel.invalid"
	recorded, err := ObserveFreshPrimaryCatalogTransition(staged, running, catalog, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecordedFreshPrimaryCatalogTransition(staged, running, catalog, 1, recorded); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecordedFreshPrimaryCatalogTransition(staged, running, catalog, 1, RecordedTransition{}); err == nil {
		t.Fatal("accepted missing durable poststart observation")
	}
	ownerEdited := cloneSnapshot(t, running)
	ownerEdited.Tables["domainmetadata"][0][3] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := VerifyFreshPrimaryCatalogTransition(staged, ownerEdited, catalog, 1); err != nil {
		t.Fatalf("test edit must retain native effect shape: %v", err)
	}
	if err := VerifyRecordedFreshPrimaryCatalogTransition(staged, ownerEdited, catalog, 1, recorded); err == nil {
		t.Fatal("accepted same-shape owner edit after durable observation")
	}
}

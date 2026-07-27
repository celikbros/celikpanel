package main

import (
	"reflect"
	"testing"
)

func TestDesiredAliasCertificateNamesAddsAndRemovesWithoutMovingPrimary(t *testing.T) {
	current := []string{"example.test", "alias-a.example.test", "www.example.test"}
	added, err := desiredAliasCertificateNames(current, "alias-b.example.test", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := added, []string{
		"example.test",
		"alias-a.example.test",
		"alias-b.example.test",
		"www.example.test",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("added names = %v, want %v", got, want)
	}

	removed, err := desiredAliasCertificateNames(added, "", "ALIAS-A.EXAMPLE.TEST")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := removed, []string{
		"example.test",
		"alias-b.example.test",
		"www.example.test",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed names = %v, want %v", got, want)
	}
}

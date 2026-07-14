package solver

import "testing"

func TestCities_ReturnsAllRegisteredCitiesInOrder(t *testing.T) {
	want := []string{
		"Tashkent", "Nurafshon", "Nukus", "Andijan", "Bukhara", "Fergana",
		"Jizzakh", "Namangan", "Navoiy", "Qarshi", "Samarkand", "Gulistan",
		"Termez", "Urgench",
	}
	got := Cities()
	if len(got) != len(want) {
		t.Fatalf("Cities() returned %d entries, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("Cities()[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

func TestCities_ReturnsADefensiveCopy(t *testing.T) {
	got := Cities()
	got[0].Name = "mutated"
	if Cities()[0].Name != "Tashkent" {
		t.Error("mutating the returned slice affected the internal registry — Cities() is not a defensive copy")
	}
}

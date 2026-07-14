package solver

// City is one entry in the urban coordinate ledger.
type City struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// cityLedger is the canonical, package-private registry of regional target
// cities. It is never mutated after init; Cities returns a defensive copy
// so callers can't reach in and change it.
//
// Covers all 14 of Uzbekistan's top-level administrative divisions (12
// regions, the Republic of Karakalpakstan, and Tashkent city itself as an
// independent capital district), each represented by its administrative
// center, so a rupture anywhere in the country has a genuine early-warning
// target near it rather than only the 5 largest cities.
var cityLedger = []City{
	{Name: "Tashkent", Lat: 41.2995, Lng: 69.2401},           // capital city
	{Name: "Nurafshon", Lat: 41.0167, Lng: 69.3417},          // Tashkent Region
	{Name: "Nukus", Lat: 42.4531, Lng: 59.6103},              // Republic of Karakalpakstan
	{Name: "Andijan", Lat: 40.7821, Lng: 72.3442},            // Andijan Region
	{Name: "Bukhara", Lat: 39.7747, Lng: 64.4286},            // Bukhara Region
	{Name: "Fergana", Lat: 40.3894, Lng: 71.7978},            // Fergana Region
	{Name: "Jizzakh", Lat: 40.1158, Lng: 67.8422},            // Jizzakh Region
	{Name: "Namangan", Lat: 40.9983, Lng: 71.6726},           // Namangan Region
	{Name: "Navoiy", Lat: 40.0844, Lng: 65.3792},             // Navoiy Region
	{Name: "Qarshi", Lat: 38.8606, Lng: 65.7891},             // Qashqadaryo Region
	{Name: "Samarkand", Lat: 39.6542, Lng: 66.9597},          // Samarqand Region
	{Name: "Gulistan", Lat: 40.4897, Lng: 68.7842},           // Sirdaryo Region
	{Name: "Termez", Lat: 37.2242, Lng: 67.2783},             // Surxondaryo Region
	{Name: "Urgench", Lat: 41.5506, Lng: 60.6317},            // Xorazm Region
}

// Cities returns a defensive copy of the urban coordinate ledger.
func Cities() []City {
	out := make([]City, len(cityLedger))
	copy(out, cityLedger)
	return out
}

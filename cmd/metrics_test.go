package cmd

import "testing"

// Metrics are scoped to one resolved geography, so a bare ZIP, a ZIP+4, and a
// state code are all wrong here. The API rejects every one of them, but with
// its shared geo_id validator message, which lists "5-digit ZIP" among the
// accepted formats and so names the exact format it just refused. These cases
// pin the local guard that answers before the request goes out.
func TestValidateMetricsGeoIDRejectsWrongScopeForms(t *testing.T) {
	cfg := newMetricsConfig("cities", "city", true)

	for _, geoID := range []string{
		"92024",      // bare ZIP
		"92024-1234", // ZIP+4
		"CA",         // 2-letter state code
		"ca",         // lowercase: /permits/search returns 200 for geo_id=ca, so
		"Ca",         // users learn this form and carry it here
	} {
		if err := validateMetricsGeoID(cfg, geoID); err == nil {
			t.Errorf("expected %q to be rejected as out of scope for city metrics", geoID)
		}
	}
}

// The guard must never reject on a prefix heuristic. Opaque geo_ids are
// 11-character base64url strings whose alphabet includes the underscore, so a
// server-issued ID can legitimately look like a fabricated prefixed format.
// Refusing one would be unrecoverable for the user — there is no bypass flag —
// and invisible server-side, because no request is made. Wrong-type opaque IDs
// must also pass through: a county ID is indistinguishable from a city ID here,
// and the API answers those with a message that already names the problem.
func TestValidateMetricsGeoIDNeverRejectsOpaqueIDs(t *testing.T) {
	cfg := newMetricsConfig("cities", "city", true)

	for _, geoID := range []string{
		"RMjg6rIIh2k", // a real city geo_id
		"xBw08qOlcdc", // a real county geo_id: wrong type, but not detectable here
		"w8aD_ZCQmSE", // real, underscore in the middle
		"oT_pAw3pfKY", // real, underscore at position 3
		"gi9bG__TnJA", // real, double underscore
		"ZIP_AAAAAAA", // shaped like a fabricated prefix, but a legal 11-char ID
		"city_XYZabc", // same, lowercase
	} {
		if err := validateMetricsGeoID(cfg, geoID); err != nil {
			t.Errorf("expected opaque geo_id %q to pass through to the API, got %v", geoID, err)
		}
	}
}

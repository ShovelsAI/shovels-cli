package cmd

import (
	"net/url"
	"testing"
)

func TestDryRunFlagRegistered(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("dry-run")
	if f == nil {
		t.Fatal("expected --dry-run persistent flag on root command")
	}
	if f.DefValue != "false" {
		t.Errorf("expected default false, got %q", f.DefValue)
	}
}

func TestValuesToMapSingleValues(t *testing.T) {
	q := url.Values{
		"geo_id":      {"92024"},
		"permit_from": {"2024-01-01"},
	}
	m := valuesToMap("/permits/search", q)

	if m["geo_id"] != "92024" {
		t.Errorf("expected geo_id=92024, got %v", m["geo_id"])
	}
	if m["permit_from"] != "2024-01-01" {
		t.Errorf("expected permit_from=2024-01-01, got %v", m["permit_from"])
	}
}

func TestValuesToMapMultiValues(t *testing.T) {
	q := url.Values{}
	q.Add("permit_tags", "solar")
	q.Add("permit_tags", "roofing")

	m := valuesToMap("/permits/search", q)
	tags, ok := m["permit_tags"].([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", m["permit_tags"])
	}
	if len(tags) != 2 || tags[0] != "solar" || tags[1] != "roofing" {
		t.Errorf("expected [solar roofing], got %v", tags)
	}
}

func TestValuesToMapArrayParamSingleValue(t *testing.T) {
	q := url.Values{}
	q.Add("permit_tags", "solar")

	m := valuesToMap("/permits/search", q)
	tags, ok := m["permit_tags"].([]string)
	if !ok {
		t.Fatalf("expected []string for single-element array param, got %T", m["permit_tags"])
	}
	if len(tags) != 1 || tags[0] != "solar" {
		t.Errorf("expected [solar], got %v", tags)
	}
}

func TestValuesToMapSizeIsInteger(t *testing.T) {
	q := url.Values{
		"size": {"50"},
	}
	m := valuesToMap("/permits/search", q)
	size, ok := m["size"].(int)
	if !ok {
		t.Fatalf("expected size to be int, got %T", m["size"])
	}
	if size != 50 {
		t.Errorf("expected size=50, got %d", size)
	}
}

func TestValuesToMapGeoIDRemainsString(t *testing.T) {
	q := url.Values{
		"geo_id": {"92024"},
	}
	m := valuesToMap("/permits/search", q)
	_, ok := m["geo_id"].(string)
	if !ok {
		t.Errorf("expected geo_id to remain string, got %T", m["geo_id"])
	}
}

func TestValuesToMapNilQuery(t *testing.T) {
	m := valuesToMap("/permits/search", nil)
	if len(m) != 0 {
		t.Errorf("expected empty map for nil query, got %v", m)
	}
}

func TestValuesToMapDeterministicKeyOrder(t *testing.T) {
	q := url.Values{
		"z_param": {"last"},
		"a_param": {"first"},
		"m_param": {"middle"},
	}
	m := valuesToMap("/permits/search", q)

	// Verify all keys present (order is internal to map, but the function
	// sorts for JSON encoder iteration consistency).
	if len(m) != 3 {
		t.Errorf("expected 3 keys, got %d", len(m))
	}
}

// property_type is a repeated key on every search route but a single scalar on
// the metrics routes, which take one value, so its dry-run rendering cannot be
// decided from the parameter name alone. A global entry would render the
// metrics request as a one-element array; these cases pin both shapes.
func TestValuesToMapPropertyTypeShapeIsEndpointScoped(t *testing.T) {
	q := url.Values{"property_type": {"residential"}}

	for _, tc := range []struct {
		endpoint  string
		wantArray bool
	}{
		{"/properties/search", true},
		{"/decisions/search", true},
		{"/permits/search", true},
		{"/contractors/search", true},
		{"/cities/RMjg6rIIh2k/metrics/current", false},
	} {
		m := valuesToMap(tc.endpoint, q)
		_, gotArray := m["property_type"].([]string)
		if gotArray != tc.wantArray {
			t.Errorf("%s: property_type array=%v, want %v (rendered %T: %v)",
				tc.endpoint, gotArray, tc.wantArray, m["property_type"], m["property_type"])
		}
	}
}

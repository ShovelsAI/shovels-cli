//go:build eval

package evals

// Scenario defines a blind task for an LLM agent to complete using only
// the shovels CLI and its --help output.
type Scenario struct {
	Name           string   // test name
	Task           string   // natural language prompt — no CLI jargon or hints
	Domain         string   // expected resource: "permits" or "contractors"
	MustHaveFields []string // dot-separated JSON paths in final_output (e.g. "meta.count")
	MinResults     int      // minimum items in data array (0 = any)
}

var scenarios = []Scenario{
	{
		Name:           "SolarPermits",
		Task:           `Show me residential solar permits in Encinitas from 2024.`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "ElectricalContractor",
		Task:           `I need an electrical contractor in California — who's highly rated?`,
		Domain:         "contractors",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "CityPermits",
		Task:           `Find building permits issued in Miami in 2024.`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "SolarInTexas",
		Task:           `What solar permits were filed in Texas in 2024?`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
}

package contract

// transportFlags returns the flags a command honors when it builds its own HTTP
// client from the resolved config and can print the resolved request instead of
// sending it.
func transportFlags() []string {
	return []string{FlagNoRetry, FlagTimeout, FlagDryRun}
}

// paginatedFlags returns the transport flags plus the two that bound how many
// records a command collects, ordered as root registers them.
func paginatedFlags() []string {
	return append([]string{FlagLimit, FlagMaxRecords}, transportFlags()...)
}

// honorsNothing is the contract of a command that reads none of the five.
func honorsNothing() Record {
	return Record{Mode: ModeNone}
}

// transportOnly is the contract of a command that issues exactly one request
// through a client built from the transport flags.
func transportOnly() Record {
	return Record{Honored: transportFlags(), Mode: ModeNone}
}

// cursorPaginated is the contract of a command that follows the endpoint's
// continuation cursor.
func cursorPaginated() Record {
	return Record{Honored: paginatedFlags(), Mode: ModeCursor}
}

// serverCapped is the contract of a command whose endpoint returns at most
// maxRecords records and no cursor.
func serverCapped(maxRecords int) Record {
	return Record{Honored: paginatedFlags(), Mode: ModeServerCapped, Cap: maxRecords}
}

// records is the authoritative contract per command path. help and the four
// completion leaves are cobra's, registered during Execute; every other entry is
// this project's. A command compiled behind a build tag files its record through
// Register instead.
//
// The OpenAPI spec declares no size parameter at all for the four capped
// searches, so it cannot express their caps. Only a live query matching more
// rows than the cap is authority on a value.
var records = map[string]Record{
	"config show":           honorsNothing(),
	"schema":                honorsNothing(),
	"version":               honorsNothing(),
	"help":                  honorsNothing(),
	"completion bash":       honorsNothing(),
	"completion fish":       honorsNothing(),
	"completion zsh":        honorsNothing(),
	"completion powershell": honorsNothing(),

	// --dry-run previews the config file write rather than an HTTP request.
	"config set": {Honored: []string{FlagDryRun}, Mode: ModeNone},

	"permits get":            transportOnly(),
	"properties get":         transportOnly(),
	"decisions get":          transportOnly(),
	"contractors get":        transportOnly(),
	"contractors metrics":    transportOnly(),
	"usage":                  transportOnly(),
	"cities coverage":        transportOnly(),
	"counties coverage":      transportOnly(),
	"jurisdictions coverage": transportOnly(),
	"states coverage":        transportOnly(),
	"zipcodes coverage":      transportOnly(),

	"addresses search":     serverCapped(20),
	"cities search":        serverCapped(15),
	"counties search":      serverCapped(15),
	"jurisdictions search": serverCapped(15),

	"permits search":                cursorPaginated(),
	"properties search":             cursorPaginated(),
	"decisions search":              cursorPaginated(),
	"contractors search":            cursorPaginated(),
	"contractors permits":           cursorPaginated(),
	"contractors employees":         cursorPaginated(),
	"addresses residents":           cursorPaginated(),
	"tags list":                     cursorPaginated(),
	"states search":                 cursorPaginated(),
	"zipcodes search":               cursorPaginated(),
	"addresses metrics current":     cursorPaginated(),
	"addresses metrics monthly":     cursorPaginated(),
	"cities metrics current":        cursorPaginated(),
	"cities metrics monthly":        cursorPaginated(),
	"counties metrics current":      cursorPaginated(),
	"counties metrics monthly":      cursorPaginated(),
	"jurisdictions metrics current": cursorPaginated(),
	"jurisdictions metrics monthly": cursorPaginated(),
}

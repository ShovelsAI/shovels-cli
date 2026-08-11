//go:build e2e

package cmd

// The fixture commands are compiled only under this tag, so the invocations
// that exercise them are registered only under it too. Both take the endpoint
// path as their single argument and send no query of their own.
func init() {
	transportOnlyInvocations["_test-http"] = transportOnlyInvocation{
		args: []string{"_test-http", "/usage"},
		path: "/usage",
	}
	transportOnlyInvocations["_test-single"] = transportOnlyInvocation{
		args: []string{"_test-single", "/usage"},
		path: "/usage",
	}
}

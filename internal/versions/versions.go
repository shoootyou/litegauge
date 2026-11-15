// Package versions holds the build version. The release build sets Version with
// -ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=<v>".
package versions

// Version is the raw build version; it goes on the wire as is (User-Agent).
var Version = "dev"

// String is the version as shown to people: a leading "v" is added once, to a version that starts with a digit.
func String() string {
	if Version == "" || Version == "dev" {
		return "dev"
	}
	if Version[0] >= '0' && Version[0] <= '9' {
		return "v" + Version
	}
	return Version
}

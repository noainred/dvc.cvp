// Package version exposes the build version of dvc.cvp. The values are
// overridable at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/noainred/dvc.cvp/internal/version.Version=v0.2.0 \
//	  -X github.com/noainred/dvc.cvp/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/noainred/dvc.cvp/internal/version.BuildTime=$(date -u +%FT%TZ)"
package version

// Build-time overridable metadata.
var (
	Version   = "v0.1.0"
	Commit    = "dev"
	BuildTime = "unknown"
)

// Info is the version metadata returned by the API and shown in the portal.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// Get returns the current build info.
func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime}
}

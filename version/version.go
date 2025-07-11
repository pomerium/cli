// Package version enables setting build-time version using ldflags.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	// ProjectName is the canonical project name.
	ProjectName = "pomerium-cli"
	// ProjectURL is the canonical project url.
	ProjectURL = "https://www.pomerium.com"
	// Version specifies Semantic versioning increment (MAJOR.MINOR.PATCH).
	Version = "v0.0.0"
	// GitCommit specifies the git commit sha, set by the compiler.
	GitCommit = ""
	// BuildMeta specifies release type (dev,rc1,beta,etc)
	BuildMeta = ""

	// Features contains a list of supported features.
	Features []string

	runtimeVersion = runtime.Version()
)

// FullVersion returns a version string.
func FullVersion() string {
	var sb strings.Builder
	sb.Grow(len(Version) + len(GitCommit) + len(BuildMeta) + len("-") + len("+"))
	sb.WriteString(Version)
	if BuildMeta != "" {
		sb.WriteString("-" + BuildMeta)
	}
	if GitCommit != "" {
		sb.WriteString("+" + GitCommit)
	}
	if len(Features) > 0 {
		sb.WriteString("\nFeatures:")
		for _, f := range Features {
			sb.WriteRune(' ')
			sb.WriteString(f)
		}
	}
	return sb.String()
}

// UserAgent returns a user-agent string as specified in RFC 2616:14.43
// https://tools.ietf.org/html/rfc2616
func UserAgent() string {
	return fmt.Sprintf("%s/%s (+%s; %s; %s)", ProjectName, Version, ProjectURL, GitCommit, runtimeVersion)
}

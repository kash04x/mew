// Package version carries the build version, overridable at link time:
//
//	go build -ldflags "-X mew/internal/version.Version=v1.2.3"
package version

// Version identifies this build in logs and the HTTP User-Agent.
var Version = "0.1.0-dev"

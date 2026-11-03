// Package buildinfo holds facts stamped into the binary at build time.
package buildinfo

// Version is the release the binary was built from, set by the Dockerfile with
// -ldflags "-X .../buildinfo.Version=x.y.z". Local builds report "dev".
var Version = "dev"

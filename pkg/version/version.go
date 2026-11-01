// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "unknown"
)

// Info returns formatted compile-time build version and metadata.
func Info() string {
	return Version + " (commit: " + Commit + ", date: " + Date + ", built by: " + BuiltBy + ")"
}

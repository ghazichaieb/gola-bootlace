// Package version holds the program's name, version and the attribution line required by ADDITIONAL-TERMS.md.
package version

// Version is the release number. A release build can override it with -ldflags "-X .../internal/version.Version=1.2.3".
var Version = "0.0.0-dev"

// String is what `bootlace --version` prints.
func String() string {
	return "Gola Bootlace " + Version + "\nGola Bootlace, by Gola Services. Copyright (c) 2026 Gola Services SUARL. GPL-3.0-or-later."
}

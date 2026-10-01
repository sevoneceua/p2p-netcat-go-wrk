//go:build !windows

package app

import "io"

// pauseIfLaunchedFromExplorer is a Windows-only concern (Explorer
// double-click); everywhere else, a terminal invoked this process and
// its output is already visible after the process exits, so there is
// nothing to wait for.
func pauseIfLaunchedFromExplorer(io.Writer) {}

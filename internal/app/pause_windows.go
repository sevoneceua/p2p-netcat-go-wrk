//go:build windows

package app

import (
	"fmt"
	"io"
	"time"

	"github.com/inconshreveable/mousetrap"
)

// pauseIfLaunchedFromExplorer keeps the console window open for a few
// seconds after an error in the auto-config path (see autoconfig.go) when
// the process was started by double-clicking in Explorer -- otherwise
// the window closes before the error is even readable, the exact problem
// cobra's own mousetrap splash exists to prevent. Not needed on the
// success path: the daemon blocks on ctx.Done() until the window is
// closed or Ctrl+C is pressed, so there's nothing to rush past.
func pauseIfLaunchedFromExplorer(stdout io.Writer) {
	if !mousetrap.StartedByExplorer() {
		return
	}
	fmt.Fprintln(stdout, "[p2p-nc] (this window will close in a few seconds)")
	time.Sleep(5 * time.Second)
}

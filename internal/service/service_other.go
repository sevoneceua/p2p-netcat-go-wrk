//go:build !windows && !linux

package service

import "fmt"

var errUnsupported = fmt.Errorf("service install/start/stop is not implemented on this OS yet (Windows and Linux/systemd are supported)")

func Install(string) error        { return errUnsupported }
func Uninstall() error            { return errUnsupported }
func Start() error                { return errUnsupported }
func Stop() error                 { return errUnsupported }
func StatusText() (string, error) { return "", errUnsupported }
func RunAsService(string) error   { return errUnsupported }

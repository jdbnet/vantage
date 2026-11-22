//go:build linux && !gtk3

package main

func enableWebkitPopouts() {}

func (d *Desktop) PopoutSupported() bool {
	return false
}

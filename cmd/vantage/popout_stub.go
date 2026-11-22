//go:build !linux && !darwin

package main

func enableWebkitPopouts() {}

func (d *Desktop) PopoutSupported() bool {
	return false
}

//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1
#include "popout_linux.h"
*/
import "C"

func enableWebkitPopouts() {
	C.vantage_enable_webkit_popouts()
}

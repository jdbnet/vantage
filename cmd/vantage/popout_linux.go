//go:build linux && gtk3

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include "popout_linux.h"
*/
import "C"

func enableWebkitPopouts() {
	C.vantage_enable_webkit_popouts()
}

//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework Foundation
#include "popout_darwin.h"
*/
import "C"

func enableWebkitPopouts() {
	C.vantage_enable_webkit_popouts()
}

//go:build !linux

package guacx

// DetectServerLayout has no session keymap source on this OS.
// guacd then keeps its default, en-us-qwerty.
func DetectServerLayout() string {
	return ""
}

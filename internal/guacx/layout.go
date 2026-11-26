package guacx

import "strings"

// ServerLayout maps an XKB layout and variant to a guacd RDP keymap name.
// An empty result means guacd should keep its default, en-us-qwerty.
func ServerLayout(layout, variant string) string {
	layout = strings.ToLower(strings.TrimSpace(layout))
	variant = strings.ToLower(strings.TrimSpace(variant))
	if i := strings.IndexAny(layout, ",:"); i >= 0 {
		layout = layout[:i]
	}
	if i := strings.IndexAny(variant, ",:"); i >= 0 {
		variant = variant[:i]
	}
	switch layout {
	case "us":
		return "en-us-qwerty"
	case "gb", "uk":
		return "en-gb-qwerty"
	case "de":
		if variant == "ch" {
			return "de-ch-qwertz"
		}
		return "de-de-qwertz"
	case "ch":
		if variant == "fr" {
			return "fr-ch-qwertz"
		}
		return "de-ch-qwertz"
	case "fr":
		if variant == "be" {
			return "fr-be-azerty"
		}
		if variant == "ch" {
			return "fr-ch-qwertz"
		}
		return "fr-fr-azerty"
	case "be":
		return "fr-be-azerty"
	case "es":
		if variant == "latam" {
			return "es-latam-qwerty"
		}
		return "es-es-qwerty"
	case "latam":
		return "es-latam-qwerty"
	case "it":
		return "it-it-qwerty"
	case "se", "sv":
		return "sv-se-qwerty"
	case "dk", "da":
		return "da-dk-qwerty"
	case "hu":
		return "hu-hu-qwertz"
	case "pt":
		if variant == "br" {
			return "pt-br-qwerty"
		}
		return ""
	case "br":
		return "pt-br-qwerty"
	case "tr":
		return "tr-tr-qwerty"
	case "jp":
		return "ja-jp-qwerty"
	default:
		return ""
	}
}

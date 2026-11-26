package guacx

import "testing"

func TestServerLayout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		layout, variant, want string
	}{
		{"gb", "", "en-gb-qwerty"},
		{"us", "intl", "en-us-qwerty"},
		{"de", "", "de-de-qwertz"},
		{"ch", "fr", "fr-ch-qwertz"},
		{"fr", "", "fr-fr-azerty"},
		{"pt", "br", "pt-br-qwerty"},
		{"es", "latam", "es-latam-qwerty"},
		{"GB,us", "", "en-gb-qwerty"},
		{"dvorak", "", ""},
	}
	for _, tc := range cases {
		got := ServerLayout(tc.layout, tc.variant)
		if got != tc.want {
			t.Fatalf("ServerLayout(%q, %q)=%q want %q", tc.layout, tc.variant, got, tc.want)
		}
	}
}

func TestBuildConfigServerLayout(t *testing.T) {
	t.Parallel()
	cfg := buildConfig(Params{Protocol: "rdp", Hostname: "pc.example", Port: 3389, ServerLayout: "en-gb-qwerty"})
	if cfg.Parameters["server-layout"] != "en-gb-qwerty" {
		t.Fatalf("server-layout=%q", cfg.Parameters["server-layout"])
	}
	blank := buildConfig(Params{Protocol: "rdp", Hostname: "pc.example", Port: 3389})
	if _, ok := blank.Parameters["server-layout"]; ok {
		t.Fatal("empty layout should be omitted")
	}
}

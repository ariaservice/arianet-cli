package printer

import "testing"

func TestCleanStripsTerminalControls(t *testing.T) {
	cases := map[string]string{
		"plain":                 "plain",
		"سرور ۱":                "سرور ۱",
		"a\tb\nc":               "a\tb\nc",
		"web\x1b[2J\x1b[Hroot":  "web[2J[Hroot",
		"bell\x07back\x08space": "bellbackspace",
		"c1\u009b31mred":        "c131mred",
		"carriage\rreturn":      "carriagereturn",
		"\x1b]0;pwned\x07title": "]0;pwnedtitle",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanLineKeepsOneLineAndDropsBidi(t *testing.T) {
	cases := map[string]string{
		"a\tb\nc":                 "a b c",
		"name\u202eevil\u202c":    "nameevil",
		"x\u2066y\u2069z\u2028w": "xyzw",
	}
	for in, want := range cases {
		if got := CleanLine(in); got != want {
			t.Errorf("CleanLine(%q) = %q, want %q", in, got, want)
		}
	}
}

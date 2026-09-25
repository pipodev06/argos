package collector

import "testing"

func TestLinuxElevated(t *testing.T) {
	t.Parallel()
	mk := func(eff string) string { return "Name:\targos\nCapEff:\t" + eff + "\nCapBnd:\t0\n" }
	tests := []struct {
		name         string
		euid         int
		status       string
		wantElevated bool
		wantOK       bool
	}{
		{"root", 0, "", true, true},
		{"ptrace efectiva", 999, mk("0000000000080000"), true, true},
		{"ptrace con dac_read_search", 999, mk("0000000000080004"), true, true},
		{"solo dac_read_search", 999, mk("0000000000000004"), false, true},
		{"sin capacidades", 999, mk("0000000000000000"), false, true},
		{"sin linea CapEff", 999, "Name:\tx\n", false, false},
		{"CapEff ilegible", 999, mk("zz"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotE, gotOK := linuxElevated(tt.euid, tt.status)
			if gotE != tt.wantElevated || gotOK != tt.wantOK {
				t.Errorf("got (%v, %v), want (%v, %v)", gotE, gotOK, tt.wantElevated, tt.wantOK)
			}
		})
	}
}

func TestPrivilegeLevel(t *testing.T) {
	t.Parallel()
	if privilegeLevel(true) != "elevated" || privilegeLevel(false) != "user" {
		t.Error("valores del protocolo: elevated | user")
	}
}

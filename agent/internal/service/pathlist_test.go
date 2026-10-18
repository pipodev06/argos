package service

import "testing"

func TestAddPathEntry(t *testing.T) {
	t.Parallel()
	const dir = `C:\Program Files\argos-agent`
	tests := []struct {
		name, in, want string
		changed        bool
	}{
		{"vacio", "", dir, true},
		{"agrega al final", `C:\a;C:\b`, `C:\a;C:\b;` + dir, true},
		{"termina en separador", `C:\a;`, `C:\a;` + dir, true},
		{"ya esta", `C:\a;` + dir, `C:\a;` + dir, false},
		{"mayusculas y barra final", `C:\a;c:\program files\ARGOS-AGENT\`, `C:\a;c:\program files\ARGOS-AGENT\`, false},
		{"prefijo no es igual", `C:\Program Files\argos-agent-x`, `C:\Program Files\argos-agent-x;` + dir, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := addPathEntry(tc.in, dir)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("addPathEntry(%q) = %q, %v; want %q, %v", tc.in, got, changed, tc.want, tc.changed)
			}
		})
	}
}

func TestRemovePathEntry(t *testing.T) {
	t.Parallel()
	const dir = `C:\Program Files\argos-agent`
	tests := []struct {
		name, in, want string
		changed        bool
	}{
		{"no esta", `C:\a;C:\b`, `C:\a;C:\b`, false},
		{"en medio", `C:\a;` + dir + `;C:\b`, `C:\a;C:\b`, true},
		{"unica", dir, "", true},
		{"mayusculas y barra final", `C:\a;C:\PROGRAM FILES\argos-agent\`, `C:\a`, true},
		{"duplicada", dir + `;C:\a;` + dir, `C:\a`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := removePathEntry(tc.in, dir)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("removePathEntry(%q) = %q, %v; want %q, %v", tc.in, got, changed, tc.want, tc.changed)
			}
		})
	}
}

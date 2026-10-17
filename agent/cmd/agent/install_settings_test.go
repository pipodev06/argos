package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"argos/agent/internal/config"
)

func readSettings(t *testing.T, h *harness) config.Settings {
	t.Helper()
	s, err := config.LoadSettings(h.svc.stateDir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	return s
}

func TestInstallSavesSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	err := h.app.execute(context.Background(), []string{"install", "--inventory", "all", "--probe-allow", "192.168.1.0/25,10.0.0.9", "--insecure"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	got := readSettings(t, h)
	want := config.Settings{Inventory: "ports,services,processes,updates", ProbeAllow: "192.168.1.0/25,10.0.0.9", Insecure: true}
	if got != want {
		t.Errorf("settings got %+v, want %+v", got, want)
	}
	if !strings.Contains(h.stderr.String(), "SIN CIFRAR") {
		t.Errorf("--insecure debe avisar en stderr: %q", h.stderr)
	}
	if !slices.Contains(h.svc.calls, "install") {
		t.Errorf("debe instalar el servicio: %v", h.svc.calls)
	}
}

func TestInstallRejectsBadSettingsBeforeTouchingSystem(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--probe-allow", "0.0.0.0/0"},
		{"--probe-allow", "::/0"},
		{"--probe-allow", "10.0.0.0/8"},
		{"--probe-allow", "10.0.0.1;reboot"},
		{"--inventory", "ports,foo"},
	} {
		h := newHarness(t)
		err := h.app.execute(context.Background(), append([]string{"install"}, args...))
		if err == nil {
			t.Errorf("install %v debe fallar", args)
		}
		if len(h.svc.calls) != 0 {
			t.Errorf("install %v no debe tocar el servicio: %v", args, h.svc.calls)
		}
		if _, statErr := os.Stat(filepath.Join(h.svc.stateDir, config.SettingsFileName)); statErr == nil {
			t.Errorf("install %v no debe escribir settings.json", args)
		}
	}
}

func TestReinstallMergesSettingsWithoutReinstallingService(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	if err := h.app.execute(ctx, []string{"install", "--inventory", "ports", "--probe-allow", "192.168.1.0/25"}); err != nil {
		t.Fatal(err)
	}
	h.svc.installed = true
	h.svc.calls = nil

	if err := h.app.execute(ctx, []string{"install", "--inventory", "services"}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if got, want := readSettings(t, h), (config.Settings{Inventory: "services", ProbeAllow: "192.168.1.0/25"}); got != want {
		t.Errorf("settings got %+v, want %+v (solo cambia lo indicado)", got, want)
	}
	if len(h.svc.calls) != 0 {
		t.Errorf("con el servicio instalado solo se actualiza la config: %v", h.svc.calls)
	}
	// Vaciar explicitamente un campo.
	if err := h.app.execute(ctx, []string{"install", "--probe-allow", ""}); err != nil {
		t.Fatal(err)
	}
	if got := readSettings(t, h); got.ProbeAllow != "" || got.Inventory != "services" {
		t.Errorf("settings got %+v, want probe_allow vacio e inventario intacto", got)
	}
	// Cambiar la definicion del servicio exige desinstalar (conservando el estado).
	if err := h.app.execute(ctx, []string{"install", "--inventory", "ports", "--privileged"}); err == nil || !strings.Contains(err.Error(), "uninstall --keep-state") {
		t.Errorf("--privileged con servicio instalado: err got %v", err)
	}
}

func TestInstallWithoutSettingsFlagsLeavesSettingsUntouched(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := h.app.execute(context.Background(), []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.svc.stateDir, config.SettingsFileName)); err == nil {
		t.Error("sin flags de config no se debe crear settings.json")
	}
}

func TestStatusShowsEffectiveSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := h.app.execute(context.Background(), []string{"install", "--inventory", "ports,updates", "--probe-allow", "192.168.1.0/25"}); err != nil {
		t.Fatal(err)
	}
	h.stdout.Reset()
	if err := h.app.execute(context.Background(), []string{"status"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ports,updates", "192.168.1.0/25", "insecure: false"} {
		if !strings.Contains(h.stdout.String(), want) {
			t.Errorf("status debe mostrar %q:\n%s", want, h.stdout)
		}
	}
	h.assertNoSecrets(t)
}

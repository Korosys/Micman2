//go:build windows
// +build windows

package main

import "testing"

func TestVoicemeeterStripMuteParam(t *testing.T) {
	if got := voicemeeterStripMuteParam(0); got != "Strip[0].Mute" {
		t.Fatalf("voicemeeterStripMuteParam(0) = %q", got)
	}
	if got := voicemeeterStripMuteParam(3); got != "Strip[3].Mute" {
		t.Fatalf("voicemeeterStripMuteParam(3) = %q", got)
	}
}

func TestVoicemeeterSetMuteScript(t *testing.T) {
	mutedScript, err := voicemeeterSetMuteScript(" Strip[0].Mute ", true)
	if err != nil {
		t.Fatalf("voicemeeterSetMuteScript returned error: %v", err)
	}
	if mutedScript != "Strip[0].Mute=1;" {
		t.Fatalf("muted script = %q", mutedScript)
	}

	unmutedScript, err := voicemeeterSetMuteScript("Strip[0].Mute", false)
	if err != nil {
		t.Fatalf("voicemeeterSetMuteScript returned error: %v", err)
	}
	if unmutedScript != "Strip[0].Mute=0;" {
		t.Fatalf("unmuted script = %q", unmutedScript)
	}

	if _, err := voicemeeterSetMuteScript("Strip[0].Mute=1;", true); err == nil {
		t.Fatal("expected invalid parameter name to be rejected")
	}
}

func TestCurrentVoicemeeterMutedSmoke(t *testing.T) {
	_, err := currentVoicemeeterMuted(voicemeeterStripMuteParam(defaultVoicemeeterStrip))
	if err != nil {
		t.Skipf("could not read Voicemeeter strip mute state: %v", err)
	}
}

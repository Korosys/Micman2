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

func TestCurrentVoicemeeterMutedSmoke(t *testing.T) {
	_, err := currentVoicemeeterMuted(voicemeeterStripMuteParam(defaultVoicemeeterStrip))
	if err != nil {
		t.Skipf("could not read Voicemeeter strip mute state: %v", err)
	}
}

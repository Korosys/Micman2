//go:build !windows
// +build !windows

package main

import "fmt"

const defaultVoicemeeterStrip = 0

func currentVoicemeeterMuted(paramName string) (bool, error) {
	return false, fmt.Errorf("Voicemeeter Remote API is only available on Windows")
}

func toggleVoicemeeterMuted(paramName string) (bool, error) {
	return false, fmt.Errorf("Voicemeeter Remote API is only available on Windows")
}

func voicemeeterStripMuteParam(strip int) string {
	return fmt.Sprintf("Strip[%d].Mute", strip)
}

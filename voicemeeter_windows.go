//go:build windows
// +build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const defaultVoicemeeterStrip = 0

// errVoicemeeterNotRunning means we logged in to the Voicemeeter Remote service
// but the Voicemeeter application/engine itself is not running yet, so there are
// no real parameters to read. Callers should treat this as "state unknown, retry"
// rather than as a definitive unmuted reading.
var errVoicemeeterNotRunning = fmt.Errorf("Voicemeeter application is not running")

type voicemeeterRemote struct {
	login             *syscall.LazyProc
	logout            *syscall.LazyProc
	isParametersDirty *syscall.LazyProc
	getParameterFloat *syscall.LazyProc
	setParameters     *syscall.LazyProc
}

// connect logs in to the Voicemeeter Remote API. VBVMR_Login returns 0 when the
// engine is running, a negative code on failure, and 1 when login itself
// succeeded but the Voicemeeter application is not launched. The 1 case is the
// reason the tray could come up showing "unmuted" while actually muted: when we
// start before Voicemeeter at boot the engine is not ready, an immediate read
// yields nothing meaningful, and treating 1 as success silently produced a false
// unmuted state. Surface it as errVoicemeeterNotRunning so callers fall back and
// keep retrying until the engine is up.
func (remote *voicemeeterRemote) connect() error {
	result := callVoicemeeter(remote.login)
	err := voicemeeterLoginError(result)
	if err != nil {
		// A non-negative result means the client registered with the Remote
		// service even though the engine is not running yet (result == 1), so
		// pair that login with a logout before bailing out instead of leaking it
		// on every retry. A negative result means login itself failed and there
		// is nothing to release.
		if result >= 0 {
			callVoicemeeter(remote.logout)
		}
		return err
	}
	return nil
}

// voicemeeterLoginError maps a VBVMR_Login return code to an error: 0 means the
// engine is running and parameters are readable; a negative code is a hard
// failure; 1 means login succeeded but the Voicemeeter application is not
// launched yet (treated as not-ready so callers retry instead of reading junk).
func voicemeeterLoginError(result int32) error {
	if result < 0 {
		return voicemeeterError("VBVMR_Login", result)
	}
	if result == 1 {
		return errVoicemeeterNotRunning
	}
	return nil
}

func currentVoicemeeterMuted(paramName string) (bool, error) {
	paramName, err := normalizeVoicemeeterParamName(paramName)
	if err != nil {
		return false, err
	}

	remote, err := newVoicemeeterRemote()
	if err != nil {
		return false, err
	}

	if err := remote.connect(); err != nil {
		return false, err
	}
	defer callVoicemeeter(remote.logout)

	return remote.syncedParameterMuted(paramName)
}

func toggleVoicemeeterMuted(paramName string) (bool, error) {
	paramName, err := normalizeVoicemeeterParamName(paramName)
	if err != nil {
		return false, err
	}

	remote, err := newVoicemeeterRemote()
	if err != nil {
		return false, err
	}

	if err := remote.connect(); err != nil {
		return false, err
	}
	defer callVoicemeeter(remote.logout)

	isMuted, err := remote.syncedParameterMuted(paramName)
	if err != nil {
		return false, err
	}

	nextMuted := !isMuted
	if err := remote.setParameterMuted(paramName, nextMuted); err != nil {
		return false, err
	}

	verifiedMuted, err := remote.waitForParameterMuted(paramName, nextMuted)
	if err != nil {
		return false, err
	}
	if verifiedMuted != nextMuted {
		return false, fmt.Errorf("Voicemeeter reported %s=%t after setting it to %t", paramName, verifiedMuted, nextMuted)
	}

	return verifiedMuted, nil
}

// voicemeeterSyncAttempts/Interval bound how long syncedParameterMuted waits for
// the engine to deliver the parameter snapshot to a freshly logged-in client.
// The snapshot was observed to arrive within ~100ms, so 40*25ms is a generous cap.
const (
	voicemeeterSyncAttempts = 40
	voicemeeterSyncInterval = 25 * time.Millisecond
)

// syncedParameterMuted reads the mute parameter after the engine has delivered
// the current parameter snapshot to this freshly logged-in client. Right after
// VBVMR_Login the client's cache is cold and VBVMR_GetParameterFloat returns a
// stale 0 until the engine pushes the real values, which it signals by the next
// VBVMR_IsParametersDirty returning 1 (observed ~100ms later). Reading before
// that snapshot is what made the tray show "unmuted" for a strip muted from the
// Voicemeeter UI; a mute set through the Remote API happens to warm the cache,
// which is why hotkey-set mutes read correctly while GUI-set mutes did not.
// Wait for that first dirty signal, then read; fall back to a best-effort read
// if it never arrives.
func (remote *voicemeeterRemote) syncedParameterMuted(paramName string) (bool, error) {
	for attempt := 0; attempt < voicemeeterSyncAttempts; attempt++ {
		if callVoicemeeter(remote.isParametersDirty) == 1 {
			return remote.parameterMuted(paramName)
		}
		time.Sleep(voicemeeterSyncInterval)
	}
	return remote.parameterMuted(paramName)
}

func (remote *voicemeeterRemote) parameterMuted(paramName string) (bool, error) {
	namePtr, err := syscall.BytePtrFromString(paramName)
	if err != nil {
		return false, err
	}

	var value float32
	result := callVoicemeeter(
		remote.getParameterFloat,
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&value)),
	)
	if result < 0 {
		return false, voicemeeterError("VBVMR_GetParameterFloat", result)
	}

	return value >= 0.5, nil
}

func (remote *voicemeeterRemote) setParameterMuted(paramName string, isMuted bool) error {
	script, err := voicemeeterSetMuteScript(paramName, isMuted)
	if err != nil {
		return err
	}
	scriptPtr, err := syscall.BytePtrFromString(script)
	if err != nil {
		return err
	}

	result := callVoicemeeter(remote.setParameters, uintptr(unsafe.Pointer(scriptPtr)))
	if result != 0 {
		return voicemeeterError("VBVMR_SetParameters", result)
	}
	return nil
}

func (remote *voicemeeterRemote) waitForParameterMuted(paramName string, wantMuted bool) (bool, error) {
	var lastMuted bool
	for attempt := 0; attempt < 6; attempt++ {
		callVoicemeeter(remote.isParametersDirty)

		isMuted, err := remote.parameterMuted(paramName)
		if err != nil {
			return false, err
		}
		lastMuted = isMuted
		if isMuted == wantMuted {
			return isMuted, nil
		}

		time.Sleep(25 * time.Millisecond)
	}

	return lastMuted, nil
}

func newVoicemeeterRemote() (*voicemeeterRemote, error) {
	dllPath, err := voicemeeterRemoteDLLPath()
	if err != nil {
		return nil, err
	}

	dll := syscall.NewLazyDLL(dllPath)
	remote := &voicemeeterRemote{
		login:             dll.NewProc("VBVMR_Login"),
		logout:            dll.NewProc("VBVMR_Logout"),
		isParametersDirty: dll.NewProc("VBVMR_IsParametersDirty"),
		getParameterFloat: dll.NewProc("VBVMR_GetParameterFloat"),
		setParameters:     dll.NewProc("VBVMR_SetParameters"),
	}
	if err := remote.login.Find(); err != nil {
		return nil, err
	}
	if err := remote.logout.Find(); err != nil {
		return nil, err
	}
	if err := remote.isParametersDirty.Find(); err != nil {
		return nil, err
	}
	if err := remote.getParameterFloat.Find(); err != nil {
		return nil, err
	}
	if err := remote.setParameters.Find(); err != nil {
		return nil, err
	}
	return remote, nil
}

func voicemeeterStripMuteParam(strip int) string {
	return "Strip[" + strconv.Itoa(strip) + "].Mute"
}

func normalizeVoicemeeterParamName(paramName string) (string, error) {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		paramName = voicemeeterStripMuteParam(defaultVoicemeeterStrip)
	}
	if strings.ContainsAny(paramName, "=;\r\n") {
		return "", fmt.Errorf("invalid Voicemeeter parameter name %q", paramName)
	}
	return paramName, nil
}

func voicemeeterSetMuteScript(paramName string, isMuted bool) (string, error) {
	paramName, err := normalizeVoicemeeterParamName(paramName)
	if err != nil {
		return "", err
	}

	value := "0"
	if isMuted {
		value = "1"
	}
	return paramName + "=" + value + ";", nil
}

func voicemeeterRemoteDLLPath() (string, error) {
	dllName := "VoicemeeterRemote.dll"
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		dllName = "VoicemeeterRemote64.dll"
	}

	candidates := []string{
		filepath.Join(".", dllName),
	}

	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		candidates = append(candidates, filepath.Join(programFilesX86, "VB", "Voicemeeter", dllName))
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "VB", "Voicemeeter", dllName))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%s not found; install Voicemeeter or copy it next to micman2.exe", dllName)
}

func callVoicemeeter(proc *syscall.LazyProc, args ...uintptr) int32 {
	result, _, _ := proc.Call(args...)
	return int32(uint32(result))
}

func voicemeeterError(function string, code int32) error {
	return fmt.Errorf("%s failed with Voicemeeter Remote API code %d", function, code)
}

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
	"unsafe"
)

const defaultVoicemeeterStrip = 0

type voicemeeterRemote struct {
	login             *syscall.LazyProc
	logout            *syscall.LazyProc
	isParametersDirty *syscall.LazyProc
	getParameterFloat *syscall.LazyProc
}

func currentVoicemeeterMuted(paramName string) (bool, error) {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		paramName = voicemeeterStripMuteParam(defaultVoicemeeterStrip)
	}

	remote, err := newVoicemeeterRemote()
	if err != nil {
		return false, err
	}

	result := callVoicemeeter(remote.login)
	if result < 0 {
		return false, voicemeeterError("VBVMR_Login", result)
	}
	defer callVoicemeeter(remote.logout)

	// Voicemeeter's API docs recommend polling this before reading parameters.
	// We only need one startup snapshot, so the result is not important here.
	callVoicemeeter(remote.isParametersDirty)

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
	return remote, nil
}

func voicemeeterStripMuteParam(strip int) string {
	return "Strip[" + strconv.Itoa(strip) + "].Mute"
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

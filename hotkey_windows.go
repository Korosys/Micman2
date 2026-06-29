//go:build windows
// +build windows

package main

import (
	"log"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	micmanHotkeyID = 1

	modControl = 0x0002
	vkSpace    = 0x20
	wmHotkey   = 0x0312
)

var (
	hotkeyUser32 = syscall.NewLazyDLL("user32.dll")

	procRegisterHotKey   = hotkeyUser32.NewProc("RegisterHotKey")
	procUnregisterHotKey = hotkeyUser32.NewProc("UnregisterHotKey")
	hotkeyGetMessageW    = hotkeyUser32.NewProc("GetMessageW")
)

type hotkeyPoint struct {
	X int32
	Y int32
}

type hotkeyMsg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      hotkeyPoint
}

func startGlobalHotkeyListener() {
	go runGlobalHotkeyListener(func() {
		isMuted, err := toggleVoicemeeterMuted(vmStateSource)
		if err != nil {
			log.Printf("Ctrl+Space mute toggle failed: %v", err)
			return
		}

		updateSystrayForMutedMode(isMuted)
	})
}

func runGlobalHotkeyListener(onHotkey func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	registered, _, err := procRegisterHotKey.Call(0, micmanHotkeyID, modControl, vkSpace)
	if registered == 0 {
		log.Printf("Could not register Ctrl+Space hotkey: %v", err)
		return
	}
	defer procUnregisterHotKey.Call(0, micmanHotkeyID)

	var message hotkeyMsg
	for {
		result, _, _ := hotkeyGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		if message.Message == wmHotkey && message.WParam == micmanHotkeyID {
			onHotkey()
		}
	}
}

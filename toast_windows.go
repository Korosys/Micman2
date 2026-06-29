//go:build windows
// +build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	toastWindowClassName = "Micman2ToastWindow"
	toastWindowWidth     = int32(360)
	toastWindowHeight    = int32(84)
	toastWindowMargin    = int32(24)
	toastDuration        = 1800 * time.Millisecond

	wsPopup = 0x80000000

	wsExLayered    = 0x00080000
	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008

	lwaAlpha = 0x00000002

	swShowNoActivate = 4

	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	spiGetWorkArea = 0x0030

	smCxScreen = 0
	smCyScreen = 1

	idcArrow = 32512

	wmClose     = 0x0010
	wmDestroy   = 0x0002
	wmPaint     = 0x000F
	wmSetCursor = 0x0020
	wmTimer     = 0x0113

	dtCenter     = 0x00000001
	dtSingleLine = 0x00000020
	dtVCenter    = 0x00000004

	transparent = 1
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procBeginPaint                 = user32.NewProc("BeginPaint")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procEndPaint                   = user32.NewProc("EndPaint")
	procFillRect                   = user32.NewProc("FillRect")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procSetTimer                   = user32.NewProc("SetTimer")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSystemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procUpdateWindow               = user32.NewProc("UpdateWindow")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetCursor        = user32.NewProc("SetCursor")
	procSetTextColor     = gdi32.NewProc("SetTextColor")

	toastWindowProcCallback = syscall.NewCallback(toastWindowProc)

	activeToastMu   sync.Mutex
	activeToastHwnd syscall.Handle
	toastCreateMu   sync.Mutex
	toastWindows    sync.Map
)

type toastWindowData struct {
	text       string
	background uint32
}

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type msg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type paintStruct struct {
	Hdc         syscall.Handle
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

func showMicStateToast(isMuted bool) {
	background := rgb(32, 120, 74)
	if isMuted {
		background = rgb(154, 35, 35)
	}

	go runMicStateToast(toastWindowData{
		text:       micStateToastText(isMuted),
		background: background,
	})
}

func runMicStateToast(data toastWindowData) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, err := syscall.UTF16PtrFromString(toastWindowClassName)
	if err != nil {
		return
	}
	title, err := syscall.UTF16PtrFromString(data.text)
	if err != nil {
		return
	}

	toastCreateMu.Lock()
	closeActiveToast()

	hInstanceRaw, _, _ := procGetModuleHandleW.Call(0)
	hInstance := syscall.Handle(hInstanceRaw)
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   toastWindowProcCallback,
		Instance:  hInstance,
		Cursor:    loadArrowCursor(),
		ClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))

	workArea := currentWorkArea()
	workAreaWidth := workArea.Right - workArea.Left
	x := workArea.Left + (workAreaWidth-toastWindowWidth)/2
	y := workArea.Bottom - toastWindowHeight - toastWindowMargin
	if x < workArea.Left {
		x = workArea.Left
	}
	if y < workArea.Top {
		y = workArea.Top
	}

	hwndRaw, _, _ := procCreateWindowExW.Call(
		wsExTopmost|wsExToolWindow|wsExLayered|wsExNoActivate,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		int32Param(x),
		int32Param(y),
		int32Param(toastWindowWidth),
		int32Param(toastWindowHeight),
		0,
		0,
		uintptr(hInstance),
		0,
	)
	if hwndRaw == 0 {
		toastCreateMu.Unlock()
		return
	}
	hwnd := syscall.Handle(hwndRaw)
	toastWindows.Store(hwnd, data)
	setActiveToast(hwnd)
	toastCreateMu.Unlock()
	defer clearActiveToast(hwnd)

	procSetLayeredWindowAttributes.Call(hwndRaw, 0, 235, lwaAlpha)
	procSetWindowPos.Call(hwndRaw, ^uintptr(0), int32Param(x), int32Param(y), int32Param(toastWindowWidth), int32Param(toastWindowHeight), swpNoActivate|swpShowWindow)
	procShowWindow.Call(hwndRaw, swShowNoActivate)
	procUpdateWindow.Call(hwndRaw)
	procSetTimer.Call(hwndRaw, 1, uintptr(toastDuration/time.Millisecond), 0)

	var message msg
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func toastWindowProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmPaint:
		paintToastWindow(hwnd)
		return 0
	case wmSetCursor:
		procSetCursor.Call(uintptr(loadArrowCursor()))
		return 1
	case wmTimer, wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		toastWindows.Delete(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}

	result, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return result
}

func paintToastWindow(hwnd syscall.Handle) {
	var ps paintStruct
	hdcRaw, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdcRaw == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var client rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&client)))

	data := toastWindowData{text: "Micman 2", background: rgb(44, 44, 44)}
	if loaded, ok := toastWindows.Load(hwnd); ok {
		data = loaded.(toastWindowData)
	}

	brush, _, _ := procCreateSolidBrush.Call(uintptr(data.background))
	if brush != 0 {
		procFillRect.Call(hdcRaw, uintptr(unsafe.Pointer(&client)), brush)
		procDeleteObject.Call(brush)
	}

	textRect := client
	textRect.Left += 22
	textRect.Right -= 22
	procSetBkMode.Call(hdcRaw, transparent)
	procSetTextColor.Call(hdcRaw, uintptr(rgb(255, 255, 255)))
	text, err := syscall.UTF16PtrFromString(data.text)
	if err != nil {
		return
	}
	procDrawTextW.Call(
		hdcRaw,
		uintptr(unsafe.Pointer(text)),
		^uintptr(0),
		uintptr(unsafe.Pointer(&textRect)),
		dtCenter|dtSingleLine|dtVCenter,
	)
}

func currentWorkArea() rect {
	workArea := rect{}
	result, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&workArea)), 0)
	if result != 0 {
		return workArea
	}

	width, _, _ := procGetSystemMetrics.Call(smCxScreen)
	height, _, _ := procGetSystemMetrics.Call(smCyScreen)
	return rect{Right: int32(width), Bottom: int32(height)}
}

func closeActiveToast() {
	activeToastMu.Lock()
	hwnd := activeToastHwnd
	activeToastMu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
}

func setActiveToast(hwnd syscall.Handle) {
	activeToastMu.Lock()
	activeToastHwnd = hwnd
	activeToastMu.Unlock()
}

func clearActiveToast(hwnd syscall.Handle) {
	activeToastMu.Lock()
	if activeToastHwnd == hwnd {
		activeToastHwnd = 0
	}
	activeToastMu.Unlock()
}

func rgb(r, g, b byte) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

func loadArrowCursor() syscall.Handle {
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	return syscall.Handle(cursor)
}

func int32Param(value int32) uintptr {
	return uintptr(int(value))
}

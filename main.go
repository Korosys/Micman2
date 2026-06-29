package main

import (
	"flag"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"micman2/icon"
	"micman2/icon2"

	"github.com/getlantern/systray"
)

var (
	mutedModeChan = make(chan bool, 1)
	mutedMode     bool
	vmStateSource string
	detectVMState atomic.Bool
)

type trayState struct {
	icon    []byte
	title   string
	tooltip string
}

func currentTrayState(isMuted bool) trayState {
	if isMuted {
		return trayState{
			icon:    icon2.Data,
			title:   "Micman 2 (muted)",
			tooltip: "Micman 2 - microphone muted",
		}
	}

	return trayState{
		icon:    icon.Data,
		title:   "Micman 2",
		tooltip: "Micman 2 - microphone unmuted",
	}
}

func applyTrayState(isMuted bool) {
	state := currentTrayState(isMuted)
	systray.SetTemplateIcon(state.icon, state.icon)
	systray.SetTitle(state.title)
	systray.SetTooltip(state.tooltip)
}

func micStateToastText(isMuted bool) string {
	if isMuted {
		return "Microphone muted"
	}
	return "Microphone unmuted"
}

func applyMicStateChange(isMutedMode bool) {
	stateChanged := mutedMode != isMutedMode
	mutedMode = isMutedMode
	applyTrayState(mutedMode)
	if stateChanged {
		showMicStateToast(mutedMode)
	}
}

func chooseInitialMuted(mutedFlag bool, unmutedFlag bool, detect func() (bool, error)) bool {
	if mutedFlag {
		return true
	}
	if unmutedFlag {
		return false
	}

	isMuted, err := detect()
	if err != nil {
		return false
	}
	return isMuted
}

func main() {
	// Parse flags once at the beginning
	muted := flag.Bool("muted", false, "Run in muted mode")
	unmuted := flag.Bool("unmuted", false, "Disable muted mode")
	vmStrip := flag.Int("vm-strip", defaultVoicemeeterStrip, "Voicemeeter strip index to read for startup mute state")
	vmParam := flag.String("vm-param", "", "Voicemeeter parameter to read instead of a strip mute parameter, for example Strip[0].Mute")
	flag.Parse()

	vmStateSource = voicemeeterStripMuteParam(*vmStrip)
	if *vmParam != "" {
		vmStateSource = *vmParam
	}
	detectVMState.Store(!*muted && !*unmuted)
	mutedMode = chooseInitialMuted(*muted, *unmuted, func() (bool, error) {
		return currentVoicemeeterMuted(vmStateSource)
	})

	// Check for single instance
	if !checkSingleInstance() {
		return
	}

	onExit := func() {
		// now := time.Now()
		// os.WriteFile(fmt.Sprintf(`on_exit_%d.txt`, now.UnixNano()), []byte(now.String()), 0644)
	}

	systray.Run(onReady, onExit)
}

// checkSingleInstance checks if another instance is running and handles flags
func checkSingleInstance() bool {
	// Try to create a listener on a unique port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("Failed to create listener: %v", err)
		return false
	}
	defer listener.Close()

	// Get the actual port assigned
	addr := listener.Addr().String()
	port := addr[strings.LastIndex(addr, ":")+1:]

	// Try to connect to existing instance on a known port
	existingPort := "12345" // Use a fixed port for the main instance
	conn, err := net.Dial("tcp", "127.0.0.1:"+existingPort)
	if err == nil {
		// Another instance is running, send our flags
		defer conn.Close()

		// Send flag information to existing instance
		message := "FLAG:" + port
		if mutedMode {
			message = "FLAG:" + port + ":MUTED"
		} else {
			message = "FLAG:" + port + ":UNMUTED"
		}

		_, err = conn.Write([]byte(message))
		if err != nil {
			log.Printf("Failed to send flags to existing instance: %v", err)
		}

		return false // Exit this instance
	}

	// No existing instance, start our own server and continue
	go startFlagServer(existingPort)
	return true
}

// startFlagServer listens for flag updates from other instances
func startFlagServer(port string) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		log.Printf("Failed to start flag server: %v", err)
		return
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go handleFlagConnection(conn)
	}
}

// handleFlagConnection processes flag updates from other instances
func handleFlagConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	message := string(buf[:n])
	if strings.HasPrefix(message, "FLAG:") {
		parts := strings.Split(message[5:], ":")
		if len(parts) > 1 {
			switch parts[1] {
			case "MUTED":
				// Handle muted flag - update the systray icon or behavior
				updateSystrayForMutedMode(true)
			case "UNMUTED":
				// Handle unmuted flag - update the systray icon or behavior
				updateSystrayForMutedMode(false)
			}
		}
	}
}

// updateSystrayForMutedMode updates the systray to indicate muted mode
func updateSystrayForMutedMode(isMutedMode bool) {
	// A hotkey/explicit flag is authoritative. Do not let startup polling
	// overwrite it while Explorer/Voicemeeter startup settles.
	detectVMState.Store(false)

	// Send signal to the main systray goroutine to update
	select {
	case mutedModeChan <- isMutedMode:
	default:
		// Channel is full, replace the stale state so quick hotkey presses don't
		// leave the tray showing the wrong final state.
		select {
		case <-mutedModeChan:
		default:
		}
		select {
		case mutedModeChan <- isMutedMode:
		default:
		}
	}
}

func onReady() {
	applyTrayState(mutedMode)

	mQuitOrig := systray.AddMenuItem("Quit", "Quit the whole app")
	go func() {
		<-mQuitOrig.ClickedCh
		systray.Quit()
	}()

	// We can manipulate the systray in other goroutines.
	// Task Scheduler can start us before Explorer's notification area is fully
	// ready. Re-applying the same state for a short window makes the icon appear
	// without needing a few hotkey-triggered updates later.
	go func() {
		startupRetry := time.NewTicker(time.Second)
		defer startupRetry.Stop()
		startupRetryC := startupRetry.C
		startupRetryCount := 0

		for {
			select {
			case isMutedMode := <-mutedModeChan:
				applyMicStateChange(isMutedMode)
			case <-startupRetryC:
				previousMutedMode := mutedMode
				if detectVMState.Load() {
					if isMutedMode, err := currentVoicemeeterMuted(vmStateSource); err == nil {
						mutedMode = isMutedMode
					}
				}
				applyTrayState(mutedMode)
				if mutedMode != previousMutedMode {
					showMicStateToast(mutedMode)
				}
				startupRetryCount++
				if startupRetryCount >= 30 {
					startupRetry.Stop()
					startupRetryC = nil
				}
			}
		}
	}()
}

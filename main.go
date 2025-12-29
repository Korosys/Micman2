package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"micman2/icon"
	"micman2/icon2"

	"github.com/getlantern/systray"
)

var (
	testModeChan = make(chan bool, 1)
	testMode     bool
)

func main() {
	// Parse flags once at the beginning
	test := flag.Bool("test", false, "Run in test mode")
	notest := flag.Bool("notest", false, "Disable test mode")
	flag.Parse()

	// Determine test mode based on flags
	if *test {
		testMode = true
	} else if *notest {
		testMode = false
	}
	// If neither flag is provided, keep current state (false by default)

	// Check for single instance
	if !checkSingleInstance() {
		return
	}

	onExit := func() {
		now := time.Now()
		os.WriteFile(fmt.Sprintf(`on_exit_%d.txt`, now.UnixNano()), []byte(now.String()), 0644)
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
		message := fmt.Sprintf("FLAG:%s", port)
		if testMode {
			message = fmt.Sprintf("FLAG:%s:TEST", port)
		} else {
			message = fmt.Sprintf("FLAG:%s:NOTEST", port)
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
			case "TEST":
				// Handle test flag - update the systray icon or behavior
				updateSystrayForTestMode(true)
			case "NOTEST":
				// Handle notest flag - update the systray icon or behavior
				updateSystrayForTestMode(false)
			}
		}
	}
}

// updateSystrayForTestMode updates the systray to indicate test mode
func updateSystrayForTestMode(isTestMode bool) {
	// Send signal to the main systray goroutine to update
	select {
	case testModeChan <- isTestMode:
	default:
		// Channel is full, ignore
	}
}

func onReady() {
	// Check if we started with --test flag
	if testMode {
		fmt.Println("Running in TEST mode!")
		systray.SetTitle("Awesome App (TEST MODE)")
		systray.SetTooltip("Running in TEST mode! 🧪")
	} else {
		systray.SetTemplateIcon(icon.Data, icon.Data)
		systray.SetTitle("Awesome App")
		systray.SetTooltip("Lantern")
	}

	mQuitOrig := systray.AddMenuItem("Quit", "Quit the whole app")
	go func() {
		<-mQuitOrig.ClickedCh
		fmt.Println("Requesting quit")
		systray.Quit()
		fmt.Println("Finished quitting")
	}()

	// We can manipulate the systray in other goroutines
	go func() {
		systray.SetTemplateIcon(icon.Data, icon.Data)
		systray.SetTitle("Micman 2")
		systray.SetTooltip("Mic Indicator")

		for {
			select {
			case isTestMode := <-testModeChan:
				if isTestMode {
					fmt.Println("Switching to TEST mode!")
					systray.SetTemplateIcon(icon2.Data, icon2.Data)
					systray.SetTitle("Awesome App (TEST MODE)")
					systray.SetTooltip("Running in TEST mode! 🧪")
				} else {
					fmt.Println("Switching to NORMAL mode!")
					systray.SetTemplateIcon(icon.Data, icon.Data)
					systray.SetTitle("Awesome App")
					systray.SetTooltip("Lantern")
				}
			default:
				// Keep the goroutine alive but don't block
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

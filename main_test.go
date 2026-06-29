package main

import "testing"

func TestCurrentTrayStateTooltips(t *testing.T) {
	unmuted := currentTrayState(false)
	if unmuted.title != "Micman 2" {
		t.Fatalf("unmuted title = %q, want %q", unmuted.title, "Micman 2")
	}
	if unmuted.tooltip != "Micman 2 - microphone unmuted" {
		t.Fatalf("unmuted tooltip = %q", unmuted.tooltip)
	}
	if unmuted.tooltip == "Lantern" {
		t.Fatal("unmuted tooltip should not use the systray example default")
	}
	if len(unmuted.icon) == 0 {
		t.Fatal("unmuted icon is empty")
	}

	muted := currentTrayState(true)
	if muted.title != "Micman 2 (muted)" {
		t.Fatalf("muted title = %q, want %q", muted.title, "Micman 2 (muted)")
	}
	if muted.tooltip != "Micman 2 - microphone muted" {
		t.Fatalf("muted tooltip = %q", muted.tooltip)
	}
	if len(muted.icon) == 0 {
		t.Fatal("muted icon is empty")
	}
}

func TestMicStateToastText(t *testing.T) {
	if got := micStateToastText(true); got != "Microphone muted" {
		t.Fatalf("micStateToastText(true) = %q", got)
	}
	if got := micStateToastText(false); got != "Microphone unmuted" {
		t.Fatalf("micStateToastText(false) = %q", got)
	}
}

func TestChooseInitialMuted(t *testing.T) {
	detectCalled := false
	detectMuted := func() (bool, error) {
		detectCalled = true
		return true, nil
	}

	if !chooseInitialMuted(false, false, detectMuted) {
		t.Fatal("expected detected muted state to be used when no flag is set")
	}
	if !detectCalled {
		t.Fatal("expected microphone detector to be called when no flag is set")
	}

	detectCalled = false
	if !chooseInitialMuted(true, false, detectMuted) {
		t.Fatal("expected --muted to force muted state")
	}
	if detectCalled {
		t.Fatal("detector should not run when --muted is set")
	}

	detectCalled = false
	if chooseInitialMuted(false, true, detectMuted) {
		t.Fatal("expected --unmuted to force unmuted state")
	}
	if detectCalled {
		t.Fatal("detector should not run when --unmuted is set")
	}
}

func TestExplicitUpdateDisablesStartupDetection(t *testing.T) {
	for {
		select {
		case <-mutedModeChan:
		default:
			detectVMState.Store(true)
			updateSystrayForMutedMode(true)
			if detectVMState.Load() {
				t.Fatal("explicit mute update should disable startup detection")
			}
			return
		}
	}
}

package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

func TestNativeMacQuitShortcut(t *testing.T) {
	if os.Getenv("PI_MANAGER_DESKTOP_TEST") != "1" {
		t.Skip("set PI_MANAGER_DESKTOP_TEST=1 on macOS to test native Command-Q")
	}
	// AppKit must initialize on one thread, independently of other native tests.
	if os.Getenv("PI_MANAGER_QUIT_TEST_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeMacQuitShortcut$", "-test.v")
		cmd.Env = append(os.Environ(), "PI_MANAGER_QUIT_TEST_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("macOS quit test: %v\n%s", err, output)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a := desktopTestApp(t)
	w, err := newDesktop(a)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	exit := w.(*managedDesktop).exit
	prompts, exits, minimizes := 0, 0, 0
	exit.dispatch = func(fn func()) { fn() }
	exit.show = func() {}
	exit.prompt = func() { prompts++ }
	exit.terminate = func() { exits++ }
	exit.minimize = func() { minimizes++ }
	exit.Ready()
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	menu := app.Send(objc.RegisterName("mainMenu"))
	if menu == 0 {
		t.Fatal("native application menu is missing")
	}
	appMenu := menu.Send(objc.RegisterName("itemAtIndex:"), int64(0)).Send(objc.RegisterName("submenu"))
	sendKey := func(modifiers uint64) bool {
		t.Helper()
		event := objc.ID(objc.GetClass("NSEvent")).Send(objc.RegisterName("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
			uint64(10), struct{ X, Y float64 }{}, modifiers, float64(0), int64(0), objc.ID(0), nsString("q"), nsString("q"), false, uint16(12))
		if event == 0 {
			t.Fatal("could not create native keyboard event")
		}
		return objc.Send[bool](menu, objc.RegisterName("performKeyEquivalent:"), event)
	}
	if sendKey(1<<18) || prompts != 0 { // Control-Q must not quit.
		t.Fatal("quit shortcut accepted the wrong modifier")
	}
	if !sendKey(1<<20) || prompts != 1 || !exit.pending || exits != 0 {
		t.Fatal("Command-Q did not request an exit confirmation")
	}
	sendKey(1 << 20)
	if prompts != 1 {
		t.Fatal("repeated Command-Q duplicated the confirmation")
	}
	if err := exit.Decide("cancel", false); err != nil {
		t.Fatal(err)
	}
	var quit objc.ID
	var quitIndex int64
	for i, count := int64(0), int64(appMenu.Send(objc.RegisterName("numberOfItems"))); i < count; i++ {
		item := appMenu.Send(objc.RegisterName("itemAtIndex:"), i)
		if objc.SEL(item.Send(objc.RegisterName("action"))) == objc.RegisterName("quitPiManager:") {
			quit, quitIndex = item, i
			break
		}
	}
	if quit == 0 {
		t.Fatal("application-menu Quit item is missing")
	}
	appMenu.Send(objc.RegisterName("performActionForItemAtIndex:"), quitIndex)
	if prompts != 2 {
		t.Fatal("clicking Quit did not use the same exit confirmation")
	}
	if err := exit.Decide("minimize", false); err != nil || minimizes != 1 || exits != 0 {
		t.Fatal("Minimize did not keep the app running", err)
	}
	if err := a.config.SetPreference(exitPreference, "minimize"); err != nil {
		t.Fatal(err)
	}
	sendKey(1 << 20)
	if minimizes != 2 || prompts != 2 || exits != 0 {
		t.Fatal("Command-Q ignored the remembered Minimize decision")
	}
	if err := a.config.SetPreference(exitPreference, "ask"); err != nil {
		t.Fatal(err)
	}
	sendKey(1 << 20)
	if err := exit.Decide("exit", false); err != nil || exits != 1 {
		t.Fatal("confirmed Command-Q did not exit", err)
	}
	// Reuse and restore an existing Quit item without replacing the host menu.
	count, originalTarget := appMenu.Send(objc.RegisterName("numberOfItems")), quit.Send(objc.RegisterName("target"))
	requests := 0
	closeMenu, err := bindMacQuitMenu(app, func() { requests++ })
	if err != nil {
		t.Fatal(err)
	}
	sendKey(1 << 20)
	closeMenu()
	if requests != 1 || app.Send(objc.RegisterName("mainMenu")) != menu || appMenu.Send(objc.RegisterName("numberOfItems")) != count || quit.Send(objc.RegisterName("target")) != originalTarget {
		t.Fatal("rebinding Quit replaced, duplicated or failed to restore the existing menu")
	}
}

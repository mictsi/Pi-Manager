package main

import "github.com/ebitengine/purego/objc"

// A status-bar Quit item does not register an application keyboard shortcut.
// Install Command-Q in the main menu and send it directly to the shared exit
// controller, preserving any menu supplied by the webview or its host.
func bindMacQuitMenu(app objc.ID, request func()) (func(), error) {
	quitAction := objc.RegisterName("quitPiManager:")
	class, err := objc.RegisterClass("PiManagerQuitMenu"+fmtTrayID(), objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: quitAction, Fn: func(objc.ID, objc.SEL, objc.ID) { request() }},
	})
	if err != nil {
		return nil, err
	}
	target := objc.ID(class).Send(objc.RegisterName("new"))
	menu := app.Send(objc.RegisterName("mainMenu"))
	createdMenu := menu == 0
	if createdMenu {
		menu = objc.ID(objc.GetClass("NSMenu")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithTitle:"), nsString("Pi-Manager"))
	} else {
		menu.Send(objc.RegisterName("retain"))
	}
	var appItem, appMenu objc.ID
	if menu.Send(objc.RegisterName("numberOfItems")) != 0 {
		appItem = menu.Send(objc.RegisterName("itemAtIndex:"), int64(0))
		appMenu = appItem.Send(objc.RegisterName("submenu"))
	}
	createdAppMenu := appMenu == 0
	if createdAppMenu {
		appItem = objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithTitle:action:keyEquivalent:"), nsString("Pi-Manager"), objc.SEL(0), nsString(""))
		appMenu = objc.ID(objc.GetClass("NSMenu")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithTitle:"), nsString("Pi-Manager"))
		appItem.Send(objc.RegisterName("setSubmenu:"), appMenu)
		menu.Send(objc.RegisterName("insertItem:atIndex:"), appItem, int64(0))
		appMenu.Send(objc.RegisterName("release"))
		appItem.Send(objc.RegisterName("release"))
	}
	const commandMask = uint64(1 << 20) // NSEventModifierFlagCommand
	var quit objc.ID
	for i, count := int64(0), int64(appMenu.Send(objc.RegisterName("numberOfItems"))); i < count; i++ {
		item := appMenu.Send(objc.RegisterName("itemAtIndex:"), i)
		key := objc.Send[string](item.Send(objc.RegisterName("keyEquivalent")), objc.RegisterName("UTF8String"))
		if objc.SEL(item.Send(objc.RegisterName("action"))) == objc.RegisterName("terminate:") ||
			(key == "q" && uint64(item.Send(objc.RegisterName("keyEquivalentModifierMask"))) == commandMask) {
			quit = item
			break
		}
	}
	createdQuit := quit == 0
	var previousTarget, previousKey objc.ID
	var previousAction objc.SEL
	var previousModifiers uint64
	if createdQuit {
		quit = objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithTitle:action:keyEquivalent:"), nsString("Quit Pi-Manager"), quitAction, nsString("q"))
		appMenu.Send(objc.RegisterName("addItem:"), quit)
		quit.Send(objc.RegisterName("release"))
	} else {
		previousTarget = quit.Send(objc.RegisterName("target")).Send(objc.RegisterName("retain"))
		previousKey = quit.Send(objc.RegisterName("keyEquivalent")).Send(objc.RegisterName("retain"))
		previousAction = objc.SEL(quit.Send(objc.RegisterName("action")))
		previousModifiers = uint64(quit.Send(objc.RegisterName("keyEquivalentModifierMask")))
	}
	quit.Send(objc.RegisterName("setTarget:"), target)
	quit.Send(objc.RegisterName("setAction:"), quitAction)
	quit.Send(objc.RegisterName("setKeyEquivalent:"), nsString("q"))
	quit.Send(objc.RegisterName("setKeyEquivalentModifierMask:"), commandMask)
	if createdMenu {
		app.Send(objc.RegisterName("setMainMenu:"), menu)
	}
	return func() {
		if createdQuit {
			appMenu.Send(objc.RegisterName("removeItem:"), quit)
		} else {
			quit.Send(objc.RegisterName("setTarget:"), previousTarget)
			quit.Send(objc.RegisterName("setAction:"), previousAction)
			quit.Send(objc.RegisterName("setKeyEquivalent:"), previousKey)
			quit.Send(objc.RegisterName("setKeyEquivalentModifierMask:"), previousModifiers)
			previousTarget.Send(objc.RegisterName("release"))
			previousKey.Send(objc.RegisterName("release"))
		}
		if createdAppMenu {
			menu.Send(objc.RegisterName("removeItem:"), appItem)
		}
		if createdMenu && app.Send(objc.RegisterName("mainMenu")) == menu {
			app.Send(objc.RegisterName("setMainMenu:"), objc.ID(0))
		}
		menu.Send(objc.RegisterName("release"))
		target.Send(objc.RegisterName("release"))
	}, nil
}

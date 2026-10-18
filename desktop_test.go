package main

import (
	"strings"
	"testing"
)

func TestEmbeddedDashboardUsesNativeConfiguration(t *testing.T) {
	html := desktopHTML(Config{Servers: []Server{{ID: "4", Label: `Name </script><script>alert(1)</script>`, URL: "https://pi.hole/api"}}, Preferences: map[string]string{"ph_combo_theme": "light"}})
	for _, expected := range []string{`<title>Pi-Manager</title>`, `<h1>Pi-Manager `, `data-theme="light" data-sk-theme="light"`, `data-sk-density="dense"`, `"id":"4"`, `"url":"https://pi.hole/api"`, "bootstrap().catch(showError)", "nativeLogin", "connect-src 'none'", "4.4.1", "max-width:none", "Ελληνικά", "Svenska", "Sekura.createDrawer", "Sekura.createDialog", "Sekura.createToaster"} {
		if !strings.Contains(html, expected) {
			t.Error("missing", expected)
		}
	}
	for _, bad := range []string{`<script src=`, "Name </script>", "renderTabs(),bootstrap();", "const ENGLISH_REPLACEMENTS"} {
		if strings.Contains(html, bad) {
			t.Error("unsafe/unadapted HTML", bad)
		}
	}
	for _, oldName := range []string{"dobbelt-pi-hole", "tursoe", "double pi-hole", "double-pihole"} {
		if strings.Contains(strings.ToLower(html), oldName) {
			t.Error("old project identity in embedded interface:", oldName)
		}
	}
}

package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A plugin's masked input keeps its retained editor masked, which is what
// draws bullets and refuses copy and cut of the password.
func TestPluginMaskedFieldStaysMaskedAndRefusesCopy(t *testing.T) {
	n := &ui.Node{Kind: ui.KindTextField, Key: "pw", Action: pluginActionPrefix + "v3:pw", Text: "hunter2", Masked: true}
	eds := map[string]*retainedEditor{}
	overlayEditors(n, eds)
	overlayEditors(n, eds) // the second pass reuses the retained slot
	f := eds[n.StableKey()].field
	if !f.Masked {
		t.Fatal("the retained editor lost the plugin's masked flag")
	}
	f.SelectAll()
	if r := f.HandleKey(ui.KeyInput{Sym: 'c', Text: "c", Mods: ui.ModCtrl}); r.Copy != "" {
		t.Fatalf("copy from a masked field returned %q", r.Copy)
	}
}

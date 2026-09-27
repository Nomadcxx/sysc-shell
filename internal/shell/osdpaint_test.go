package shell

import "testing"

// The OSD draws real glyph ink for its label: more than the placeholder's
// row of 4x8 bars, and different pixels for different labels.
func TestOSDPaintsRealText(t *testing.T) {
	r := newPanelRegistry(t)
	m := newOSDManager(r, 0)
	paint := func(v OSDView) []byte {
		r.mu.Lock()
		m.view, m.theme = v, r.panelTheme()
		r.mu.Unlock()
		pix := make([]byte, osdWidth*osdHeight*4)
		if err := m.render(pix, osdWidth, osdHeight, osdWidth*4); err != nil {
			t.Fatal(err)
		}
		return pix
	}
	a := paint(OSDView{Kind: osdCapsLock, On: true})
	b := paint(OSDView{Kind: osdNumLock})
	same := true
	for i := range a {
		if a[i] != b[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("Caps Lock and Num Lock painted identical pixels: the label is not text")
	}
}

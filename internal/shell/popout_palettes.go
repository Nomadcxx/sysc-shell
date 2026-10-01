package shell

// paletteUI is the Palettes page's state. Task 9 completes it; the preview
// flag is what panel teardown needs to hide a painted draft.
type paletteUI struct {
	preview bool // a draft preview is painted and must be hidden
}

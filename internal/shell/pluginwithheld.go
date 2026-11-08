package shell

import (
	"fmt"
	"log"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

// withheldClickWindow is how long after the user's input to a plugin a
// refused call still counts as the answer to it. A panel.open follows its
// click within milliseconds; a refusal later than this is the plugin's own.
const withheldClickWindow = 5 * time.Second

// markInputLocked records that the user just sent input to a plugin.
func (h *pluginHost) markInputLocked(id string) {
	if h.lastInput == nil {
		h.lastInput = map[string]time.Time{}
	}
	h.lastInput[id] = time.Now()
}

// withheldCall hears a call refused because the plugin changed since the user
// approved it. The plugin may discard the refusal, so a click would appear to
// do nothing; when the refusal answers the user's own input, the shell says
// why, once per copy of the plugin (digest).
func (h *pluginHost) withheldCall(id, name, digest string, c plugin.Capability) {
	h.mu.Lock()
	at, ok := h.lastInput[id]
	if !ok || time.Since(at) > withheldClickWindow || h.withheldToasted[id] == digest {
		h.mu.Unlock()
		return
	}
	if h.withheldToasted == nil {
		h.withheldToasted = map[string]string{}
	}
	h.withheldToasted[id] = digest
	toast := h.opts.WithheldToast
	h.mu.Unlock()
	if toast != nil {
		toast(id, name, c)
	}
}

// PluginWithheldToast tells the user a plugin's permissions are paused and
// names what the refused call wanted to do. A repeat for the same plugin
// replaces its toast rather than stacking.
func (r *Registry) PluginWithheldToast(id, name string, c plugin.Capability) {
	r.mu.Lock()
	locale := i18n.Resolve(r.cfg.Session.Language)
	r.mu.Unlock()
	key, english := withheldToastBody(c)
	summary := fmt.Sprintf(i18n.T(locale, "plugins.withheld_toast_summary", "%s changed since you approved it"), name)
	body := i18n.T(locale, key, english)
	if _, err := r.publishToast("sysc-shell:plugin-withheld:"+id, summary, body, protocol.UrgencyNormal, -1); err != nil {
		log.Printf("shell: plugin %s withheld toast: %v", id, err)
	}
}

// withheldToastBody is the catalog key and English text naming what a call of
// capability c wanted to do.
func withheldToastBody(c plugin.Capability) (key, english string) {
	switch c {
	case plugin.CapPanels, plugin.CapFloatingSurfaces:
		return "plugins.withheld_toast_panel", "Approve it again in Settings > Plugins to let it open its panel."
	case plugin.CapNotifications:
		return "plugins.withheld_toast_notify", "Approve it again in Settings > Plugins to let it post notifications."
	case plugin.CapClipboardRead, plugin.CapClipboardWrite:
		return "plugins.withheld_toast_clipboard", "Approve it again in Settings > Plugins to let it use the clipboard."
	case plugin.CapOpenURL:
		return "plugins.withheld_toast_links", "Approve it again in Settings > Plugins to let it open links."
	case plugin.CapFiles:
		return "plugins.withheld_toast_files", "Approve it again in Settings > Plugins to let it open files."
	case plugin.CapLocation:
		return "plugins.withheld_toast_location", "Approve it again in Settings > Plugins to let it read your location."
	case plugin.CapScreenshotCapture, plugin.CapScreenshotRead:
		return "plugins.withheld_toast_screenshot", "Approve it again in Settings > Plugins to let it take screenshots."
	}
	return "plugins.withheld_toast_other", "Approve it again in Settings > Plugins to turn its permissions back on."
}

package plugin

import v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"

// TextOut coalesces committed text-change events per node. Submit and other
// control events stay ordered and take the newest pending change with them.
type TextOut struct {
	change map[string]v1.InputEvent
}

func textKey(ev v1.InputEvent) string { return ev.ViewID + "\x00" + ev.Node }

// Push stores a change or drains the pending change for that node in front of
// a control event.
func (o *TextOut) Push(ev v1.InputEvent) []v1.InputEvent {
	if o.change == nil {
		o.change = map[string]v1.InputEvent{}
	}
	if ev.Event == v1.EventChange {
		o.change[textKey(ev)] = ev
		return nil
	}
	k := textKey(ev)
	var out []v1.InputEvent
	if prev, ok := o.change[k]; ok {
		delete(o.change, k)
		out = append(out, prev)
	}
	return append(out, ev)
}

// Flush returns every pending change and clears the buffer.
func (o *TextOut) Flush() []v1.InputEvent {
	if len(o.change) == 0 {
		return nil
	}
	out := make([]v1.InputEvent, 0, len(o.change))
	for k, ev := range o.change {
		out = append(out, ev)
		delete(o.change, k)
	}
	return out
}

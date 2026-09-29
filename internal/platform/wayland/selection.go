package wayland

import (
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// pasteLimit and pasteTimeout bound one clipboard read. A source that sends
// more, or stalls, cannot hold a field or a goroutine for long.
const (
	pasteLimit   = 1 << 20
	pasteTimeout = time.Second
)

// textMimes are offered on copy and accepted on paste, in preference order.
var textMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "TEXT", "STRING"}

// SelectionRequest asks the owner to act on the system clipboard for the
// input event with Serial.
type SelectionRequest struct {
	// Copy, when non-empty, becomes the clipboard's text.
	Copy string
	// Data, when non-empty, becomes the clipboard's content, offered as Mime
	// and nothing else. It is never mutated after the request is sent.
	Mime string
	Data []byte
	// Paste asks for the clipboard's text, delivered later as EventPaste.
	Paste  bool
	Serial uint32
	// Done, when non-nil, receives once the owner has handled a copy: nil
	// once set_selection is sent, or why it was not. The sender buffers it;
	// the owner never blocks on it.
	//
	// The compositor ignores set_selection from a client without keyboard
	// focus, so a surface that copies and then closes must wait for Done
	// before it closes.
	Done chan<- error
}

// offer is the MIME types a copy offers and the bytes it serves for each.
func (req SelectionRequest) offer() ([]string, []byte) {
	if len(req.Data) > 0 {
		return []string{req.Mime}, req.Data
	}
	return textMimes, []byte(req.Copy)
}

// answer reports a copy's outcome to its sender, if it asked.
func (req SelectionRequest) answer(err error) {
	if req.Done == nil {
		return
	}
	select {
	case req.Done <- err:
	default:
	}
}

func pickTextMime(offered []string) (string, bool) {
	for _, want := range textMimes {
		for _, got := range offered {
			if got == want {
				return want, true
			}
		}
	}
	return "", false
}

// readPaste reads at most limit bytes, giving up after timeout, and closes r.
// It runs off the owner goroutine; r must be pollable for the deadline to
// hold, which is what os.Pipe gives the read end.
func readPaste(r *os.File, limit int64, timeout time.Duration) (string, error) {
	defer r.Close()
	if err := r.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sanitizePaste(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// selectionState is the owner's view of the seat's clipboard.
type selectionState struct {
	manager *client.DataDeviceManager
	device  *client.DataDevice
	source  *client.DataSource
	offer   *client.DataOffer
	mimes   map[*client.DataOffer][]string
	warned  map[string]bool
}

type pasteResult struct {
	unit *surfaceUnit
	text string
	err  error
}

// warnSelectionOnce logs one clipboard failure per kind, never per key.
func (o *owner) warnSelectionOnce(kind string, err error) {
	if o.sel.warned == nil {
		o.sel.warned = map[string]bool{}
	}
	if !o.sel.warned[kind] {
		o.sel.warned[kind] = true
		log.Printf("wayland: clipboard %s: %v", kind, err)
	}
}

// bindSelection binds wl_data_device_manager (v3) and the seat's data
// device. Without a manager, copy and paste are unavailable and silent.
func (o *owner) bindSelection(ctx *client.Context) error {
	entry, ok := o.rs.singletons["wl_data_device_manager"]
	if !ok {
		return nil
	}
	o.sel.manager = client.NewDataDeviceManager(ctx)
	if err := o.registry.Bind(entry.global, "wl_data_device_manager", min(entry.version, 3), o.sel.manager); err != nil {
		return err
	}
	dev, err := o.sel.manager.GetDataDevice(o.seat)
	if err != nil {
		return err
	}
	o.sel.device = dev
	o.sel.mimes = map[*client.DataOffer][]string{}
	dev.SetDataOfferHandler(func(e client.DataDeviceDataOfferEvent) {
		offer := e.Id
		o.sel.mimes[offer] = nil
		offer.SetOfferHandler(func(m client.DataOfferOfferEvent) {
			o.sel.mimes[offer] = append(o.sel.mimes[offer], m.MimeType)
		})
	})
	// A nil Id means the clipboard was cleared.
	dev.SetSelectionHandler(func(e client.DataDeviceSelectionEvent) {
		if o.sel.offer != nil && o.sel.offer != e.Id {
			_ = o.sel.offer.Destroy()
			delete(o.sel.mimes, o.sel.offer)
		}
		o.sel.offer = e.Id
	})
	return nil
}

// destroySelection releases the data device and its manager.
func (o *owner) destroySelection() []error {
	var errs []error
	if o.sel.source != nil {
		errs = append(errs, o.sel.source.Destroy())
	}
	if o.sel.offer != nil {
		errs = append(errs, o.sel.offer.Destroy())
	}
	if o.sel.device != nil {
		errs = append(errs, o.sel.device.Release())
	}
	if o.sel.manager != nil {
		errs = append(errs, o.sel.manager.Destroy())
	}
	o.sel = selectionState{}
	return errs
}

// handleSelection serves one request from the shell on the owner goroutine.
// Pipe writes and reads run on their own goroutines.
func (o *owner) handleSelection(req SelectionRequest, results chan<- pasteResult) {
	copying := req.Copy != "" || len(req.Data) > 0
	if o.sel.device == nil {
		if copying {
			req.answer(errors.New("wayland: the seat has no data device"))
		}
		return
	}
	switch {
	case copying:
		src, err := o.sel.manager.CreateDataSource()
		if err != nil {
			o.warnSelectionOnce("source", err)
			req.answer(err)
			return
		}
		mimes, payload := req.offer()
		for _, m := range mimes {
			_ = src.Offer(m)
		}
		src.SetSendHandler(func(e client.DataSourceSendEvent) {
			f := os.NewFile(uintptr(e.Fd), "wl-selection-send")
			go func() { _, _ = f.Write(payload); f.Close() }()
		})
		// The compositor cancels a source when another client takes the
		// clipboard, including our own next copy.
		src.SetCancelledHandler(func(client.DataSourceCancelledEvent) {
			_ = src.Destroy()
			if o.sel.source == src {
				o.sel.source = nil
			}
		})
		if err := o.sel.device.SetSelection(src, req.Serial); err != nil {
			o.warnSelectionOnce("set", err)
			req.answer(err)
			return
		}
		o.sel.source = src
		req.answer(nil)
	case req.Paste:
		if o.sel.offer == nil {
			return
		}
		mime, ok := pickTextMime(o.sel.mimes[o.sel.offer])
		if !ok {
			return
		}
		// os.Pipe leaves the read end pollable, so readPaste's deadline
		// holds; Fd hands the compositor a blocking write end.
		r, w, err := os.Pipe()
		if err != nil {
			o.warnSelectionOnce("pipe", err)
			return
		}
		err = o.sel.offer.Receive(mime, int(w.Fd()))
		w.Close()
		if err != nil {
			r.Close()
			o.warnSelectionOnce("receive", err)
			return
		}
		unit := o.keyFocus.unit
		go func() {
			text, err := readPaste(r, pasteLimit, pasteTimeout)
			results <- pasteResult{unit: unit, text: sanitizePaste(text), err: err}
		}()
	}
}

// deliverPaste runs on the owner goroutine. A result for a surface that no
// longer has keyboard focus is dropped: the user has moved on.
func (o *owner) deliverPaste(p pasteResult) {
	if p.err != nil {
		o.warnSelectionOnce("read", p.err)
		return
	}
	if p.text == "" || p.unit == nil || o.keyFocus.unit != p.unit {
		return
	}
	o.deliverUnit(o.keyFocus.host, p.unit, Event{Kind: EventPaste, Paste: p.text})
}

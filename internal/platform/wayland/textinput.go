package wayland

import (
	"fmt"

	"github.com/Nomadcxx/sysc-wayland/client"
	"github.com/Nomadcxx/sysc-wayland/cursorshape"
	"github.com/Nomadcxx/sysc-wayland/textinput"
)

type cursorShaper interface {
	SetShape(serial, shape uint32) error
}

func applyCursorShape(dev cursorShaper, serial uint32, ibeam bool) error {
	if dev == nil {
		return nil
	}
	shape := uint32(cursorshape.WpCursorShapeDeviceV1ShapeDefault)
	if ibeam {
		shape = uint32(cursorshape.WpCursorShapeDeviceV1ShapeText)
	}
	return dev.SetShape(serial, shape)
}

type imePending struct {
	preedit, commit string
	delBefore       uint32
	delAfter        uint32
}

func (o *owner) bindOptionalInput(ctx *client.Context) error {
	if entry, ok := o.rs.singletons["zwp_text_input_manager_v3"]; ok {
		o.textInputMgr = textinput.NewZwpTextInputManagerV3(ctx)
		if err := o.registry.Bind(entry.global, "zwp_text_input_manager_v3", entry.version, o.textInputMgr); err != nil {
			return fmt.Errorf("wayland: bind zwp_text_input_manager_v3: %w", err)
		}
		ti, err := o.textInputMgr.GetTextInput(o.seat)
		if err != nil {
			return fmt.Errorf("wayland: get text input: %w", err)
		}
		o.textInput = ti
		o.wireTextInput(ti)
	}
	if entry, ok := o.rs.singletons["wp_cursor_shape_manager_v1"]; ok {
		o.cursorMgr = cursorshape.NewWpCursorShapeManagerV1(ctx)
		if err := o.registry.Bind(entry.global, "wp_cursor_shape_manager_v1", entry.version, o.cursorMgr); err != nil {
			return fmt.Errorf("wayland: bind wp_cursor_shape_manager_v1: %w", err)
		}
	}
	return nil
}

func (o *owner) wireTextInput(ti *textinput.ZwpTextInputV3) {
	ti.SetPreeditStringHandler(func(e textinput.ZwpTextInputV3PreeditStringEvent) {
		o.ime.preedit = e.Text
	})
	ti.SetCommitStringHandler(func(e textinput.ZwpTextInputV3CommitStringEvent) {
		o.ime.commit = e.Text
	})
	ti.SetDeleteSurroundingTextHandler(func(e textinput.ZwpTextInputV3DeleteSurroundingTextEvent) {
		o.ime.delBefore = e.BeforeLength
		o.ime.delAfter = e.AfterLength
	})
	ti.SetDoneHandler(func(textinput.ZwpTextInputV3DoneEvent) {
		pending := o.ime
		o.ime = imePending{}
		if o.keyFocus.unit == nil {
			return
		}
		o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, Event{
			Kind:            EventIME,
			IMEPreedit:      pending.preedit,
			IMECommit:       pending.commit,
			IMEDeleteBefore: pending.delBefore,
			IMEDeleteAfter:  pending.delAfter,
		})
	})
}

func (o *owner) setTextInputEnabled(on bool) {
	if o.textInput == nil || o.imeOn == on {
		return
	}
	o.imeOn = on
	if on {
		o.fail(o.textInput.Enable())
	} else {
		o.fail(o.textInput.Disable())
	}
	o.fail(o.textInput.Commit())
}

func (o *owner) syncIME(u *surfaceUnit) {
	if o.keyFocus.unit != u {
		return
	}
	want := u != nil && u.app.WantIME != nil && u.app.WantIME()
	o.setTextInputEnabled(want)
}

func (o *owner) syncCursor(u *surfaceUnit, x, y float64, serial uint32) {
	ibeam := u != nil && u.app.IBeamAt != nil && u.app.IBeamAt(x, y)
	o.fail(applyCursorShape(o.cursorDevice, serial, ibeam))
}

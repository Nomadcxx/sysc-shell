package shell

import (
	"reflect"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// barPaintCache keeps the latest complete pixels, not pixels from a particular
// Wayland slot. Each callback starts by copying that frame into its supplied
// buffer, then repaints the changed regions; alternating slots therefore
// always contain a valid frame before partial paint begins.
type barPaintCache struct {
	pixels    []byte
	root      *ui.Node
	style     render.Style
	width     int
	height    int
	stride    int
	pixelLen  int
	cacheable bool
}

func (cache *barPaintCache) paint(canvas *render.Canvas, root *ui.Node, text *render.TextRenderer, style render.Style) error {
	width, height, stride, pixelLen := canvas.Width, canvas.Height, canvas.Stride, len(canvas.Pix)
	safe := barTreeCacheable(root)
	reuse := cache.cacheable && safe && cache.root != nil &&
		cache.width == width && cache.height == height && cache.stride == stride && cache.pixelLen == pixelLen &&
		reflect.DeepEqual(cache.style, style)

	if !reuse {
		if err := render.Paint(canvas, root, text, style); err != nil {
			*cache = barPaintCache{}
			return err
		}
	} else {
		regions := barDamageRegions(cache.root, root, style.Scale120, width, height)
		copy(canvas.Pix, cache.pixels)
		for _, region := range regions {
			if err := render.PaintRegion(canvas, root, text, style, region); err != nil {
				*cache = barPaintCache{}
				return err
			}
		}
	}

	cache.pixels = append(cache.pixels[:0], canvas.Pix...)
	cache.root, cache.style = root, style
	cache.width, cache.height, cache.stride, cache.pixelLen = width, height, stride, pixelLen
	cache.cacheable = safe
	return nil
}

// Dynamic lists and schedules can produce paint content from shared pointers or
// callbacks that the render snapshot cannot compare. Keep those uncommon bar
// trees on the established full-paint path.
func barTreeCacheable(n *ui.Node) bool {
	if n == nil || n.Item != nil || n.Schedule != nil || n.ScheduleItem != nil ||
		n.Kind == ui.KindVirtualList || n.Kind == ui.KindScheduleGrid {
		return false
	}
	for _, child := range n.Children {
		if !barTreeCacheable(child) {
			return false
		}
	}
	return true
}

func barDamageRegions(previous, current *ui.Node, scale ui.Scale120, width, height int) []ui.Rect {
	full := []ui.Rect{{W: width, H: height}}
	var regions []ui.Rect
	var compare func(*ui.Node, *ui.Node, bool)
	compare = func(old, next *ui.Node, root bool) {
		if root && (old == nil || next == nil) {
			regions = full
			return
		}
		if old != nil && next != nil && !samePaintNode(old, next) {
			before := len(regions)
			appendBarNodeBoundsOnly(&regions, old, scale, width, height)
			appendBarNodeBoundsOnly(&regions, next, scale, width, height)
			if root && len(regions) == before {
				regions = full
				return
			}
		}
		oldCount, nextCount := 0, 0
		if old != nil {
			oldCount = len(old.Children)
		}
		if next != nil {
			nextCount = len(next.Children)
		}
		for i := 0; i < max(oldCount, nextCount); i++ {
			var oldChild, nextChild *ui.Node
			if i < oldCount {
				oldChild = old.Children[i]
			}
			if i < nextCount {
				nextChild = next.Children[i]
			}
			if oldChild == nil || nextChild == nil {
				appendBarSubtreeBounds(&regions, oldChild, scale, width, height)
				appendBarSubtreeBounds(&regions, nextChild, scale, width, height)
				continue
			}
			compare(oldChild, nextChild, false)
		}
	}
	compare(previous, current, true)
	if len(regions) == 0 {
		return nil
	}
	return mergeBarDamage(regions, width, height)
}

func samePaintNode(a, b *ui.Node) bool {
	left, right := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	children := nodeChildrenFieldIndex()
	for i := 0; i < left.NumField(); i++ {
		if i == children {
			continue
		}
		if !samePaintValue(left.Field(i), right.Field(i)) {
			return false
		}
	}
	return true
}

// nodeChildrenFieldIndex is the field index of ui.Node.Children. It is a
// function rather than a package-level variable so no test can swap it out
// from under the bar's render goroutine, and it is computed once.
var nodeChildrenFieldIndex = sync.OnceValue(func() int {
	field, ok := reflect.TypeOf(ui.Node{}).FieldByName("Children")
	if !ok {
		panic("shell: ui.Node.Children field is missing")
	}
	return field.Index[0]
})

// nodeChildrenFieldIndexReady forces the index to be resolved at start-up, so
// a ui.Node without a Children field fails the process rather than the first
// frame that compares two nodes.
var _ = nodeChildrenFieldIndex()

func samePaintValue(a, b reflect.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Ptr:
		return a.Pointer() == b.Pointer()
	case reflect.Func:
		return a.IsNil() && b.IsNil()
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !samePaintValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return samePaintValue(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !samePaintValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String,
		reflect.Chan, reflect.UnsafePointer:
		return a.Equal(b)
	default:
		// A new non-comparable Node field conservatively causes damage rather
		// than allowing the cache to retain stale pixels.
		return false
	}
}

func appendBarNodeBoundsOnly(regions *[]ui.Rect, n *ui.Node, scale ui.Scale120, width, height int) {
	if n == nil {
		return
	}
	if n.Bounds.W > 0 && n.Bounds.H > 0 {
		box := scale.PhysicalRect(n.Bounds)
		right, bottom := min(width, box.X+box.W), min(height, box.Y+box.H)
		box.X, box.Y = max(0, box.X), max(0, box.Y)
		box.W, box.H = right-box.X, bottom-box.Y
		if box.W > 0 && box.H > 0 {
			*regions = append(*regions, box)
		}
	}
}

func appendBarSubtreeBounds(regions *[]ui.Rect, n *ui.Node, scale ui.Scale120, width, height int) {
	if n == nil {
		return
	}
	appendBarNodeBoundsOnly(regions, n, scale, width, height)
	for _, child := range n.Children {
		appendBarSubtreeBounds(regions, child, scale, width, height)
	}
}

func mergeBarDamage(regions []ui.Rect, width, height int) []ui.Rect {
	merged := make([]ui.Rect, 0, len(regions))
	for _, region := range regions {
		right, bottom := min(width, region.X+region.W), min(height, region.Y+region.H)
		region.X, region.Y = max(0, region.X), max(0, region.Y)
		region.W, region.H = right-region.X, bottom-region.Y
		if region.W <= 0 || region.H <= 0 {
			continue
		}
		for i := 0; i < len(merged); {
			old := merged[i]
			if region.X >= old.X+old.W || old.X >= region.X+region.W ||
				region.Y >= old.Y+old.H || old.Y >= region.Y+region.H {
				i++
				continue
			}
			x0, y0 := min(region.X, old.X), min(region.Y, old.Y)
			x1, y1 := max(region.X+region.W, old.X+old.W), max(region.Y+region.H, old.Y+old.H)
			region = ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
			merged = append(merged[:i], merged[i+1:]...)
			i = 0
		}
		merged = append(merged, region)
	}
	return merged
}

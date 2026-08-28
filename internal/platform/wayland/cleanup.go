package wayland

import "errors"

// cleanupStep is one named destructor.
type cleanupStep struct {
	name string
	fn   func() error
}

// cleanupStack unwinds resources in reverse creation order, which is
// child-to-parent. Every step runs even when an earlier one fails, so a single
// failure cannot strand the remaining resources.
type cleanupStack struct {
	steps []cleanupStep
}

// push registers a destructor at creation time.
func (c *cleanupStack) push(name string, fn func() error) {
	c.steps = append(c.steps, cleanupStep{name: name, fn: fn})
}

// unwind runs every destructor in reverse order and reports the names run,
// joined with any failures.
func (c *cleanupStack) unwind() ([]string, error) {
	order := make([]string, 0, len(c.steps))
	var errs []error
	for i := len(c.steps) - 1; i >= 0; i-- {
		step := c.steps[i]
		order = append(order, step.name)
		if err := step.fn(); err != nil {
			errs = append(errs, err)
		}
	}
	c.steps = nil
	return order, errors.Join(errs...)
}

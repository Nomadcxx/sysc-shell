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

// unwindTo runs destructors in reverse order down to, but not including, the
// named step, and keeps that step and everything below it. It is how a host
// tears down its surface while keeping its wl_output.
func (c *cleanupStack) unwindTo(name string) ([]string, error) {
	stop := len(c.steps)
	for i := len(c.steps) - 1; i >= 0; i-- {
		if c.steps[i].name == name {
			stop = i + 1
			break
		}
	}
	order := make([]string, 0, len(c.steps)-stop)
	var errs []error
	for i := len(c.steps) - 1; i >= stop; i-- {
		order = append(order, c.steps[i].name)
		if err := c.steps[i].fn(); err != nil {
			errs = append(errs, err)
		}
	}
	c.steps = c.steps[:stop]
	return order, errors.Join(errs...)
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

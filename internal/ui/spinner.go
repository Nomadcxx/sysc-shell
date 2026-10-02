package ui

// SpinnerSize is a spinner's diameter when its node gives none: an inline
// indicator the height of a line of body text.
const SpinnerSize = 20

// SpinnerDiameter is the square a spinner occupies. It ignores the band its
// row offers, unlike a gauge: a spinner beside a label must not swell to the
// row's height.
func SpinnerDiameter(n *Node) int {
	if n.Width > 0 {
		return n.Width
	}
	return SpinnerSize
}

package shell

type mediaStripIntent struct{}

func (i *mediaStripIntent) setShown(bool) {}
func (i *mediaStripIntent) strip(bool)    {}

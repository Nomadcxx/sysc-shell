package shell

import (
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// A reload that leaves the place alone must not push location.changed again,
// and each plugin is tracked on its own.
func TestPluginLocationsPushOnlyNews(t *testing.T) {
	var l pluginLocations
	melbourne := v1.Location{Configured: true, Latitude: -37.81, Longitude: 144.96, Label: "Melbourne"}
	if !l.record("a", melbourne) {
		t.Fatal("first location for a plugin is not news")
	}
	if l.record("a", melbourne) {
		t.Fatal("an unchanged location was pushed again")
	}
	if !l.record("b", melbourne) {
		t.Fatal("a second plugin shared the first one's record")
	}
	london := v1.Location{Configured: true, Latitude: 51.51, Longitude: -0.13, Label: "London"}
	if !l.record("a", london) {
		t.Fatal("a moved location was not news")
	}
}

func TestPluginLocationsDropSupersededPushes(t *testing.T) {
	var l pluginLocations
	older := l.nextRevision()
	newer := l.nextRevision()
	melbourne := v1.Location{Configured: true, Latitude: -37.81, Longitude: 144.96, Label: "Melbourne"}
	london := v1.Location{Configured: true, Latitude: 51.51, Longitude: -0.13, Label: "London"}

	sentNewer := false
	if !l.sendIfCurrent(newer, "a", london, func() error {
		sentNewer = true
		return nil
	}) || !sentNewer {
		t.Fatal("latest location was not sent")
	}

	sentOlder := false
	if l.sendIfCurrent(older, "a", melbourne, func() error {
		sentOlder = true
		return nil
	}) || sentOlder {
		t.Fatal("superseded location was sent")
	}
	if l.record("a", london) {
		t.Fatal("superseded push replaced the latest location")
	}
}

func TestPluginLocationsRevisionDoesNotWaitForSend(t *testing.T) {
	var l pluginLocations
	revision := l.nextRevision()
	started := make(chan struct{})
	release := make(chan struct{})
	sendDone := make(chan struct{})
	defer func() {
		close(release)
		<-sendDone
	}()

	go func() {
		l.sendIfCurrent(revision, "a", v1.Location{Label: "London"}, func() error {
			close(started)
			<-release
			return nil
		})
		close(sendDone)
	}()
	<-started

	revisionDone := make(chan struct{})
	go func() {
		l.nextRevision()
		close(revisionDone)
	}()
	select {
	case <-revisionDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("config reload waited for a location send")
	}
}

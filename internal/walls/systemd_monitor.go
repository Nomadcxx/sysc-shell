package walls

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	systemdBusName      = "org.freedesktop.systemd1"
	systemdManagerPath  = dbus.ObjectPath("/org/freedesktop/systemd1")
	systemdManagerIface = "org.freedesktop.systemd1.Manager"
	systemdUnitPath     = dbus.ObjectPath("/org/freedesktop/systemd1/unit/sysc_2dwalls_2eservice")
)

func (s *Service) watchSystemd(done chan struct{}) {
	defer close(done)
	retry := time.Second
	for {
		conn, err := dbus.ConnectSessionBus()
		if err == nil {
			var signals chan *dbus.Signal
			signals, err = subscribeSystemdSignals(conn)
			if err == nil {
				retry = time.Second
				// Close the gap between the initial discovery and subscription.
				s.systemdChanged()
				if !watchSystemdSignals(signals, conn.Context().Done(), s.quit, s.systemdChanged) {
					conn.RemoveSignal(signals)
					_ = conn.Close()
					return
				}
				conn.RemoveSignal(signals)
			}
			_ = conn.Close()
		}
		if err != nil {
			log.Printf("sysc-walls: monitoring user systemd unit changes: %v", err)
		}
		if !waitSystemdRetry(s.quit, retry) {
			return
		}
		if retry < 30*time.Second {
			retry *= 2
			if retry > 30*time.Second {
				retry = 30 * time.Second
			}
		}
	}
}

func subscribeSystemdSignals(conn *dbus.Conn) (chan *dbus.Signal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	match := []dbus.MatchOption{dbus.WithMatchSender(systemdBusName)}
	if err := conn.AddMatchSignalContext(ctx, match...); err != nil {
		return nil, fmt.Errorf("subscribe to systemd signals: %w", err)
	}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	if err := conn.Object(systemdBusName, systemdManagerPath).
		CallWithContext(ctx, systemdManagerIface+".Subscribe", 0).Err; err != nil {
		conn.RemoveSignal(signals)
		_ = conn.RemoveMatchSignal(match...)
		return nil, fmt.Errorf("subscribe to systemd unit changes: %w", err)
	}
	return signals, nil
}

func watchSystemdSignals(signals <-chan *dbus.Signal, disconnected, quit <-chan struct{}, refresh func()) bool {
	for {
		select {
		case <-quit:
			return false
		case <-disconnected:
			return true
		case signal, ok := <-signals:
			if !ok {
				return true
			}
			if systemdSignalAffectsWalls(signal) {
				refresh()
			}
		}
	}
}

func systemdSignalAffectsWalls(signal *dbus.Signal) bool {
	if signal == nil {
		return false
	}
	switch signal.Name {
	case systemdManagerIface + ".UnitFilesChanged":
		return signal.Path == systemdManagerPath
	case systemdManagerIface + ".JobRemoved":
		if signal.Path != systemdManagerPath || len(signal.Body) < 3 {
			return false
		}
		name, _ := signal.Body[2].(string)
		return name == unitName
	case systemdManagerIface + ".UnitNew", systemdManagerIface + ".UnitRemoved":
		// Ignored on purpose. Discovery runs 'systemctl --user show', which
		// loads a garbage-collected unit and lets systemd drop it again when
		// the query returns, so these signals describe our own probe and
		// reacting to them makes every discovery schedule the next one. Real
		// transitions still arrive as UnitFilesChanged, JobRemoved, or
		// PropertiesChanged for the unit object.
		return false
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		return signal.Path == systemdUnitPath
	default:
		return false
	}
}

func waitSystemdRetry(quit <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-quit:
		return false
	case <-timer.C:
		return true
	}
}

package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestServiceActionRoundTripsToPlugin(t *testing.T) {
	line := []byte(`{"type":"service.action","service_id":"lid-guard","action_id":"toggle"}` + "\n")
	message, err := NewDecoder(bytes.NewReader(line), ToPlugin).Decode()
	if err != nil {
		t.Fatalf("decode service.action: %v", err)
	}
	if got := reflect.TypeOf(message).Elem().Name(); got != "ServiceAction" {
		t.Fatalf("decoded %T, want *ServiceAction", message)
	}

	roundTrip, err := NewDecoder(bytes.NewReader(encodeOne(t, message)), ToPlugin).Decode()
	if err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	if !reflect.DeepEqual(message, roundTrip) {
		t.Fatalf("round trip = %#v, want %#v", roundTrip, message)
	}
}

func TestServiceActionRejectsPluginDirection(t *testing.T) {
	line := []byte(`{"type":"service.action","service_id":"lid-guard","action_id":"toggle"}` + "\n")
	_, err := NewDecoder(bytes.NewReader(line), ToHost).Decode()
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("decode error = %v, want ErrUnknownType", err)
	}
}

func TestServiceStatusRoundTripsToHost(t *testing.T) {
	line := []byte(`{"type":"service.status","service_id":"lid-guard","state":"active","busy":true,"message":"inhibiting lid switch","revision":9}` + "\n")
	message, err := NewDecoder(bytes.NewReader(line), ToHost).Decode()
	if err != nil {
		t.Fatalf("decode service.status: %v", err)
	}
	typ := reflect.TypeOf(message).Elem()
	if typ.Name() != "ServiceStatus" {
		t.Fatalf("decoded %T, want *ServiceStatus", message)
	}
	state, ok := typ.FieldByName("State")
	if !ok || state.Type.Name() != "ServiceState" {
		t.Fatalf("ServiceStatus.State = %v, want ServiceState enum", state.Type)
	}

	out := encodeOne(t, message)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	want := map[string]string{
		"type":       `"service.status"`,
		"service_id": `"lid-guard"`,
		"state":      `"active"`,
		"busy":       `true`,
		"message":    `"inhibiting lid switch"`,
		"revision":   `9`,
	}
	for field, value := range want {
		if string(got[field]) != value {
			t.Errorf("%s = %s, want %s", field, got[field], value)
		}
	}
}

func TestServiceStatusRejectsHostDirection(t *testing.T) {
	line := []byte(`{"type":"service.status","service_id":"lid-guard","state":"active","busy":false,"revision":1}` + "\n")
	_, err := NewDecoder(bytes.NewReader(line), ToPlugin).Decode()
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("decode error = %v, want ErrUnknownType", err)
	}
}

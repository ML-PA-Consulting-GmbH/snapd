package devicestate

import "testing"

func TestDeterministicDeviceSerial(t *testing.T) {
	serial, err := getDeviceSerial()
	if err != nil {
		t.Fatalf("failed to get device serial: %s", err)
	}
	t.Logf("device serial: %s", serial)
}

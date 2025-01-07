package main

import (
	"encoding/base64"
	"fmt"
	"github.com/snapcore/snapd/asserts"
)

const (
	testBodyToSign = "eyJjb250ZXh0IjpbeyJzbmFwLWlkIjoiMDcwN2EyNzlkODgwNzQ5ZDU3NmRjZjgxNGE1ZjBkZmYiLCJpbnN0YW5jZS1rZXkiOiIwNzA3YTI3OWQ4ODA3NDlkNTc2ZGNmODE0YTVmMGRmZiIsInJldmlzaW9uIjo3LCJ0cmFja2luZy1jaGFubmVsIjoibGF0ZXN0L3N0YWJsZSIsImVwb2NoIjp7InJlYWQiOlswXSwid3JpdGUiOlswXX0sInJlZnJlc2hlZC1kYXRlIjoiMjAyNC0wMi0yOVQxNDoyNDoyMloifSx7InNuYXAtaWQiOiIzM2JiMDdiNGI5MWViMDc1YWIyMzMzOTFkMWNlZjFlNiIsImluc3RhbmNlLWtleSI6IjMzYmIwN2I0YjkxZWIwNzVhYjIzMzM5MWQxY2VmMWU2IiwicmV2aXNpb24iOjksInRyYWNraW5nLWNoYW5uZWwiOiJsYXRlc3Qvc3RhYmxlIiwiZXBvY2giOnsicmVhZCI6WzBdLCJ3cml0ZSI6WzBdfSwicmVmcmVzaGVkLWRhdGUiOiIyMDI0LTAyLTI5VDE0OjI0OjIyWiJ9LHsic25hcC1pZCI6IjA2ODJmMzZkYTczYzQ5ZDhhNTVlZmZkZjY1NTMzZjlkIiwiaW5zdGFuY2Uta2V5IjoiMDY4MmYzNmRhNzNjNDlkOGE1NWVmZmRmNjU1MzNmOWQiLCJyZXZpc2lvbiI6MjQsInRyYWNraW5nLWNoYW5uZWwiOiJsYXRlc3Qvc3RhYmxlIiwiZXBvY2giOnsicmVhZCI6WzBdLCJ3cml0ZSI6WzBdfSwicmVmcmVzaGVkLWRhdGUiOiIyMDI0LTAzLTAxVDEwOjIwOjU2Ljc5OTk5OTA1NloifSx7InNuYXAtaWQiOiI2ZWJmMWZkNzkxMzA0ODQ1OTljMjM1ODg0OTZjYzI4MiIsImluc3RhbmNlLWtleSI6IjZlYmYxZmQ3OTEzMDQ4NDU5OWMyMzU4ODQ5NmNjMjgyIiwicmV2aXNpb24iOjEsInRyYWNraW5nLWNoYW5uZWwiOiJsYXRlc3Qvc3RhYmxlIiwiZXBvY2giOnsicmVhZCI6WzBdLCJ3cml0ZSI6WzBdfSwicmVmcmVzaGVkLWRhdGUiOiIyMDI0LTAyLTI5VDE0OjI0OjIyWiJ9LHsic25hcC1pZCI6Ijc3NWZmMjY5YWExNDQxMTQ5MDFiNWU3Zjg0ZGY2ZmU4IiwiaW5zdGFuY2Uta2V5IjoiNzc1ZmYyNjlhYTE0NDExNDkwMWI1ZTdmODRkZjZmZTgiLCJyZXZpc2lvbiI6MSwidHJhY2tpbmctY2hhbm5lbCI6ImxhdGVzdC9zdGFibGUiLCJlcG9jaCI6eyJyZWFkIjpbMF0sIndyaXRlIjpbMF19LCJyZWZyZXNoZWQtZGF0ZSI6IjIwMjQtMDItMjlUMTQ6MjQ6MjJaIn0seyJzbmFwLWlkIjoiNzY0YWJmZDc5ZGRkMjA0MzMzZTZhMDZhOTcwYjZiZjUiLCJpbnN0YW5jZS1rZXkiOiI3NjRhYmZkNzlkZGQyMDQzMzNlNmEwNmE5NzBiNmJmNSIsInJldmlzaW9uIjo3LCJ0cmFja2luZy1jaGFubmVsIjoibGF0ZXN0L3N0YWJsZSIsImVwb2NoIjp7InJlYWQiOlswXSwid3JpdGUiOlswXX0sInJlZnJlc2hlZC1kYXRlIjoiMjAyNC0wMi0yOVQxNDoyNDoyMloifSx7InNuYXAtaWQiOiI0ZDcwOWNjNGMyMWQ5MTc3NTc4YjU5NTQ4NWVjMjNhZSIsImluc3RhbmNlLWtleSI6IjRkNzA5Y2M0YzIxZDkxNzc1NzhiNTk1NDg1ZWMyM2FlIiwicmV2aXNpb24iOjEsInRyYWNraW5nLWNoYW5uZWwiOiJsYXRlc3Qvc3RhYmxlIiwiZXBvY2giOnsicmVhZCI6WzBdLCJ3cml0ZSI6WzBdfSwicmVmcmVzaGVkLWRhdGUiOiIyMDI0LTAyLTI5VDE0OjI0OjIyWiJ9XSwiYWN0aW9ucyI6W3siYWN0aW9uIjoicmVmcmVzaCIsImluc3RhbmNlLWtleSI6IjA2ODJmMzZkYTczYzQ5ZDhhNTVlZmZkZjY1NTMzZjlkIiwic25hcC1pZCI6IjA2ODJmMzZkYTczYzQ5ZDhhNTVlZmZkZjY1NTMzZjlkIn1dLCJmaWVsZHMiOlsiYXJjaGl0ZWN0dXJlcyIsImJhc2UiLCJjb25maW5lbWVudCIsImxpbmtzIiwiY29udGFjdCIsImNyZWF0ZWQtYXQiLCJkZXNjcmlwdGlvbiIsImRvd25sb2FkIiwiZXBvY2giLCJsaWNlbnNlIiwibmFtZSIsInByaWNlcyIsInByaXZhdGUiLCJwdWJsaXNoZXIiLCJyZXZpc2lvbiIsInNuYXAtaWQiLCJzbmFwLXlhbWwiLCJzdW1tYXJ5IiwidGl0bGUiLCJ0eXBlIiwidmVyc2lvbiIsIndlYnNpdGUiLCJzdG9yZS11cmwiLCJtZWRpYSIsImNvbW1vbi1pZHMiLCJjYXRlZ29yaWVzIl19"
)

func main() {
	runTest("Generating deterministic serial", func() (string, error) {
		if serial, err := asserts.DeterministicDeviceSerial(); err != nil {
			return "", err
		} else {
			return serial, nil
		}
	})

	runTest("Getting m2cp-public-key", func() (string, error) {
		if key, err := asserts.TpmGetEndorsementPublicKey(); err != nil {
			return "", err
		} else {
			return key.ID(), nil
		}
	})

	runTest("Getting m2cp-public-key-base64", func() (string, error) {
		if key, err := asserts.TpmGetEndorsementPublicKeyBase64(); err != nil {
			return "", err
		} else {
			return key, nil
		}
	})

	runTest("Signing 'hello-world'", func() (string, error) {
		if signature, err := asserts.TpmSignBytes([]byte("hello world")); err != nil {
			return "", err
		} else {
			return base64.StdEncoding.EncodeToString(signature), nil
		}
	})

	runTest(fmt.Sprintf("Signing test body of len %d", len(testBodyToSign)), func() (string, error) {
		if signature, err := asserts.TpmSignBytes([]byte(testBodyToSign)); err != nil {
			return "", err
		} else {
			return base64.StdEncoding.EncodeToString(signature), nil
		}
	})

}

func runTest(name string, f func() (string, error)) {
	fmt.Println(name)
	if res, err := f(); err != nil {
		fmt.Printf("\nfailed: %s\n\n", err)
	} else {
		fmt.Printf("\nsuccess\n%s\n\n", res)
	}
	fmt.Println()
}

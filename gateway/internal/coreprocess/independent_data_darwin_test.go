package coreprocess

import "testing"

func TestDataDirFromPSArgs(t *testing.T) {
	command := "/private/tmp/we-clay-core -stage resume -data /private/tmp/account with spaces/core -device-type mac -update-version=false -listen 127.0.0.1:8201 -grpc-listen 127.0.0.1:8088"
	got, err := dataDirFromPSArgs(command)
	if err != nil || got != "/private/tmp/account with spaces/core" {
		t.Fatalf("path with spaces: got=%q err=%v", got, err)
	}
	if _, err = dataDirFromPSArgs("core -data /private/tmp/account -grpc-listen 127.0.0.1:8088"); err == nil {
		t.Fatal("accepted ambiguous process command")
	}
}

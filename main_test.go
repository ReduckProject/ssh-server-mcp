package main

import "testing"

func TestRegisterDoesNotConnectImmediately(t *testing.T) {
	manager := NewSSHServerManager()

	err := manager.Register(SSHServerConfig{
		Name:     "unreachable",
		Host:     "192.0.2.1",
		Port:     22,
		User:     "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("Register() returned error before first use: %v", err)
	}

	if got := manager.GetConfig("unreachable"); got == nil {
		t.Fatal("registered server config was not stored")
	}
}

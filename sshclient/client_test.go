package sshclient

import "testing"

func TestNewSSHClientDoesNotConnectImmediately(t *testing.T) {
	client, err := NewSSHClient(SSHConfig{
		Host:     "192.0.2.1",
		Port:     22,
		User:     "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("NewSSHClient() returned error before first use: %v", err)
	}
	if client.client != nil {
		t.Fatal("NewSSHClient() should not establish an SSH connection")
	}
}

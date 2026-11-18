package main

import "testing"

func TestPeerIP(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"ipv4", "tcp ESTAB 0 0 10.0.0.1:22 203.0.113.5:54322", "203.0.113.5"},
		{"state filtered", "tcp 0 0 10.0.0.1:22 203.0.113.5:54322", "203.0.113.5"},
		{"ipv6", "tcp ESTAB 0 0 [2001:db8::1]:443 [2001:db8::2]:52345", "2001:db8::2"},
		{"ipv6 without brackets", "tcp ESTAB 0 0 :::22 ::1:52345", "::1"},
		{"header", "Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port", ""},
		{"header without state", "Netid Recv-Q Send-Q Local Address:Port Peer Address:Port", ""},
		{"short line", "tcp 0 0 10.0.0.1:22", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := peerIP(tt.line)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("peerIP() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.String() != tt.want {
				t.Fatalf("peerIP() = %v, want %s", got, tt.want)
			}
		})
	}
}

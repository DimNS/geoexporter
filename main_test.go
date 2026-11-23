package main

import "testing"

func TestPeerIP(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"tcp established",
			"   7: 7600A8C0:B452 CB403414:01BB 01 00000000:00000000 02:00000402 00000000  1000        0 140322 2 0000000000000000 27 4 30 10 -1",
			"20.52.64.203",
		},
		{
			"udp connected",
			" 946: FEFFFF0A:0035 CB403414:0035 01 00000000:00000000 00:00000000 00000000     0        0 9225 2 0000000000000000 0",
			"20.52.64.203",
		},
		{
			"tcp listen",
			"   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4861 1 0000000000000000 100 0 0 10 0",
			"",
		},
		{
			"udp unconnected",
			" 946: FEFFFF0A:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 9225 2 0000000000000000 0",
			"",
		},
		{
			"tcp6 established",
			"   0: 00000000000000000000000000000000:0050 00000000000000000000000001000000:1F90 01 00000000:00000000 00:00000000 00000000     0        0 4862 1 0000000000000000 100 0 0 10 0",
			"::1",
		},
		{
			"header",
			"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
			"",
		},
		{"short line", "   0: 0100007F:1F90", ""},
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

func TestDecodeIP(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0100007F", "127.0.0.1"},
		{"CB403414", "20.52.64.203"},
		{"00000000000000000000000001000000", "::1"},
		{"B80D0120000000000000000002000000", "2001:db8::2"},
		{"", ""},
		{"0100007", ""},
		{"ZZZZZZZZ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := decodeIP(tt.in)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("decodeIP(%q) = %v, want nil", tt.in, got)
				}
				return
			}
			if got == nil || got.String() != tt.want {
				t.Fatalf("decodeIP(%q) = %v, want %s", tt.in, got, tt.want)
			}
		})
	}
}

package protocol

import "testing"

func TestPacketRoundTripAndResize(t *testing.T) {
	packet, err := Parse(Encode(Packet{Type: Input, Data: []byte("ls\n")}))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Type != Input || string(packet.Data) != "ls\n" {
		t.Fatalf("packet = %#v", packet)
	}
	cols, rows, err := ParseResize([]byte{0, 120, 0, 40})
	if err != nil {
		t.Fatal(err)
	}
	if cols != 120 || rows != 40 {
		t.Fatalf("resize = %dx%d", cols, rows)
	}
}

func TestParseRejectsEmptyPacket(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
}

func TestParseCopiesPacketData(t *testing.T) {
	raw := []byte{byte(Input), 'l', 's'}
	packet, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[1] = 'x'
	if string(packet.Data) != "ls" {
		t.Fatalf("packet data = %q, want independent copy", packet.Data)
	}
}

func TestParseResizeRejectsMalformedDimensions(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "too short", data: []byte{0, 80, 0}},
		{name: "zero cols", data: []byte{0, 0, 0, 24}},
		{name: "zero rows", data: []byte{0, 80, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ParseResize(tt.data); err == nil {
				t.Fatal("ParseResize() error = nil, want error")
			}
		})
	}
}

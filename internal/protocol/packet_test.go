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

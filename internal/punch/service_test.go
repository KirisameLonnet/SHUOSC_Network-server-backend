package punch

import (
	"encoding/binary"
	"testing"
)

func TestParseBindingResponseXORMappedAddress(t *testing.T) {
	t.Parallel()

	var tx [12]byte
	copy(tx[:], []byte("abcdefghijkl"))

	packet := make([]byte, 32)
	binary.BigEndian.PutUint16(packet[0:2], bindingSuccess)
	binary.BigEndian.PutUint16(packet[2:4], 12)
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], tx[:])
	binary.BigEndian.PutUint16(packet[20:22], attrXORMapped)
	binary.BigEndian.PutUint16(packet[22:24], 8)
	packet[25] = 0x01
	binary.BigEndian.PutUint16(packet[26:28], 40000^uint16(stunMagicCookie>>16))
	ipRaw := uint32(203)<<24 | uint32(0)<<16 | uint32(113)<<8 | uint32(9)
	binary.BigEndian.PutUint32(packet[28:32], ipRaw^stunMagicCookie)

	gotTx, endpoint, ok := parseBindingResponse(packet)
	if !ok {
		t.Fatal("expected binding response to parse")
	}
	if gotTx != tx {
		t.Fatal("transaction ID mismatch")
	}
	if endpoint != "203.0.113.9:40000" {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
}

func TestParseBindingResponseRejectsURLsAsNonSTUN(t *testing.T) {
	t.Parallel()

	if _, _, ok := parseBindingResponse([]byte("https://example.com/api/v1")); ok {
		t.Fatal("expected non-STUN payload to be rejected")
	}
}

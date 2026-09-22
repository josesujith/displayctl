package display

import (
	"bytes"
	"errors"
	"testing"
)

func TestFrame(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    []byte
	}{
		// The Get VCP example from the DDC/CI spec: 6E 51 82 01 10 AC.
		{"get brightness", []byte{0x01, 0x10}, []byte{0x82, 0x01, 0x10, 0xAC}},
		{"set brightness 50", []byte{0x03, 0x10, 0x00, 0x32}, []byte{0x84, 0x03, 0x10, 0x00, 0x32, 0x9A}},
	}
	for _, tt := range tests {
		if got := frame(tt.payload...); !bytes.Equal(got, tt.want) {
			t.Errorf("%s: frame = % x, want % x", tt.name, got, tt.want)
		}
	}
}

func TestParseReply(t *testing.T) {
	ok := []byte{0x6E, 0x88, 0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x32, 0xF2}
	v, m, err := parseReply(Brightness, ok)
	if err != nil || v != 50 || m != 100 {
		t.Fatalf("parseReply = %d, %d, %v; want 50, 100, nil", v, m, err)
	}

	bad := bytes.Clone(ok)
	bad[9] = 0x33
	if _, _, err := parseReply(Brightness, bad); err == nil {
		t.Error("parseReply accepted a bad checksum")
	}

	if _, _, err := parseReply(Contrast, ok); err == nil {
		t.Error("parseReply accepted a reply for a different VCP code")
	}

	unsupported := []byte{0x6E, 0x88, 0x02, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0}
	for _, b := range unsupported[:10] {
		unsupported[10] ^= b
	}
	unsupported[10] ^= replyAddr
	if _, _, err := parseReply(Brightness, unsupported); !errors.Is(err, ErrUnsupported) {
		t.Errorf("parseReply = %v, want ErrUnsupported", err)
	}
}

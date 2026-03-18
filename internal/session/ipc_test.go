package session

import (
	"encoding/binary"
	"testing"
)

func TestHeaderMarshalRoundtrip(t *testing.T) {
	tests := []struct {
		tag Tag
		len uint32
	}{
		{TagInput, 0},
		{TagOutput, 1024},
		{TagInfo, 0},
		{TagKill, 0},
		{TagAck, 42},
	}
	for _, tt := range tests {
		h := Header{Tag: tt.tag, Len: tt.len}
		buf := h.Marshal()
		got, err := ParseHeader(buf[:])
		if err != nil {
			t.Fatalf("ParseHeader(%v): %v", tt.tag, err)
		}
		if got.Tag != tt.tag || got.Len != tt.len {
			t.Errorf("roundtrip %v: got {%v, %d}, want {%v, %d}", tt.tag, got.Tag, got.Len, tt.tag, tt.len)
		}
	}
}

func TestHeaderMarshalLayout(t *testing.T) {
	h := Header{Tag: TagInfo, Len: 0}
	buf := h.Marshal()
	want := [5]byte{0x06, 0x00, 0x00, 0x00, 0x00}
	if buf != want {
		t.Errorf("Info header bytes = %v, want %v", buf, want)
	}

	h2 := Header{Tag: TagKill, Len: 256}
	buf2 := h2.Marshal()
	if buf2[0] != 0x05 {
		t.Errorf("Kill tag byte = %d, want 5", buf2[0])
	}
	gotLen := binary.LittleEndian.Uint32(buf2[1:5])
	if gotLen != 256 {
		t.Errorf("Kill len = %d, want 256", gotLen)
	}
}

func TestParseHeaderTooShort(t *testing.T) {
	_, err := ParseHeader([]byte{0x00, 0x01})
	if err == nil {
		t.Error("expected error for short header")
	}
}

func TestParseInfoValidPayload(t *testing.T) {
	data := make([]byte, InfoSize)

	// clients_len = 3 at offset 0
	binary.LittleEndian.PutUint64(data[0:8], 3)
	// pid = 12345 at offset 8
	binary.LittleEndian.PutUint32(data[8:12], 12345)
	// cmd_len = 4 at offset 12
	binary.LittleEndian.PutUint16(data[12:14], 4)
	// cwd_len = 5 at offset 14
	binary.LittleEndian.PutUint16(data[14:16], 5)
	// cmd = "bash" at offset 16
	copy(data[16:], "bash")
	// cwd = "/home" at offset 272
	copy(data[272:], "/home")
	// created_at = 1000 at offset 528
	binary.LittleEndian.PutUint64(data[528:536], 1000)
	// task_ended_at = 2000 at offset 536
	binary.LittleEndian.PutUint64(data[536:544], 2000)
	// task_exit_code = 42 at offset 544
	data[544] = 42

	info, err := ParseInfo(data)
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	if info.ClientsLen != 3 {
		t.Errorf("ClientsLen = %d, want 3", info.ClientsLen)
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.CmdString() != "bash" {
		t.Errorf("CmdString = %q, want %q", info.CmdString(), "bash")
	}
	if info.CwdString() != "/home" {
		t.Errorf("CwdString = %q, want %q", info.CwdString(), "/home")
	}
	if info.CreatedAt != 1000 {
		t.Errorf("CreatedAt = %d, want 1000", info.CreatedAt)
	}
	if info.TaskEndedAt != 2000 {
		t.Errorf("TaskEndedAt = %d, want 2000", info.TaskEndedAt)
	}
	if info.TaskExitCode != 42 {
		t.Errorf("TaskExitCode = %d, want 42", info.TaskExitCode)
	}
}

func TestParseInfoTooShort(t *testing.T) {
	_, err := ParseInfo(make([]byte, 100))
	if err == nil {
		t.Error("expected error for short payload")
	}
}

func TestInfoCmdStringClamped(t *testing.T) {
	info := InfoPayload{CmdLen: 300} // exceeds MaxCmdLen
	copy(info.Cmd[:], "x]")
	got := info.CmdString()
	if len(got) != MaxCmdLen {
		t.Errorf("CmdString len = %d, want %d (clamped)", len(got), MaxCmdLen)
	}
}

func TestMarshalMessage(t *testing.T) {
	msg := MarshalMessage(TagKill, nil)
	if len(msg) != HeaderSize {
		t.Errorf("empty payload message len = %d, want %d", len(msg), HeaderSize)
	}

	payload := []byte("hello")
	msg2 := MarshalMessage(TagInput, payload)
	if len(msg2) != HeaderSize+len(payload) {
		t.Errorf("message len = %d, want %d", len(msg2), HeaderSize+len(payload))
	}
	// Verify payload follows header
	if string(msg2[HeaderSize:]) != "hello" {
		t.Errorf("payload = %q, want %q", msg2[HeaderSize:], "hello")
	}
}

func TestTagString(t *testing.T) {
	if TagInfo.String() != "Info" {
		t.Errorf("TagInfo.String() = %q, want %q", TagInfo.String(), "Info")
	}
	if Tag(99).String() != "Tag(99)" {
		t.Errorf("Tag(99).String() = %q, want %q", Tag(99).String(), "Tag(99)")
	}
}

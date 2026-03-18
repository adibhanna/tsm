package session

import (
	"encoding/binary"
	"fmt"
)

const (
	HeaderSize = 5
	MaxCmdLen  = 256
	MaxCwdLen  = 256

	// InfoSize is the size of the IPC Info extern struct on 64-bit platforms.
	// Layout: clients_len(8) + pid(4) + cmd_len(2) + cwd_len(2) +
	//         cmd(256) + cwd(256) + created_at(8) + task_ended_at(8) +
	//         task_exit_code(1) + padding(7) = 552
	InfoSize = 552
)

// Tag identifies the type of an IPC message.
type Tag uint8

const (
	TagInput     Tag = 0
	TagOutput    Tag = 1
	TagResize    Tag = 2
	TagDetach    Tag = 3
	TagDetachAll Tag = 4
	TagKill      Tag = 5
	TagInfo      Tag = 6
	TagInit      Tag = 7
	TagHistory   Tag = 8
	TagRun       Tag = 9
	TagAck       Tag = 10
)

func (t Tag) String() string {
	switch t {
	case TagInput:
		return "Input"
	case TagOutput:
		return "Output"
	case TagResize:
		return "Resize"
	case TagDetach:
		return "Detach"
	case TagDetachAll:
		return "DetachAll"
	case TagKill:
		return "Kill"
	case TagInfo:
		return "Info"
	case TagInit:
		return "Init"
	case TagHistory:
		return "History"
	case TagRun:
		return "Run"
	case TagAck:
		return "Ack"
	}
	return fmt.Sprintf("Tag(%d)", t)
}

// Header is the 5-byte IPC message header: 1 byte tag + 4 byte LE payload length.
type Header struct {
	Tag Tag
	Len uint32
}

// Marshal encodes the header into a 5-byte wire format.
func (h Header) Marshal() [HeaderSize]byte {
	var buf [HeaderSize]byte
	buf[0] = byte(h.Tag)
	binary.LittleEndian.PutUint32(buf[1:], h.Len)
	return buf
}

// ParseHeader decodes a 5-byte buffer into a Header.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("header too short: %d < %d", len(b), HeaderSize)
	}
	return Header{
		Tag: Tag(b[0]),
		Len: binary.LittleEndian.Uint32(b[1:5]),
	}, nil
}

// InfoPayload represents the IPC Info extern struct (C ABI layout, 552 bytes).
type InfoPayload struct {
	ClientsLen   uint64
	PID          int32
	CmdLen       uint16
	CwdLen       uint16
	Cmd          [MaxCmdLen]byte
	Cwd          [MaxCwdLen]byte
	CreatedAt    uint64
	TaskEndedAt  uint64
	TaskExitCode uint8
}

// ParseInfo decodes a 552-byte payload into an InfoPayload using exact C ABI offsets.
func ParseInfo(data []byte) (InfoPayload, error) {
	if len(data) < InfoSize {
		return InfoPayload{}, fmt.Errorf("info payload too short: %d < %d", len(data), InfoSize)
	}
	var info InfoPayload
	info.ClientsLen = binary.LittleEndian.Uint64(data[0:8])
	info.PID = int32(binary.LittleEndian.Uint32(data[8:12]))
	info.CmdLen = binary.LittleEndian.Uint16(data[12:14])
	info.CwdLen = binary.LittleEndian.Uint16(data[14:16])
	copy(info.Cmd[:], data[16:16+MaxCmdLen])
	copy(info.Cwd[:], data[272:272+MaxCwdLen])
	info.CreatedAt = binary.LittleEndian.Uint64(data[528:536])
	info.TaskEndedAt = binary.LittleEndian.Uint64(data[536:544])
	info.TaskExitCode = data[544]
	return info, nil
}

// CmdString returns the command as a Go string, clamped to MaxCmdLen.
func (info *InfoPayload) CmdString() string {
	n := int(info.CmdLen)
	if n > MaxCmdLen {
		n = MaxCmdLen
	}
	return string(info.Cmd[:n])
}

// CwdString returns the working directory as a Go string, clamped to MaxCwdLen.
func (info *InfoPayload) CwdString() string {
	n := int(info.CwdLen)
	if n > MaxCwdLen {
		n = MaxCwdLen
	}
	return string(info.Cwd[:n])
}

// MarshalMessage builds a complete wire message (header + payload).
func MarshalMessage(tag Tag, payload []byte) []byte {
	h := Header{Tag: tag, Len: uint32(len(payload))}
	buf := h.Marshal()
	msg := make([]byte, HeaderSize+len(payload))
	copy(msg, buf[:])
	copy(msg[HeaderSize:], payload)
	return msg
}

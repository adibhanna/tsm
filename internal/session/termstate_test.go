package session

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"testing"
)

func TestModeTrackerSnapshotTracksPrivateModes(t *testing.T) {
	tracker := newModeTracker()

	tracker.Consume([]byte("\x1b[?1049h\x1b[?1002;1006h\x1b[?2004h\x1b[?25l"))

	got := tracker.Snapshot()
	want := []byte("\x1b[?1049h\x1b[?1002h\x1b[?1006h\x1b[?2004h\x1b[?25l")
	if !bytes.Equal(got, want) {
		t.Fatalf("Snapshot() = %q, want %q", got, want)
	}
}

func TestModeTrackerSnapshotHandlesSplitEscapeSequence(t *testing.T) {
	tracker := newModeTracker()

	tracker.Consume([]byte("\x1b[?104"))
	tracker.Consume([]byte("9h\x1b[?1004h"))

	got := tracker.Snapshot()
	want := []byte("\x1b[?1049h\x1b[?1004h")
	if !bytes.Equal(got, want) {
		t.Fatalf("Snapshot() = %q, want %q", got, want)
	}
}

func TestBuildInfoCountsOnlyAttachedClients(t *testing.T) {
	attachedServer, attachedClient := net.Pipe()
	defer attachedServer.Close()
	defer attachedClient.Close()

	probeServer, probeClient := net.Pipe()
	defer probeServer.Close()
	defer probeClient.Close()

	d := &Daemon{
		cmd: &exec.Cmd{Process: &os.Process{Pid: 123}},
		clients: map[net.Conn]*clientState{
			attachedServer: &clientState{attached: true},
			probeServer:    &clientState{},
		},
	}

	info := d.buildInfo()
	if info.ClientsLen != 1 {
		t.Fatalf("ClientsLen = %d, want 1", info.ClientsLen)
	}
}

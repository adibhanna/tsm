package session

import "testing"

func TestWriteAndReadDaemonBuildInfo(t *testing.T) {
	cfg := Config{LogDir: t.TempDir()}
	if err := writeDaemonBuildInfo(cfg, "demo"); err != nil {
		t.Fatalf("writeDaemonBuildInfo: %v", err)
	}
	info, err := ReadDaemonBuildInfo(cfg, "demo")
	if err != nil {
		t.Fatalf("ReadDaemonBuildInfo: %v", err)
	}
	if info.Executable == "" || info.ModTimeUnix == 0 {
		t.Fatalf("info = %+v, want executable and mod time", info)
	}
	if err := RemoveDaemonBuildInfo(cfg, "demo"); err != nil {
		t.Fatalf("RemoveDaemonBuildInfo: %v", err)
	}
}

package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type DaemonBuildInfo struct {
	Executable  string `json:"executable"`
	ModTimeUnix int64  `json:"mod_time_unix"`
}

func daemonBuildDir(cfg Config) string {
	return filepath.Join(cfg.LogDir, "daemon-build")
}

func daemonBuildInfoPath(cfg Config, sessionName string) string {
	return filepath.Join(daemonBuildDir(cfg), sessionName+".json")
}

func writeDaemonBuildInfo(cfg Config, sessionName string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	data, err := json.Marshal(DaemonBuildInfo{
		Executable:  exe,
		ModTimeUnix: info.ModTime().Unix(),
	})
	if err != nil {
		return err
	}
	dir := daemonBuildDir(cfg)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := daemonBuildInfoPath(cfg, sessionName)
	tmp, err := os.CreateTemp(dir, sessionName+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ReadDaemonBuildInfo(cfg Config, sessionName string) (DaemonBuildInfo, error) {
	var info DaemonBuildInfo
	data, err := os.ReadFile(daemonBuildInfoPath(cfg, sessionName))
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return DaemonBuildInfo{}, err
	}
	return info, nil
}

func RemoveDaemonBuildInfo(cfg Config, sessionName string) error {
	err := os.Remove(daemonBuildInfoPath(cfg, sessionName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func CurrentBuildInfo() (DaemonBuildInfo, error) {
	exe, err := os.Executable()
	if err != nil {
		return DaemonBuildInfo{}, err
	}
	info, err := os.Stat(exe)
	if err != nil {
		return DaemonBuildInfo{}, err
	}
	return DaemonBuildInfo{
		Executable:  exe,
		ModTimeUnix: info.ModTime().Unix(),
	}, nil
}

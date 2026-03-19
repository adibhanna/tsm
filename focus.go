package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adibhanna/tsm/internal/session"
)

type focusState struct {
	Current  string `json:"current"`
	Previous string `json:"previous"`
}

var listSessionsForFocus = session.ListSessions

func focusStatePath(cfg session.Config) string {
	return filepath.Join(cfg.SocketDir, ".focus.json")
}

func loadFocusState(cfg session.Config) (focusState, error) {
	data, err := os.ReadFile(focusStatePath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return focusState{}, nil
		}
		return focusState{}, err
	}
	var state focusState
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return focusState{}, err
	}
	return state, nil
}

func saveFocusState(cfg session.Config, state focusState) error {
	if err := os.MkdirAll(cfg.SocketDir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(focusStatePath(cfg), data, 0o640)
}

func markSessionFocused(cfg session.Config, target, current string) error {
	if target == "" {
		return nil
	}
	state, err := loadFocusState(cfg)
	if err != nil {
		return err
	}
	prev := current
	if prev == "" {
		prev = state.Current
	}
	if prev != "" && prev != target {
		state.Previous = prev
	}
	state.Current = target
	return saveFocusState(cfg, state)
}

func removeFocusSession(cfg session.Config, name string) error {
	if name == "" {
		return nil
	}
	state, err := loadFocusState(cfg)
	if err != nil {
		return err
	}
	changed := false
	if state.Current == name {
		state.Current = state.Previous
		state.Previous = ""
		changed = true
	}
	if state.Previous == name {
		state.Previous = ""
		changed = true
	}
	if !changed {
		return nil
	}
	return saveFocusState(cfg, state)
}

func resolveToggleTarget(cfg session.Config, current string) (string, error) {
	state, err := loadFocusState(cfg)
	if err != nil {
		return "", err
	}
	live, err := liveSessionNames(cfg)
	if err != nil {
		return "", err
	}

	seen := map[string]bool{}
	for _, candidate := range toggleCandidates(state, current) {
		if candidate == "" || candidate == current || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if live[candidate] {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no previous session available")
}

func toggleCandidates(state focusState, current string) []string {
	if current == "" {
		return []string{state.Current, state.Previous}
	}
	if state.Current == current {
		return []string{state.Previous}
	}
	if state.Previous == current {
		return []string{state.Current}
	}
	return []string{state.Current, state.Previous}
}

func liveSessionNames(cfg session.Config) (map[string]bool, error) {
	sessions, err := listSessionsForFocus(cfg)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		live[s.Name] = true
	}
	return live, nil
}

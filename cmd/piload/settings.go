package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Settings struct {
	Host           string `json:"host"`
	Port           string `json:"port"`
	User           string `json:"user"`
	Auth           string `json:"auth"`
	KeyPath        string `json:"key_path"`
	OutputDir      string `json:"output_dir"`
	LocalDir       string `json:"local_dir"`
	Target         string `json:"target"`
	Quality        string `json:"quality"`
	Playlist       bool   `json:"playlist"`
	SavePassword   bool   `json:"save_password"`
	Password       string `json:"password"`
	AutoUpdate     bool   `json:"auto_update"`
	DismissedYTDLP string `json:"dismissed_ytdlp"`
	DismissedDeno  string `json:"dismissed_deno"`
}

func settingsPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".piload")
	}
	return filepath.Join(base, "PiLoad", "settings.json")
}

func defaultSettings() Settings {
	return Settings{
		Host:       "192.168.1.42",
		Port:       "22",
		User:       "dietpi",
		Auth:       "password",
		OutputDir:  "/mnt/dietpi_userdata/downloads",
		LocalDir:   defaultLocalDir(),
		Target:     "remote",
		Quality:    "best",
		AutoUpdate: true,
	}
}

func loadSettings() Settings {
	cfg := defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if _, ok := raw["auto_update"]; !ok {
			cfg.AutoUpdate = true
		}
	}
	if cfg.Target != "local" {
		cfg.Target = "remote"
	}
	if strings.TrimSpace(cfg.LocalDir) == "" {
		cfg.LocalDir = defaultLocalDir()
	}
	if _, ok := presets[cfg.Quality]; !ok {
		cfg.Quality = "best"
	}
	return cfg
}

func saveSettings(cfg Settings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func defaultLocalDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "Videos")
	case "darwin":
		return filepath.Join(home, "Movies")
	default:
		if p := xdgVideos(home); p != "" {
			return p
		}
		return filepath.Join(home, "Videos")
	}
}

func xdgVideos(home string) string {
	data, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "XDG_VIDEOS_DIR=") {
			continue
		}
		val := strings.Trim(strings.TrimPrefix(line, "XDG_VIDEOS_DIR="), `"`)
		val = strings.ReplaceAll(val, "$HOME", home)
		val = strings.ReplaceAll(val, "${HOME}", home)
		return val
	}
	return ""
}

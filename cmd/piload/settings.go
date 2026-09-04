package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	Host         string `json:"host"`
	Port         string `json:"port"`
	User         string `json:"user"`
	Auth         string `json:"auth"`
	KeyPath      string `json:"key_path"`
	OutputDir    string `json:"output_dir"`
	Quality      string `json:"quality"`
	Playlist     bool   `json:"playlist"`
	SavePassword bool   `json:"save_password"`
	Password     string `json:"password"`
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
		Host:      "192.168.1.42",
		Port:      "22",
		User:      "dietpi",
		Auth:      "password",
		OutputDir: "/mnt/dietpi_userdata/downloads",
		Quality:   "best",
	}
}

func loadSettings() Settings {
	cfg := defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
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

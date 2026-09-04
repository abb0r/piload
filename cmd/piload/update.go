package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const repoAPI = "https://api.github.com/repos/abb0r/piload/releases/latest"

type ghRelease struct {
	Tag  string `json:"tag_name"`
	Body string `json:"body"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func latestAppRelease() (tag, exeURL, notes string, err error) {
	req, err := http.NewRequest(http.MethodGet, repoAPI, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode >= 400 {
		return "", "", "", fmt.Errorf("GitHub API %s", resp.Status)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", "", err
	}
	tag = strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
	notes = strings.TrimSpace(rel.Body)
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "PiLoad.exe") {
			return tag, a.URL, notes, nil
		}
	}
	return tag, "", notes, fmt.Errorf("no PiLoad.exe in latest release")
}

func applyUpdate(exeURL string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		self, _ = os.Executable()
	}
	dir := filepath.Dir(self)
	tmp := filepath.Join(dir, "PiLoad.exe.new")
	req, err := http.NewRequest(http.MethodGet, exeURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	bat := filepath.Join(dir, "piload-update.bat")
	script := fmt.Sprintf("ping 127.0.0.1 -n 2 >nul\r\nmove /y \"%s\" \"%s\"\r\nstart \"\" \"%s\"\r\ndel \"%%~f0\"\r\n", tmp, self, self)
	if err := os.WriteFile(bat, []byte(script), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/C", "start", "", bat)
	cmd.Dir = dir
	return cmd.Start()
}

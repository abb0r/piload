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

func cleanupOldBinary() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(self)
	_ = os.Remove(self + ".old")
	_ = os.Remove(filepath.Join(dir, "PiLoad.exe.old"))
	_ = os.Remove(filepath.Join(dir, "PiLoad.exe.new"))
	_ = os.Remove(filepath.Join(dir, "piload-update.bat"))
}

type progressReader struct {
	r     io.Reader
	got   int64
	total int64
	last  time.Time
	cb    func(got, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.got += int64(n)
		if p.cb != nil && (err != nil || time.Since(p.last) > 80*time.Millisecond) {
			p.last = time.Now()
			p.cb(p.got, p.total)
		}
	}
	if err != nil && p.cb != nil {
		p.cb(p.got, p.total)
	}
	return n, err
}

func applyUpdate(exeURL string, progress func(got, total int64)) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Dir(self)
	tmp := filepath.Join(dir, "PiLoad.exe.new")
	old := self + ".old"

	req, err := http.NewRequest(http.MethodGet, exeURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
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
	src := &progressReader{r: resp.Body, total: resp.ContentLength, cb: progress}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}

	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("could not replace running app: %w", err)
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Rename(old, self)
		return fmt.Errorf("could not install update: %w", err)
	}

	cmd := exec.Command(self)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update installed but restart failed: %w", err)
	}
	return nil
}


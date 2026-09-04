package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	repoAPI      = "https://api.github.com/repos/abb0r/piload/releases/latest"
	flatpakAppID = "com.abb0r.PiLoad"
)

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
	want := releaseAssetName()
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, want) {
			return tag, a.URL, notes, nil
		}
	}
	return tag, "", notes, fmt.Errorf("no %s in latest release", want)
}

func releaseAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "PiLoad.exe"
	case "darwin":
		return "PiLoad-macos-arm64.dmg"
	default:
		return "PiLoad-linux-x86_64.flatpak"
	}
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

func applyUpdate(fileURL string, progress func(got, total int64)) error {
	tmp, err := downloadUpdate(fileURL, progress)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return applyWindowsUpdate(tmp)
	case "darwin":
		return applyDarwinDMG(tmp)
	default:
		return applyLinuxFlatpak(tmp)
	}
}

func downloadUpdate(fileURL string, progress func(got, total int64)) (string, error) {
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	dir := os.TempDir()
	if home, err := os.UserHomeDir(); err == nil {
		dl := filepath.Join(home, "Downloads")
		if st, err := os.Stat(dl); err == nil && st.IsDir() {
			dir = dl
		}
	}
	tmp := filepath.Join(dir, releaseAssetName())
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	src := &progressReader{r: resp.Body, total: resp.ContentLength, cb: progress}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	return tmp, nil
}

func applyWindowsUpdate(tmp string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Dir(self)
	old := self + ".old"
	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return fmt.Errorf("could not replace running app: %w", err)
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = copyFile(tmp, self)
		if _, statErr := os.Stat(self); statErr != nil {
			_ = os.Rename(old, self)
			return fmt.Errorf("could not install update: %w", err)
		}
	}
	cmd := exec.Command(self)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("update installed but restart failed: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func darwinBundlePath(self string) string {
	const marker = ".app/Contents/MacOS/"
	i := strings.Index(self, marker)
	if i < 0 {
		return ""
	}
	return self[:i+4]
}

func applyDarwinDMG(dmg string) error {
	out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", dmg).CombinedOutput()
	if err != nil {
		_ = exec.Command("open", dmg).Start()
		return fmt.Errorf("could not open disk image: %s", strings.TrimSpace(string(out)))
	}
	mount := ""
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "/Volumes/"); i >= 0 {
			mount = strings.TrimSpace(line[i:])
			break
		}
	}
	if mount == "" {
		_ = exec.Command("open", dmg).Start()
		return fmt.Errorf("disk image attached but no volume found")
	}
	defer func() { _ = exec.Command("hdiutil", "detach", mount, "-quiet").Run() }()

	matches, _ := filepath.Glob(filepath.Join(mount, "*.app"))
	if len(matches) == 0 {
		_ = exec.Command("open", dmg).Start()
		return fmt.Errorf("no .app found in the disk image")
	}
	srcApp := matches[0]

	self, err := os.Executable()
	if err != nil {
		return err
	}
	dstApp := darwinBundlePath(self)
	if dstApp == "" {
		home, _ := os.UserHomeDir()
		dstApp = filepath.Join(home, "Applications", "PiLoad.app")
	}
	_ = os.MkdirAll(filepath.Dir(dstApp), 0o755)
	if err := exec.Command("ditto", srcApp, dstApp).Run(); err != nil {
		return fmt.Errorf("could not install PiLoad.app: %w", err)
	}
	cmd := exec.Command("open", dstApp)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("app installed but restart failed: %w", err)
	}
	return nil
}

func hostFlatpak(args ...string) *exec.Cmd {
	if os.Getenv("FLATPAK_ID") != "" {
		all := append([]string{"--host", "flatpak"}, args...)
		return exec.Command("flatpak-spawn", all...)
	}
	return exec.Command("flatpak", args...)
}

func applyLinuxFlatpak(bundle string) error {
	cmd := hostFlatpak("install", "--user", "--or-update", "--noninteractive", bundle)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("flatpak install failed: %s\n%s", err, strings.TrimSpace(string(out)))
	}
	run := hostFlatpak("run", flatpakAppID)
	run.Stdin, run.Stdout, run.Stderr = nil, nil, nil
	if err := run.Start(); err != nil {
		return fmt.Errorf("flatpak installed but restart failed: %w", err)
	}
	return nil
}

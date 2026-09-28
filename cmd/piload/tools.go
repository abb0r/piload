package main

import (
	"archive/tar"
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

// minDeno is the oldest Deno release yt-dlp still accepts for YouTube.
const minDeno = "2.3.0"

var toolMu sync.Mutex

type toolProgress func(label string, got, total int64)

type updatePlan struct {
	appTag, appURL, appNotes string
	ytdlpTo                  string
	ytdlpNotes               string
	denoTo                   string
	withFFmpeg               bool
	summary                  string
}

func (p updatePlan) hasTools() bool {
	return p.ytdlpTo != "" || p.denoTo != "" || p.withFFmpeg
}

func (p updatePlan) empty() bool {
	return p.appURL == "" && !p.hasTools()
}

func toolsDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		return filepath.Join(filepath.Dir(exe), "bin"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "PiLoad", "bin"), nil
	default:
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			return filepath.Join(base, "piload", "bin"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "piload", "bin"), nil
	}
}

func toolFile(name string) string {
	dir, err := toolsDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, name)
}

func ytdlpFileName() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

func ffmpegFileName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func ffprobeFileName() string {
	if runtime.GOOS == "windows" {
		return "ffprobe.exe"
	}
	return "ffprobe"
}

func denoFileName() string {
	if runtime.GOOS == "windows" {
		return "deno.exe"
	}
	return "deno"
}

func ytdlpPath() string  { return toolFile(ytdlpFileName()) }
func ffmpegPath() string { return toolFile(ffmpegFileName()) }
func ffprobePath() string {
	return toolFile(ffprobeFileName())
}
func denoPath() string { return toolFile(denoFileName()) }

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}

func cmdVersion(bin string, args ...string) (string, error) {
	if bin == "" {
		return "", fmt.Errorf("missing program")
	}
	cmd := exec.Command(bin, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%s", text)
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("no version from %s", filepath.Base(bin))
	}
	if strings.EqualFold(fields[0], "deno") && len(fields) > 1 {
		return fields[1], nil
	}
	if strings.EqualFold(fields[0], "ffmpeg") || strings.EqualFold(fields[0], "ffprobe") {
		if len(fields) > 2 {
			return fields[2], nil
		}
	}
	return fields[0], nil
}

func toolsReady() bool {
	if _, err := cmdVersion(ytdlpPath(), "--version"); err != nil {
		return false
	}
	if _, err := cmdVersion(ffmpegPath(), "-version"); err != nil {
		return false
	}
	if _, err := cmdVersion(ffprobePath(), "-version"); err != nil {
		return false
	}
	if _, err := cmdVersion(denoPath(), "--version"); err != nil {
		return false
	}
	return true
}

func localToolsStatus() string {
	dir, err := toolsDir()
	if err != nil {
		return "Local tools: " + err.Error()
	}
	if !fileExists(ytdlpPath()) && !fileExists(ffmpegPath()) && !fileExists(denoPath()) {
		return "Local tools: not installed yet. They download on the first local job.\n" + dir
	}
	y := "missing"
	if v, err := cmdVersion(ytdlpPath(), "--version"); err == nil {
		y = v
	}
	ff := "missing"
	if v, err := cmdVersion(ffmpegPath(), "-version"); err == nil {
		ff = v
	}
	d := "missing"
	if v, err := cmdVersion(denoPath(), "--version"); err == nil {
		d = v
	}
	return fmt.Sprintf("Local tools: yt-dlp %s, ffmpeg %s, deno %s\n%s", y, ff, d, dir)
}

func githubRelease(repo string) (ghRelease, error) {
	var rel ghRelease
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return rel, err
	}
	if resp.StatusCode >= 400 {
		return rel, fmt.Errorf("GitHub API %s", resp.Status)
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return rel, err
	}
	return rel, nil
}

func assetURL(rel ghRelease, exact string, match func(string) bool) (string, error) {
	for _, a := range rel.Assets {
		if exact != "" && strings.EqualFold(a.Name, exact) {
			return a.URL, nil
		}
		if exact == "" && match != nil && match(a.Name) {
			return a.URL, nil
		}
	}
	if exact != "" {
		return "", fmt.Errorf("asset %s not in release", exact)
	}
	return "", fmt.Errorf("matching asset not in release")
}

func downloadURL(fileURL, dest string, progress func(got, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	partial := dest + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	src := &progressReader{r: resp.Body, total: resp.ContentLength, cb: progress}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return closeErr
	}
	if err := os.Chmod(partial, 0o755); err != nil && runtime.GOOS != "windows" {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, dest); err != nil {
		if copyFile(partial, dest) != nil {
			_ = os.Remove(partial)
			return err
		}
		_ = os.Remove(partial)
		_ = os.Chmod(dest, 0o755)
	}
	return nil
}

func ytdlpAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "yt-dlp.exe"
	case "darwin":
		return "yt-dlp_macos"
	default:
		return "yt-dlp_linux"
	}
}

func denoAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "deno-x86_64-pc-windows-msvc.zip"
	case "darwin":
		return "deno-aarch64-apple-darwin.zip"
	default:
		return "deno-x86_64-unknown-linux-gnu.zip"
	}
}

func installYTDLP(progress toolProgress) error {
	rel, err := githubRelease("yt-dlp/yt-dlp")
	if err != nil {
		return err
	}
	url, err := assetURL(rel, ytdlpAssetName(), nil)
	if err != nil {
		return err
	}
	dir, err := toolsDir()
	if err != nil {
		return err
	}
	stage := filepath.Join(dir, "new-"+ytdlpFileName())
	if err := downloadURL(url, stage, func(got, total int64) {
		if progress != nil {
			progress("Downloading yt-dlp…", got, total)
		}
	}); err != nil {
		return err
	}
	if _, err := cmdVersion(stage, "--version"); err != nil {
		_ = os.Remove(stage)
		return fmt.Errorf("downloaded yt-dlp does not run: %w", err)
	}
	return commitFiles(map[string]string{ytdlpPath(): stage}, ytdlpFileName())
}

func installDeno(progress toolProgress) error {
	rel, err := githubRelease("denoland/deno")
	if err != nil {
		return err
	}
	url, err := assetURL(rel, denoAssetName(), nil)
	if err != nil {
		return err
	}
	archive := filepath.Join(os.TempDir(), denoAssetName())
	if err := downloadURL(url, archive, func(got, total int64) {
		if progress != nil {
			progress("Downloading Deno…", got, total)
		}
	}); err != nil {
		return err
	}
	defer os.Remove(archive)
	stage, err := os.MkdirTemp("", "piload-deno")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extractMatching(archive, "zip", stage, func(base string) bool {
		return base == "deno" || base == "deno.exe"
	}); err != nil {
		return err
	}
	src := filepath.Join(stage, denoFileName())
	if _, err := cmdVersion(src, "--version"); err != nil {
		return fmt.Errorf("downloaded Deno does not run: %w", err)
	}
	return commitFiles(map[string]string{denoPath(): src}, denoFileName())
}

func installFFmpeg(progress toolProgress) error {
	repo := "yt-dlp/FFmpeg-Builds"
	asset := ""
	kind := "zip"
	var match func(string) bool
	switch runtime.GOOS {
	case "windows":
		asset = "ffmpeg-master-latest-win64-gpl.zip"
	case "darwin":
		// yt-dlp/FFmpeg-Builds has no macOS binaries.
		repo = "Nothing-Software/FFmpeg-Builds"
		match = func(name string) bool {
			n := strings.ToLower(name)
			return strings.HasPrefix(n, "ffmpeg") && strings.Contains(n, "macos-arm64") && strings.HasSuffix(n, ".zip")
		}
	default:
		asset = "ffmpeg-master-latest-linux64-gpl.tar.xz"
		kind = "tarxz"
	}
	rel, err := githubRelease(repo)
	if err != nil {
		return err
	}
	url, err := assetURL(rel, asset, match)
	if err != nil {
		return err
	}
	archive := filepath.Join(os.TempDir(), "piload-ffmpeg.archive")
	if err := downloadURL(url, archive, func(got, total int64) {
		if progress != nil {
			progress("Downloading FFmpeg…", got, total)
		}
	}); err != nil {
		return err
	}
	defer os.Remove(archive)
	if progress != nil {
		progress("Installing FFmpeg…", 1, 1)
	}
	stage, err := os.MkdirTemp("", "piload-ffmpeg")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extractMatching(archive, kind, stage, wantFFmpegFile); err != nil {
		return err
	}
	if _, err := cmdVersion(filepath.Join(stage, ffmpegFileName()), "-version"); err != nil {
		return fmt.Errorf("downloaded ffmpeg does not run: %w", err)
	}
	if _, err := cmdVersion(filepath.Join(stage, ffprobeFileName()), "-version"); err != nil {
		return fmt.Errorf("downloaded ffprobe does not run: %w", err)
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	dir, err := toolsDir()
	if err != nil {
		return err
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files[filepath.Join(dir, e.Name())] = filepath.Join(stage, e.Name())
	}
	if err := commitFiles(files, ffmpegFileName()); err != nil {
		return err
	}
	if _, err := cmdVersion(ffmpegPath(), "-version"); err != nil {
		return fmt.Errorf("ffmpeg does not run from the tool folder: %w", err)
	}
	return nil
}

func wantFFmpegFile(base string) bool {
	switch strings.ToLower(base) {
	case "ffmpeg", "ffmpeg.exe", "ffprobe", "ffprobe.exe":
		return true
	}
	return strings.HasSuffix(strings.ToLower(base), ".dylib")
}

func extractMatching(archive, kind, dest string, keep func(base string) bool) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	switch kind {
	case "zip":
		return extractZip(archive, dest, keep)
	case "tarxz":
		return extractTarXZ(archive, dest, keep)
	default:
		return fmt.Errorf("unknown archive type %s", kind)
	}
}

func extractZip(archive, dest string, keep func(base string) bool) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	n := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := path.Base(strings.ReplaceAll(f.Name, "\\", "/"))
		if base == "." || base == "/" || !keep(base) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeNewFile(filepath.Join(dest, base), rc, f.FileInfo().Mode())
		rc.Close()
		if err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("archive did not contain the expected files")
	}
	return nil
}

func extractTarXZ(archive, dest string, keep func(base string) bool) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	xr, err := xz.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	n := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		base := path.Base(strings.ReplaceAll(hdr.Name, "\\", "/"))
		if base == "." || !keep(base) {
			continue
		}
		if err := writeNewFile(filepath.Join(dest, base), io.LimitReader(tr, hdr.Size), os.FileMode(hdr.Mode)); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("archive did not contain the expected files")
	}
	return nil
}

func writeNewFile(dest string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if mode == 0 {
		mode = 0o755
	}
	return os.Chmod(dest, mode|0o111)
}

type placedFile struct {
	dst    string
	hadBak bool
}

func commitFiles(files map[string]string, verifyName string) error {
	var placed []placedFile
	ok := false
	defer func() {
		if ok {
			for _, p := range placed {
				if p.hadBak {
					_ = os.Remove(p.dst + ".bak")
				}
			}
			return
		}
		for _, p := range placed {
			_ = os.Remove(p.dst)
			if p.hadBak {
				_ = os.Rename(p.dst+".bak", p.dst)
			}
		}
	}()
	for dst, src := range files {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		bak := dst + ".bak"
		_ = os.Remove(bak)
		had := false
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, bak); err != nil {
				return err
			}
			had = true
		}
		if err := os.Rename(src, dst); err != nil {
			if copyFile(src, dst) != nil {
				if had {
					_ = os.Rename(bak, dst)
				}
				return err
			}
			_ = os.Remove(src)
		}
		_ = os.Chmod(dst, 0o755)
		placed = append(placed, placedFile{dst: dst, hadBak: had})
	}
	if verifyName != "" {
		bin := ""
		for dst := range files {
			if filepath.Base(dst) == verifyName {
				bin = dst
				break
			}
		}
		if bin == "" {
			return fmt.Errorf("%s missing after install", verifyName)
		}
		args := []string{"--version"}
		if strings.HasPrefix(verifyName, "ffmpeg") || strings.HasPrefix(verifyName, "ffprobe") {
			args = []string{"-version"}
		}
		if _, err := cmdVersion(bin, args...); err != nil {
			return fmt.Errorf("%s does not run: %w", verifyName, err)
		}
	}
	ok = true
	return nil
}

func ensureLocalTools(progress toolProgress) error {
	toolMu.Lock()
	defer toolMu.Unlock()
	dir, err := toolsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := cmdVersion(ytdlpPath(), "--version"); err != nil {
		if err := installYTDLP(progress); err != nil {
			return err
		}
	}
	if _, err := cmdVersion(ffmpegPath(), "-version"); err != nil {
		if err := installFFmpeg(progress); err != nil {
			return err
		}
	} else if _, err := cmdVersion(ffprobePath(), "-version"); err != nil {
		if err := installFFmpeg(progress); err != nil {
			return err
		}
	}
	if _, err := cmdVersion(denoPath(), "--version"); err != nil {
		if err := installDeno(progress); err != nil {
			return err
		}
	}
	return nil
}

func applyToolPlan(p updatePlan, progress toolProgress) error {
	toolMu.Lock()
	defer toolMu.Unlock()
	dir, err := toolsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if p.withFFmpeg {
		if err := installFFmpeg(progress); err != nil {
			return err
		}
	}
	if p.denoTo != "" {
		if err := installDeno(progress); err != nil {
			return err
		}
	}
	if p.ytdlpTo != "" {
		if err := installYTDLP(progress); err != nil {
			return err
		}
	}
	return nil
}

func planToolUpdate(dismissedYT, dismissedDeno string) updatePlan {
	toolMu.Lock()
	defer toolMu.Unlock()
	var p updatePlan
	var lines []string
	if fileExists(ytdlpPath()) {
		cur, _ := cmdVersion(ytdlpPath(), "--version")
		if cur == "" {
			cur = "unknown"
		}
		rel, err := githubRelease("yt-dlp/yt-dlp")
		if err == nil {
			latest := strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
			if latest != "" && versionLess(cur, latest) && latest != dismissedYT {
				p.ytdlpTo = latest
				p.withFFmpeg = true
				p.ytdlpNotes = trimRunes(rel.Body, 700)
				lines = append(lines, fmt.Sprintf("yt-dlp %s → %s", cur, latest))
			}
		}
	}
	if fileExists(denoPath()) {
		cur, err := cmdVersion(denoPath(), "--version")
		tooOld := err != nil || versionLess(cur, minDeno)
		if tooOld {
			rel, err2 := githubRelease("denoland/deno")
			if err2 == nil {
				latest := strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
				if latest != "" && latest != dismissedDeno {
					p.denoTo = latest
					p.withFFmpeg = true
					if cur == "" {
						cur = "unknown"
					}
					lines = append(lines, fmt.Sprintf("Deno %s is below %s and will be updated to %s.", cur, minDeno, latest))
				}
			}
		}
	}
	if p.withFFmpeg {
		lines = append(lines, "FFmpeg will be updated at the same time.")
	}
	if len(lines) == 0 {
		return updatePlan{}
	}
	p.summary = "Local tools\n" + strings.Join(lines, "\n")
	if p.ytdlpNotes != "" {
		p.summary += "\n\nyt-dlp changes\n" + p.ytdlpNotes
	}
	return p
}

func trimRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

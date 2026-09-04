package main

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var presets = map[string][]string{
	"best": {
		"-f", "bv*+ba/b",
		"-S", "res,vcodec:h264,acodec:m4a,ext:mp4:m4a",
		"--merge-output-format", "mp4",
	},
	"1080": {
		"-f", "bv*[height<=1080]+ba/b[height<=1080]/b",
		"-S", "res:1080,vcodec:h264,acodec:m4a,ext:mp4:m4a",
		"--merge-output-format", "mp4",
	},
	"720": {
		"-f", "bv*[height<=720]+ba/b[height<=720]/b",
		"-S", "res:720,vcodec:h264,acodec:m4a,ext:mp4:m4a",
		"--merge-output-format", "mp4",
	},
	"audio": {"-f", "ba/b", "-x", "--audio-format", "mp3", "--audio-quality", "0"},
}

var commonArgs = []string{
	"--embed-metadata", "--embed-chapters", "--embed-subs",
	"--sub-langs", "en.*,de.*,-live_chat", "--write-auto-subs",
	"--sponsorblock-mark", "all", "--concurrent-fragments", "4",
	"--no-mtime", "--restrict-filenames", "--newline", "--no-warnings",
}

type qualityProfile struct {
	Key, Label, Tip string
}

var qualityProfiles = []qualityProfile{
	{"best", "Best Quality", "Highest available video and audio, merged to MP4.\nPrefers H.264 + AAC so the file plays widely without recoding."},
	{"1080", "1080p", "Best video up to 1080p plus best audio, merged to MP4.\nPrefers H.264 + AAC. Higher resolutions are ignored."},
	{"720", "720p", "Best video up to 720p plus best audio, merged to MP4.\nPrefers H.264 + AAC. Higher resolutions are ignored."},
	{"audio", "Audio only", "Best audio track only, converted to MP3 at maximum quality.\nNo video is downloaded."},
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func buildCommand(url, quality, outputDir string, playlist bool) string {
	args := []string{"yt-dlp"}
	if extra, ok := presets[quality]; ok {
		args = append(args, extra...)
	} else {
		args = append(args, presets["best"]...)
	}
	args = append(args, commonArgs...)
	out := strings.TrimRight(outputDir, "/") + "/%(title)s [%(id)s].%(ext)s"
	args = append(args, "-o", out)
	if !playlist {
		args = append(args, "--no-playlist")
	}
	args = append(args, url)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shQuote(a)
	}
	return strings.Join(quoted, " ")
}

func versionKey(value string) []int {
	re := regexp.MustCompile(`\d+`)
	parts := re.FindAllString(value, -1)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	if len(out) == 0 {
		return []int{0}
	}
	return out
}

func versionLess(a, b string) bool {
	ka, kb := versionKey(a), versionKey(b)
	n := len(ka)
	if len(kb) > n {
		n = len(kb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(ka) {
			va = ka[i]
		}
		if i < len(kb) {
			vb = kb[i]
		}
		if va < vb {
			return true
		}
		if va > vb {
			return false
		}
	}
	return false
}

func latestYTDLP() (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "PiLoad/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var data struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(data.Tag)
	return strings.TrimPrefix(tag, "v"), nil
}

package main

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed icon.png
var iconPNG []byte

// Version is set at build time with -X main.Version=0.2.0
var Version = "0.2.0"

const repoURL = "https://github.com/abb0r/piload"

var progressRE = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)

type job struct {
	ID, URL, Status string
	Progress        int
	Log             []string
}

type ui struct {
	win                                           fyne.Window
	status, notice, profileTip, queue             *widget.Label
	urls                                          *widget.Entry
	host, port, user, keyPath, password, outputDir *widget.Entry
	savePW, playlist                              *widget.Check
	qualityBtns                                   map[string]*widget.Button
	tabs                                          *container.AppTabs
	tabQueue                                      *container.TabItem
	tabSetup                                      *container.TabItem
	quality                                       string
	jobs                                          []*job
	ytdlpChecked                                  bool
}

func main() {
	a := app.NewWithID("com.abb0r.piload")
	a.Settings().SetTheme(theme.DarkTheme())
	if res := fyne.NewStaticResource("icon.png", iconPNG); res != nil {
		a.SetIcon(res)
	}
	w := a.NewWindow("PiLoad")
	w.Resize(fyne.NewSize(980, 720))
	w.SetMaster()

	u := &ui{win: w, quality: "best", qualityBtns: map[string]*widget.Button{}}
	cfg := loadSettings()
	u.quality = cfg.Quality
	u.build(cfg)
	w.SetContent(u.layout())
	go u.checkAppUpdate()
	w.ShowAndRun()
}

func (u *ui) build(cfg Settings) {
	u.status = widget.NewLabel("SSH not tested")
	u.notice = widget.NewLabel("")
	u.profileTip = widget.NewLabel("")
	u.profileTip.Wrapping = fyne.TextWrapWord
	u.queue = widget.NewLabel("No jobs yet.\nProgress appears here once a download is running over SSH.")
	u.queue.Wrapping = fyne.TextWrapWord

	u.urls = widget.NewMultiLineEntry()
	u.urls.SetPlaceHolder("https://www.youtube.com/watch?v=…")
	u.urls.Wrapping = fyne.TextWrapWord

	u.outputDir = widget.NewEntry()
	u.outputDir.SetText(cfg.OutputDir)
	u.playlist = widget.NewCheck("Download entire playlist", nil)
	u.playlist.SetChecked(cfg.Playlist)

	u.host = widget.NewEntry()
	u.host.SetText(cfg.Host)
	u.port = widget.NewEntry()
	u.port.SetText(cfg.Port)
	u.user = widget.NewEntry()
	u.user.SetText(cfg.User)
	u.keyPath = widget.NewEntry()
	u.keyPath.SetText(cfg.KeyPath)
	u.password = widget.NewPasswordEntry()
	if cfg.SavePassword {
		u.password.SetText(cfg.Password)
	}
	u.savePW = widget.NewCheck("Save SSH password", nil)
	u.savePW.SetChecked(cfg.SavePassword)
	u.setProfileTip()
}

func (u *ui) layout() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("PiLoad", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ver := widget.NewLabel(Version)
	logo := canvas.NewImageFromResource(fyne.NewStaticResource("icon.png", iconPNG))
	logo.SetMinSize(fyne.NewSize(36, 36))
	logo.FillMode = canvas.ImageFillContain
	header := container.NewBorder(nil, nil, container.NewHBox(logo, title, ver), nil)

	u.tabs = container.NewAppTabs(
		container.NewTabItem("Download", u.downloadTab()),
		container.NewTabItem("Queue", container.NewPadded(container.NewVScroll(u.queue))),
		container.NewTabItem("Setup", u.setupTab()),
	)
	u.tabQueue = u.tabs.Items[1]
	u.tabSetup = u.tabs.Items[2]
	return container.NewBorder(container.NewVBox(header, u.status), nil, nil, nil, u.tabs)
}

func (u *ui) downloadTab() fyne.CanvasObject {
	row := container.NewHBox()
	for _, p := range qualityProfiles {
		p := p
		btn := widget.NewButton(p.Label, func() {
			u.quality = p.Key
			u.refreshQuality()
		})
		u.qualityBtns[p.Key] = btn
		row.Add(btn)
	}
	u.refreshQuality()
	goBtn := widget.NewButton("Download via SSH", u.startDownload)
	goBtn.Importance = widget.HighImportance
	return container.NewPadded(container.NewVBox(
		widget.NewLabel("Video URLs (one per line)"),
		container.NewGridWrap(fyne.NewSize(900, 120), u.urls),
		row,
		u.profileTip,
		widget.NewLabel("Folder on the Pi"),
		u.outputDir,
		u.playlist,
		u.notice,
		container.NewBorder(nil, nil, nil, goBtn),
	))
}

func (u *ui) setupTab() fyne.CanvasObject {
	form := widget.NewForm(
		widget.NewFormItem("Host / IP", u.host),
		widget.NewFormItem("SSH port", u.port),
		widget.NewFormItem("User", u.user),
		widget.NewFormItem("Key file (optional)", u.keyPath),
		widget.NewFormItem("Password", u.password),
	)
	test := widget.NewButton("Test connection", u.testConnection)
	save := widget.NewButton("Save settings", u.persist)
	link, _ := url.Parse(repoURL)
	hyper := widget.NewHyperlink(strings.TrimPrefix(repoURL, "https://"), link)
	return container.NewPadded(container.NewVBox(
		form,
		u.savePW,
		container.NewHBox(test, save),
		widget.NewLabel("Version "+Version),
		hyper,
	))
}

func (u *ui) refreshQuality() {
	u.setProfileTip()
	for key, btn := range u.qualityBtns {
		if key == u.quality {
			btn.Importance = widget.HighImportance
		} else {
			btn.Importance = widget.MediumImportance
		}
		btn.Refresh()
	}
}

func (u *ui) setProfileTip() {
	for _, p := range qualityProfiles {
		if p.Key == u.quality {
			u.profileTip.SetText(p.Tip)
			return
		}
	}
	u.profileTip.SetText("")
}

func (u *ui) cfg() sshCfg {
	auth := "password"
	if strings.TrimSpace(u.keyPath.Text) != "" {
		auth = "key"
	}
	return sshCfg{
		Host:     strings.TrimSpace(u.host.Text),
		Port:     strings.TrimSpace(u.port.Text),
		User:     strings.TrimSpace(u.user.Text),
		Auth:     auth,
		KeyPath:  strings.TrimSpace(u.keyPath.Text),
		Password: u.password.Text,
	}
}

func (u *ui) persist() {
	cfg := Settings{
		Host:         strings.TrimSpace(u.host.Text),
		Port:         strings.TrimSpace(u.port.Text),
		User:         strings.TrimSpace(u.user.Text),
		Auth:         u.cfg().Auth,
		KeyPath:      strings.TrimSpace(u.keyPath.Text),
		OutputDir:    strings.TrimSpace(u.outputDir.Text),
		Quality:      u.quality,
		Playlist:     u.playlist.Checked,
		SavePassword: u.savePW.Checked,
	}
	if cfg.Port == "" {
		cfg.Port = "22"
	}
	if u.savePW.Checked {
		cfg.Password = u.password.Text
	}
	if err := saveSettings(cfg); err != nil {
		u.status.SetText("error: " + err.Error())
		return
	}
	u.status.SetText("Settings saved")
}

func (u *ui) testConnection() {
	u.status.SetText("Testing SSH…")
	cfg := u.cfg()
	go func() {
		out, errOut, code, err := sshRun(cfg, "hostname; yt-dlp --version", 20*time.Second)
		fyne.Do(func() {
			if err != nil {
				u.status.SetText("error: " + err.Error())
				return
			}
			if code != 0 {
				msg := strings.TrimSpace(errOut)
				if msg == "" {
					msg = "yt-dlp did not respond"
				}
				u.status.SetText("error: " + msg)
				return
			}
			line := strings.ReplaceAll(strings.TrimSpace(out), "\n", " · ")
			u.status.SetText("connected: " + line)
		})
	}()
}

func (u *ui) startDownload() {
	var urls []string
	for _, line := range strings.Split(u.urls.Text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		u.notice.SetText("Please paste at least one video URL.")
		return
	}
	if strings.TrimSpace(u.host.Text) == "" || strings.TrimSpace(u.user.Text) == "" {
		u.notice.SetText("SSH details missing — see Setup.")
		u.tabs.Select(u.tabSetup)
		return
	}
	u.persist()
	cfg := u.cfg()
	quality := u.quality
	outDir := strings.TrimSpace(u.outputDir.Text)
	playlist := u.playlist.Checked
	batch := make([]*job, 0, len(urls))
	for i, raw := range urls {
		j := &job{ID: fmt.Sprintf("%d", time.Now().UnixNano()+int64(i)), URL: raw, Status: "queued"}
		if i == 0 {
			j.Status = "running"
		}
		u.jobs = append([]*job{j}, u.jobs...)
		batch = append(batch, j)
	}
	u.renderJobs()
	u.tabs.Select(u.tabQueue)
	u.urls.SetText("")
	if len(batch) == 1 {
		u.notice.SetText("1 job started over SSH.")
	} else {
		u.notice.SetText(fmt.Sprintf("%d jobs started over SSH.", len(batch)))
	}
	go u.runBatch(cfg, quality, outDir, playlist, batch)
}

func (u *ui) runBatch(cfg sshCfg, quality, outDir string, playlist bool, batch []*job) {
	if !u.ytdlpChecked {
		fyne.Do(func() { u.status.SetText("Checking yt-dlp version…") })
		msg := u.checkYTDLP(cfg)
		fyne.Do(func() { u.status.SetText(msg) })
		u.ytdlpChecked = true
	}
	for _, j := range batch {
		j.Status = "running"
		cmd := buildCommand(j.URL, quality, outDir, playlist)
		j.Log = append(j.Log, cmd)
		fyne.Do(u.renderJobs)
		code, err := sshStream(cfg, cmd, func(line string) {
			if len(line) > 240 {
				line = line[:240]
			}
			j.Log = append(j.Log, line)
			if len(j.Log) > 40 {
				j.Log = j.Log[len(j.Log)-40:]
			}
			if m := progressRE.FindStringSubmatch(line); len(m) > 1 {
				var p float64
				fmt.Sscanf(m[1], "%f", &p)
				if int(p) < 99 {
					j.Progress = int(p)
				} else {
					j.Progress = 99
				}
			}
			fyne.Do(u.renderJobs)
		})
		if err != nil {
			j.Status = "error"
			j.Log = append(j.Log, err.Error())
		} else if code == 0 {
			j.Status = "done"
			j.Progress = 100
			j.Log = append(j.Log, "finished")
		} else {
			j.Status = "error"
			j.Log = append(j.Log, fmt.Sprintf("yt-dlp exit %d", code))
		}
		fyne.Do(u.renderJobs)
	}
}

func (u *ui) checkYTDLP(cfg sshCfg) string {
	out, errOut, code, err := sshRun(cfg, "yt-dlp --version", 20*time.Second)
	if err != nil {
		return "yt-dlp check failed: " + err.Error()
	}
	if code != 0 {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = "yt-dlp did not respond"
		}
		return "yt-dlp check failed: " + msg
	}
	installed := strings.Fields(strings.TrimSpace(out))
	cur := ""
	if len(installed) > 0 {
		cur = installed[0]
	}
	latest, err := latestYTDLP()
	if err != nil {
		return fmt.Sprintf("yt-dlp %s (could not check GitHub: %v)", cur, err)
	}
	if !versionLess(cur, latest) {
		return fmt.Sprintf("yt-dlp %s is current", cur)
	}
	updOut, updErr, _, _ := sshRun(cfg, "yt-dlp -U", 3*time.Minute)
	text := strings.TrimSpace(updOut + "\n" + updErr)
	lines := strings.Split(text, "\n")
	last := ""
	if len(lines) > 0 {
		last = lines[len(lines)-1]
	}
	if len(last) > 180 {
		last = last[:180]
	}
	out2, _, _, _ := sshRun(cfg, "yt-dlp --version", 20*time.Second)
	now := strings.TrimSpace(out2)
	if f := strings.Fields(now); len(f) > 0 {
		now = f[0]
	}
	return fmt.Sprintf("yt-dlp update %s: %s", now, last)
}

func (u *ui) renderJobs() {
	if len(u.jobs) == 0 {
		u.queue.SetText("No jobs yet.\nProgress appears here once a download is running over SSH.")
		return
	}
	var b strings.Builder
	for _, j := range u.jobs {
		fmt.Fprintf(&b, "[%s] %d%%  %s\n", j.Status, j.Progress, j.URL)
		start := 0
		if len(j.Log) > 8 {
			start = len(j.Log) - 8
		}
		for _, line := range j.Log[start:] {
			fmt.Fprintf(&b, "    %s\n", line)
		}
		b.WriteByte('\n')
	}
	u.queue.SetText(b.String())
}

func (u *ui) checkAppUpdate() {
	tag, exeURL, err := latestAppRelease()
	if err != nil || tag == "" {
		return
	}
	if !versionLess(Version, tag) {
		return
	}
	fyne.Do(func() {
		msg := fmt.Sprintf("PiLoad %s is available (you have %s).\nUpdate now?", tag, Version)
		dialog.ShowConfirm("Update available", msg, func(ok bool) {
			if !ok {
				return
			}
			u.status.SetText("Downloading update…")
			go func() {
				err := applyUpdate(exeURL)
				fyne.Do(func() {
					if err != nil {
						u.status.SetText("Update failed: " + err.Error())
						dialog.ShowError(err, u.win)
						return
					}
					u.status.SetText("Update downloaded. PiLoad will restart.")
					time.Sleep(400 * time.Millisecond)
					u.win.Close()
					os.Exit(0)
				})
			}()
		}, u.win)
	})
}
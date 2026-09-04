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

// Version is set at build time with -X main.Version=0.3.1
var Version = "0.3.1"

const repoURL = "https://github.com/abb0r/piload"

var progressRE = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)

type job struct {
	ID, URL, Status string
	Progress        int
}

type logLine struct {
	Text, Kind string
}

type ui struct {
	win                                            fyne.Window
	status, notice, profileTip                     *widget.Label
	queue                                          *widget.RichText
	queueScroll                                    *container.Scroll
	urls                                           *widget.Entry
	host, port, user, keyPath, password, outputDir *widget.Entry
	savePW, playlist, autoUpdate                   *widget.Check
	qualityBtns                                    map[string]*widget.Button
	tabs                                           *container.AppTabs
	tabQueue                                       *container.TabItem
	tabSetup                                       *container.TabItem
	quality                                        string
	jobs                                           []*job
	session                                        []logLine
	ytdlpChecked                                   bool
}

func main() {
	cleanupOldBinary()
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
	if cfg.AutoUpdate {
		go u.checkAppUpdate()
	}
	w.ShowAndRun()
}

func (u *ui) build(cfg Settings) {
	u.status = widget.NewLabel("SSH not tested")
	u.notice = widget.NewLabel("")
	u.profileTip = widget.NewLabel("")
	u.profileTip.Wrapping = fyne.TextWrapWord
	u.queue = widget.NewRichText()
	u.queue.Wrapping = fyne.TextWrapBreak
	u.renderLog()

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
	u.autoUpdate = widget.NewCheck("Check for PiLoad updates on startup", func(bool) {
		u.persist()
	})
	u.autoUpdate.SetChecked(cfg.AutoUpdate)
	u.setProfileTip()
}

func (u *ui) layout() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("PiLoad", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ver := widget.NewLabel(Version)
	logo := canvas.NewImageFromResource(fyne.NewStaticResource("icon.png", iconPNG))
	logo.SetMinSize(fyne.NewSize(36, 36))
	logo.FillMode = canvas.ImageFillContain
	header := container.NewBorder(nil, nil, container.NewHBox(logo, title, ver), nil)

	u.queueScroll = container.NewVScroll(u.queue)
	u.queueScroll.SetMinSize(fyne.NewSize(200, 200))
	u.tabs = container.NewAppTabs(
		container.NewTabItem("Download", u.downloadTab()),
		container.NewTabItem("Queue", container.NewBorder(nil, nil, nil, nil, u.queueScroll)),
		container.NewTabItem("Settings", u.setupTab()),
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
		u.autoUpdate,
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
		AutoUpdate:   u.autoUpdate.Checked,
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
				u.appendLog("SSH test failed: "+err.Error(), "error")
				return
			}
			if code != 0 {
				msg := strings.TrimSpace(errOut)
				if msg == "" {
					msg = "yt-dlp did not respond"
				}
				u.status.SetText("error: " + msg)
				u.appendLog(msg, "error")
				return
			}
			line := strings.ReplaceAll(strings.TrimSpace(out), "\n", " · ")
			u.status.SetText("connected: " + line)
			u.appendLog("connected: "+line, "ok")
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
		u.notice.SetText("SSH details missing — see Settings.")
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
		u.jobs = append(u.jobs, j)
		batch = append(batch, j)
	}
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
		fyne.Do(func() {
			u.status.SetText("Checking yt-dlp version…")
			u.appendLog("Checking yt-dlp version…", "info")
		})
		msg := u.checkYTDLP(cfg)
		kind := "ok"
		if strings.Contains(strings.ToLower(msg), "fail") || strings.Contains(strings.ToLower(msg), "error") {
			kind = "error"
		}
		fyne.Do(func() {
			u.status.SetText(msg)
			u.appendLog(msg, kind)
		})
		u.ytdlpChecked = true
	}
	for _, j := range batch {
		j.Status = "running"
		cmd := buildCommand(j.URL, quality, outDir, playlist)
		fyne.Do(func() {
			u.appendLog("", "info")
			u.appendLog("==> "+j.URL, "ok")
			u.appendLog(cmd, "cmd")
		})
		code, err := sshStream(cfg, cmd, func(line string) {
			kind := classifyLine(line)
			if m := progressRE.FindStringSubmatch(line); len(m) > 1 {
				var p float64
				fmt.Sscanf(m[1], "%f", &p)
				if int(p) < 99 {
					j.Progress = int(p)
				} else {
					j.Progress = 99
				}
			}
			fyne.Do(func() { u.appendLog(line, kind) })
		})
		if err != nil {
			j.Status = "error"
			fyne.Do(func() { u.appendLog(err.Error(), "error") })
		} else if code == 0 {
			j.Status = "done"
			j.Progress = 100
			fyne.Do(func() { u.appendLog("finished", "ok") })
		} else {
			j.Status = "error"
			fyne.Do(func() { u.appendLog(fmt.Sprintf("yt-dlp exit %d", code), "error") })
		}
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
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			fyne.Do(func() { u.appendLog(line, classifyLine(line)) })
		}
	}
	out2, _, _, _ := sshRun(cfg, "yt-dlp --version", 20*time.Second)
	now := strings.TrimSpace(out2)
	if f := strings.Fields(now); len(f) > 0 {
		now = f[0]
	}
	return fmt.Sprintf("yt-dlp updated to %s", now)
}

func classifyLine(line string) string {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "error"), strings.Contains(l, "failed"), strings.Contains(l, "traceback"),
		strings.Contains(l, "exit "), strings.HasPrefix(l, "error:"):
		return "error"
	case strings.Contains(l, "warning"), strings.Contains(l, "warn"):
		return "warn"
	case strings.Contains(l, "finished"), strings.Contains(l, "is current"), strings.HasPrefix(l, "connected"):
		return "ok"
	case strings.HasPrefix(l, "yt-dlp ") || strings.HasPrefix(l, "'yt-dlp'"):
		return "cmd"
	default:
		return "info"
	}
}

func (u *ui) appendLog(text, kind string) {
	u.session = append(u.session, logLine{Text: text, Kind: kind})
	u.renderLog()
}

func (u *ui) renderLog() {
	if len(u.session) == 0 {
		u.queue.Segments = []widget.RichTextSegment{
			&widget.TextSegment{
				Text:  "No jobs yet.\nProgress appears here once a download is running over SSH.\nYou can scroll back to the start of the session.",
				Style: widget.RichTextStyle{ColorName: theme.ColorNameDisabled, TextStyle: fyne.TextStyle{Monospace: true}},
			},
		}
		u.queue.Refresh()
		return
	}
	segs := make([]widget.RichTextSegment, 0, len(u.session))
	for _, line := range u.session {
		colorName := theme.ColorNameForeground
		switch line.Kind {
		case "error":
			colorName = theme.ColorNameError
		case "warn":
			colorName = theme.ColorNameWarning
		case "ok":
			colorName = theme.ColorNameSuccess
		case "cmd":
			colorName = theme.ColorNamePrimary
		}
		text := line.Text
		if text == "" {
			text = " "
		}
		segs = append(segs, &widget.TextSegment{
			Text: text + "\n",
			Style: widget.RichTextStyle{
				ColorName: colorName,
				Inline:    false,
				TextStyle: fyne.TextStyle{Monospace: true},
			},
		})
	}
	u.queue.Segments = segs
	u.queue.Refresh()
	if u.queueScroll != nil {
		sz := u.queueScroll.Size()
		if sz.Width > 40 {
			u.queue.Resize(fyne.NewSize(sz.Width-8, u.queue.MinSize().Height))
		}
		u.queueScroll.ScrollToBottom()
	}
}

func (u *ui) checkAppUpdate() {
	tag, exeURL, notes, err := latestAppRelease()
	if err != nil || tag == "" {
		return
	}
	if !versionLess(Version, tag) {
		return
	}
	fyne.Do(func() {
		intro := widget.NewLabel(fmt.Sprintf("PiLoad %s is available (you have %s).", tag, Version))
		intro.Wrapping = fyne.TextWrapWord
		change := widget.NewLabel(notes)
		if strings.TrimSpace(notes) == "" {
			change.SetText("No changelog provided.")
		}
		change.Wrapping = fyne.TextWrapWord
		change.Alignment = fyne.TextAlignLeading
		scroll := container.NewVScroll(change)
		scroll.SetMinSize(fyne.NewSize(460, 240))
		content := container.NewBorder(
			container.NewVBox(intro, widget.NewLabel("Changelog")),
			nil, nil, nil, scroll,
		)
		dialog.ShowCustomConfirm("Update available", "Update", "Later", content, func(ok bool) {
			if !ok {
				return
			}
			bar := widget.NewProgressBar()
			label := widget.NewLabel("Downloading update…")
			prog := dialog.NewCustomWithoutButtons("Updating PiLoad", container.NewVBox(label, bar), u.win)
			prog.Show()
			go func() {
				err := applyUpdate(exeURL, func(got, total int64) {
					fyne.Do(func() {
						if total > 0 {
							bar.SetValue(float64(got) / float64(total))
							label.SetText(fmt.Sprintf("Downloading update… %.0f%%", 100*float64(got)/float64(total)))
						} else {
							label.SetText(fmt.Sprintf("Downloading update… %d KB", got/1024))
						}
					})
				})
				fyne.Do(func() {
					prog.Hide()
					if err != nil {
						u.status.SetText("Update failed: " + err.Error())
						dialog.ShowError(err, u.win)
						return
					}
					u.status.SetText("Update installed. Restarting…")
					time.Sleep(250 * time.Millisecond)
					u.win.Close()
					os.Exit(0)
				})
			}()
		}, u.win)
	})
}

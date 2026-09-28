package main

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// Version is set at build time with -X main.Version=0.4.0
var Version = "0.4.0"

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
	win                                                      fyne.Window
	status, notice, profileTip                               *widget.Label
	queue                                                    *widget.Entry
	queueScroll                                              *container.Scroll
	urls                                                     *widget.Entry
	host, port, user, keyPath, password, outputDir, localDir *widget.Entry
	savePW, playlist, autoUpdate                             *widget.Check
	target                                                   *widget.RadioGroup
	goBtn                                                    *widget.Button
	destHint, toolsLabel                                     *widget.Label
	qualityBtns                                              map[string]*widget.Button
	tabs                                                     *container.AppTabs
	tabQueue                                                 *container.TabItem
	tabSetup                                                 *container.TabItem
	quality                                                  string
	wantLocal                                                bool
	dismissedYTDLP, dismissedDeno                            string
	jobs                                                     []*job
	session                                                  []logLine
	ytdlpChecked                                             bool
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
	u.wantLocal = cfg.Target == "local"
	u.dismissedYTDLP = cfg.DismissedYTDLP
	u.dismissedDeno = cfg.DismissedDeno
	u.build(cfg)
	w.SetContent(u.layout())
	if cfg.AutoUpdate {
		go u.checkAppUpdate()
	}
	w.ShowAndRun()
}

func (u *ui) build(cfg Settings) {
	u.status = widget.NewLabel("Ready")
	u.notice = widget.NewLabel("")
	u.profileTip = widget.NewLabel("")
	u.profileTip.Wrapping = fyne.TextWrapWord
	u.queue = widget.NewMultiLineEntry()
	u.queue.Wrapping = fyne.TextWrapBreak
	u.queue.TextStyle = fyne.TextStyle{Monospace: true}
	u.renderLog()

	u.urls = widget.NewMultiLineEntry()
	u.urls.SetPlaceHolder("https://www.youtube.com/watch?v=…")
	u.urls.Wrapping = fyne.TextWrapWord

	u.outputDir = widget.NewEntry()
	u.outputDir.SetText(cfg.OutputDir)
	u.outputDir.OnChanged = func(string) { u.refreshDest() }
	u.localDir = widget.NewEntry()
	u.localDir.SetText(cfg.LocalDir)
	u.localDir.OnChanged = func(string) { u.refreshDest() }
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
	u.autoUpdate = widget.NewCheck("Check for updates on startup", nil)
	u.autoUpdate.SetChecked(cfg.AutoUpdate)
	u.autoUpdate.OnChanged = func(bool) { u.persist() }
	u.toolsLabel = widget.NewLabel("Local tools: checking…")
	u.toolsLabel.Wrapping = fyne.TextWrapWord
	u.setProfileTip()
	u.refreshToolsLabel()
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
	copyLog := widget.NewButton("Copy log", func() {
		fyne.CurrentApp().Clipboard().SetContent(u.queue.Text)
	})
	u.tabs = container.NewAppTabs(
		container.NewTabItem("Download", u.downloadTab()),
		container.NewTabItem("Queue", container.NewBorder(container.NewHBox(copyLog), nil, nil, nil, u.queueScroll)),
		container.NewTabItem("Settings", u.setupTab()),
	)
	u.tabQueue = u.tabs.Items[1]
	u.tabSetup = u.tabs.Items[2]
	return container.NewBorder(container.NewVBox(header, u.status), nil, nil, nil, u.tabs)
}

func (u *ui) downloadTab() fyne.CanvasObject {
	u.target = widget.NewRadioGroup([]string{"Raspberry Pi", "This PC"}, func(sel string) {
		u.wantLocal = sel == "This PC"
		u.refreshDest()
	})
	u.target.Horizontal = true
	if u.wantLocal {
		u.target.SetSelected("This PC")
	} else {
		u.target.SetSelected("Raspberry Pi")
	}
	u.destHint = widget.NewLabel("")
	u.destHint.Wrapping = fyne.TextWrapWord
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
	u.goBtn = widget.NewButton("Download via SSH", u.startDownload)
	u.goBtn.Importance = widget.HighImportance
	u.refreshDest()
	return container.NewPadded(container.NewVBox(
		widget.NewLabel("Download to"),
		u.target,
		u.destHint,
		widget.NewLabel("Video URLs (one per line)"),
		container.NewGridWrap(fyne.NewSize(900, 120), u.urls),
		row,
		u.profileTip,
		u.playlist,
		u.notice,
		container.NewBorder(nil, nil, nil, u.goBtn),
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
	browse := widget.NewButton("Browse", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			u.localDir.SetText(uriPath(uri))
		}, u.win)
	})
	link, _ := url.Parse(repoURL)
	hyper := widget.NewHyperlink(strings.TrimPrefix(repoURL, "https://"), link)
	body := container.NewVBox(
		form,
		u.savePW,
		widget.NewLabel("Pi folder"),
		u.outputDir,
		widget.NewLabel("Local folder"),
		container.NewBorder(nil, nil, nil, browse, u.localDir),
		u.autoUpdate,
		u.toolsLabel,
		container.NewHBox(test, save),
		widget.NewLabel("Version "+Version),
		hyper,
	)
	return container.NewPadded(container.NewVScroll(body))
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

func (u *ui) snapshot() Settings {
	target := "remote"
	if u.isLocal() {
		target = "local"
	}
	cfg := Settings{
		Host:           strings.TrimSpace(u.host.Text),
		Port:           strings.TrimSpace(u.port.Text),
		User:           strings.TrimSpace(u.user.Text),
		Auth:           u.cfg().Auth,
		KeyPath:        strings.TrimSpace(u.keyPath.Text),
		OutputDir:      strings.TrimSpace(u.outputDir.Text),
		LocalDir:       strings.TrimSpace(u.localDir.Text),
		Target:         target,
		Quality:        u.quality,
		Playlist:       u.playlist.Checked,
		SavePassword:   u.savePW.Checked,
		AutoUpdate:     u.autoUpdate.Checked,
		DismissedYTDLP: u.dismissedYTDLP,
		DismissedDeno:  u.dismissedDeno,
	}
	if cfg.Port == "" {
		cfg.Port = "22"
	}
	if u.savePW.Checked {
		cfg.Password = u.password.Text
	}
	return cfg
}

func (u *ui) persist() {
	if err := saveSettings(u.snapshot()); err != nil {
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
	local := u.isLocal()
	if local {
		if strings.TrimSpace(u.localDir.Text) == "" {
			u.notice.SetText("Choose a local folder in Settings.")
			u.tabs.Select(u.tabSetup)
			return
		}
	} else if strings.TrimSpace(u.host.Text) == "" || strings.TrimSpace(u.user.Text) == "" {
		u.notice.SetText("SSH details missing — see Settings.")
		u.tabs.Select(u.tabSetup)
		return
	}
	u.persist()
	cfg := u.cfg()
	quality := u.quality
	outDir := strings.TrimSpace(u.outputDir.Text)
	if local {
		outDir = strings.TrimSpace(u.localDir.Text)
	}
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
		if local {
			u.notice.SetText("1 job started on this PC.")
		} else {
			u.notice.SetText("1 job started over SSH.")
		}
	} else if local {
		u.notice.SetText(fmt.Sprintf("%d jobs started on this PC.", len(batch)))
	} else {
		u.notice.SetText(fmt.Sprintf("%d jobs started over SSH.", len(batch)))
	}
	go func() {
		if local {
			u.runLocalBatch(quality, outDir, playlist, batch)
			return
		}
		u.runBatch(cfg, quality, outDir, playlist, batch)
	}()
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
		u.queue.SetText("No jobs yet.\nProgress appears here once a download is running.\nSelect text to copy, or use Copy log.")
		return
	}
	var b strings.Builder
	for i, line := range u.session {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line.Text)
	}
	u.queue.SetText(b.String())
	u.scrollQueueToEnd()
}

// The log is a multiline entry, which scrolls itself. The outer container
// cannot move that inner view, so the cursor is placed past the last row.
// Fyne then clamps the scroll offset to the bottom.
func (u *ui) scrollQueueToEnd() {
	u.queue.CursorRow = strings.Count(u.queue.Text, "\n") + 100000
	u.queue.CursorColumn = 0
	u.queue.Refresh()
}

func (u *ui) checkAppUpdate() {
	plan := updatePlan{}
	tag, exeURL, notes, err := latestAppRelease()
	if err == nil && tag != "" && exeURL != "" && versionLess(Version, tag) {
		plan.appTag = tag
		plan.appURL = exeURL
		plan.appNotes = notes
	}
	tools := planToolUpdate(u.dismissedYTDLP, u.dismissedDeno)
	plan.ytdlpTo = tools.ytdlpTo
	plan.ytdlpNotes = tools.ytdlpNotes
	plan.denoTo = tools.denoTo
	plan.withFFmpeg = tools.withFFmpeg
	plan.summary = tools.summary
	if plan.empty() {
		return
	}
	fyne.Do(func() { u.confirmUpdate(plan) })
}

func (u *ui) confirmUpdate(plan updatePlan) {
	var b strings.Builder
	if plan.appURL != "" {
		fmt.Fprintf(&b, "PiLoad %s is available (you have %s).\n\nChangelog\n", plan.appTag, Version)
		if strings.TrimSpace(plan.appNotes) == "" {
			b.WriteString("No changelog provided.\n")
		} else {
			b.WriteString(plan.appNotes)
			b.WriteString("\n")
		}
	}
	if plan.summary != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(plan.summary)
	}
	body := widget.NewLabel(b.String())
	body.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(480, 260))
	dialog.ShowCustomConfirm("Updates available", "Update", "Later", scroll, func(ok bool) {
		if !ok {
			if plan.ytdlpTo != "" {
				u.dismissedYTDLP = plan.ytdlpTo
			}
			if plan.denoTo != "" {
				u.dismissedDeno = plan.denoTo
			}
			_ = saveSettings(u.snapshot())
			return
		}
		u.runUpdatePlan(plan)
	}, u.win)
}

func (u *ui) runUpdatePlan(plan updatePlan) {
	bar := widget.NewProgressBar()
	label := widget.NewLabel("Preparing update…")
	prog := dialog.NewCustomWithoutButtons("Updating", container.NewVBox(label, bar), u.win)
	prog.Show()
	go func() {
		set := func(text string, got, total int64) {
			fyne.Do(func() {
				label.SetText(text)
				if total > 0 {
					bar.SetValue(float64(got) / float64(total))
				}
			})
		}
		var err error
		if plan.hasTools() {
			err = applyToolPlan(plan, func(l string, got, total int64) { set(l, got, total) })
		}
		if err == nil && plan.appURL != "" {
			err = applyUpdate(plan.appURL, func(got, total int64) {
				text := "Downloading PiLoad…"
				if total > 0 {
					text = fmt.Sprintf("Downloading PiLoad… %.0f%%", 100*float64(got)/float64(total))
				}
				set(text, got, total)
			})
		}
		fyne.Do(func() {
			prog.Hide()
			if err != nil {
				u.status.SetText("Update failed: " + err.Error())
				dialog.ShowError(err, u.win)
				return
			}
			if plan.appURL != "" {
				u.status.SetText("Update installed. Restarting…")
				time.Sleep(250 * time.Millisecond)
				u.win.Close()
				os.Exit(0)
				return
			}
			u.status.SetText("Tools updated")
			u.refreshToolsLabel()
		})
	}()
}

func (u *ui) isLocal() bool {
	if u.target != nil && u.target.Selected != "" {
		return u.target.Selected == "This PC"
	}
	return u.wantLocal
}

func (u *ui) refreshDest() {
	if u.destHint != nil && u.localDir != nil && u.outputDir != nil {
		if u.isLocal() {
			u.destHint.SetText("Saving on this PC to " + strings.TrimSpace(u.localDir.Text))
		} else {
			u.destHint.SetText("Saving on the Pi to " + strings.TrimSpace(u.outputDir.Text))
		}
	}
	if u.goBtn != nil {
		if u.isLocal() {
			u.goBtn.SetText("Download on this PC")
		} else {
			u.goBtn.SetText("Download via SSH")
		}
	}
}

func (u *ui) refreshToolsLabel() {
	if u.toolsLabel == nil {
		return
	}
	go func() {
		text := localToolsStatus()
		fyne.Do(func() { u.toolsLabel.SetText(text) })
	}()
}

func uriPath(uri fyne.URI) string {
	if uri == nil {
		return ""
	}
	p := uri.Path()
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func (u *ui) runLocalBatch(quality, outDir string, playlist bool, batch []*job) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fyne.Do(func() { u.appendLog("Could not create folder: "+err.Error(), "error") })
		return
	}
	need := !toolsReady()
	var prog dialog.Dialog
	var bar *widget.ProgressBar
	var label *widget.Label
	if need {
		shown := make(chan struct{})
		fyne.Do(func() {
			bar = widget.NewProgressBar()
			label = widget.NewLabel("Preparing local tools…")
			prog = dialog.NewCustomWithoutButtons("Downloading tools", container.NewVBox(label, bar), u.win)
			prog.Show()
			close(shown)
		})
		<-shown
	}
	err := ensureLocalTools(func(text string, got, total int64) {
		fyne.Do(func() {
			u.status.SetText(text)
			if label != nil {
				if total > 0 {
					label.SetText(fmt.Sprintf("%s %.0f%%", text, 100*float64(got)/float64(total)))
					bar.SetValue(float64(got) / float64(total))
				} else {
					label.SetText(fmt.Sprintf("%s %d KB", text, got/1024))
				}
			}
		})
	})
	if prog != nil {
		fyne.Do(func() { prog.Hide() })
	}
	if err != nil {
		fyne.Do(func() {
			u.status.SetText("Tools failed: " + err.Error())
			u.appendLog("Could not install yt-dlp, FFmpeg or Deno: "+err.Error(), "error")
		})
		return
	}
	fyne.Do(func() { u.refreshToolsLabel() })
	for _, j := range batch {
		j.Status = "running"
		args := buildLocalArgs(j.URL, quality, outDir, playlist)
		shown := "yt-dlp " + strings.Join(args, " ")
		fyne.Do(func() {
			u.appendLog("", "info")
			u.appendLog("==> "+j.URL, "ok")
			u.appendLog(shown, "cmd")
		})
		code, err := runLocalYTDLP(args, func(line string) {
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

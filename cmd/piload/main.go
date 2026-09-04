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

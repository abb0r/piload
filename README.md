# <img src="docs/icon.png" alt="PiLoad" width="36" height="36"> PiLoad

Windows, Linux and macOS (Apple Silicon) program that runs **yt-dlp** on this computer, or on a DietPi Raspberry Pi over SSH.

![PiLoad](docs/piload.png)

## Download

- [Windows](https://github.com/abb0r/piload/releases/latest/download/PiLoad.exe)
- [Linux Flatpak](https://github.com/abb0r/piload/releases/latest/download/PiLoad-linux-x86_64.flatpak)
- [macOS Apple Silicon](https://github.com/abb0r/piload/releases/latest/download/PiLoad-macos-arm64.dmg)

On startup PiLoad checks GitHub for a newer release and asks before updating. If yt-dlp is already installed for local downloads, that check can also offer a newer yt-dlp. FFmpeg is updated only together with yt-dlp or Deno, and Deno only when it is too old for YouTube.

### Linux

```bash
flatpak install --user PiLoad-linux-x86_64.flatpak
flatpak run com.abb0r.PiLoad
```

### macOS

Open the `.dmg` and drag **PiLoad.app** to Applications.

## Settings in the app

On the **Download** tab choose **Raspberry Pi** or **This PC**, paste one video URL per line, and start the job.

On the **Settings** tab set the Pi SSH details and the Pi folder, and the folder used on this computer. The local default is Videos on Windows, Movies on macOS, and the Videos user directory on Linux.

The first local download fetches yt-dlp, FFmpeg, ffprobe and Deno if they are missing:

- Windows: `bin` next to `PiLoad.exe`
- macOS: `~/Library/Application Support/PiLoad/bin`
- Linux: the app data folder

## yt-dlp on DietPi

Install yt-dlp from the DietPi software list: [dietpi.com/docs/software](https://dietpi.com/docs/software/)  
(`dietpi-software` → Browse/Search → **yt-dlp**)

Also install ffmpeg on the Pi:

```bash
sudo apt update
sudo apt install -y ffmpeg
```

Optional, for embedded metadata and thumbnails:

```bash
sudo apt install -y atomicparsley python3-mutagen
```

SSH must be reachable on the Pi (enable OpenSSH in DietPi).
The computer running PiLoad must be on the same LAN.

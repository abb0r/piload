# <img src="docs/icon.png" alt="PiLoad" width="36" height="36"> PiLoad

Windows, Linux and macOS (Apple Silicon) program that starts **yt-dlp on a DietPi Raspberry Pi over SSH**.

![PiLoad](docs/piload.png)

## Download

- [Windows](https://github.com/abb0r/piload/releases/latest/download/PiLoad.exe)
- [Linux Flatpak](https://github.com/abb0r/piload/releases/latest/download/PiLoad-linux-x86_64.flatpak)
- [macOS Apple Silicon](https://github.com/abb0r/piload/releases/latest/download/PiLoad-macos-arm64.dmg)

On startup PiLoad checks GitHub for a newer release and asks before updating.

### Linux

```bash
flatpak install --user PiLoad-linux-x86_64.flatpak
flatpak run com.abb0r.PiLoad
```

### macOS

Open the `.dmg` and drag **PiLoad.app** to Applications.

## Settings in the app

On the **Settings** tab enter host, port `22`, user (`dietpi`) and a password or key file.
On the **Download** tab paste one video URL per line, then send them over SSH.

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

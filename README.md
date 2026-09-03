# PiLoad

Windows-Programm, das **yt-dlp auf einem DietPi-Raspberry per SSH** startet.
Kein Agent, kein Extra-Dienst auf dem Pi.

## Windows

1. [PiLoad.exe](https://github.com/abb0r/piload/actions) aus dem neuesten erfolgreichen Workflow-Lauf unter Artifacts laden (`PiLoad-windows`).
2. Oder lokal:

```powershell
pip install -r desktop/requirements.txt
python desktop/piload.py
```

Exe selbst bauen:

```powershell
pip install -r desktop/requirements.txt pyinstaller
pyinstaller --windowed --onefile --name PiLoad desktop/piload.py
```

## Setup in der App

Unter **Setup** Host, Port `22`, Benutzer (`dietpi`) und Passwort oder Schlüssel eintragen.
**Verbindung prüfen** führt remote `hostname` und `yt-dlp --version` aus.

## yt-dlp auf DietPi

Nur das — einmal per SSH auf dem Pi:

```bash
sudo apt update
sudo apt install -y ffmpeg
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
  -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
sudo mkdir -p /mnt/dietpi_userdata/downloads
sudo chown dietpi:dietpi /mnt/dietpi_userdata/downloads
yt-dlp --version
```

SSH auf dem Pi muss erreichbar sein (DietPi: OpenSSH aktivieren).
Der PC mit PiLoad.exe muss im selben LAN sein.

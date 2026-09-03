# PiLoad

Windows-Programm, das **yt-dlp auf einem DietPi-Raspberry per SSH** startet.
Kein Agent, kein Extra-Dienst auf dem Pi.

## Windows

[PiLoad.exe herunterladen](https://github.com/abb0r/piload/releases/latest/download/PiLoad.exe) (Release [v1.0.0](https://github.com/abb0r/piload/releases/tag/v1.0.0))

Oder lokal:

```powershell
pip install -r desktop/requirements.txt
python desktop/piload.py
```

## Setup in der App

Unter **Setup** Host, Port `22`, Benutzer (`dietpi`) und Passwort oder Schlüssel eintragen.
**Verbindung prüfen** führt remote `hostname` und `yt-dlp --version` aus.

## yt-dlp auf DietPi

yt-dlp über die DietPi-Softwareliste installieren: [dietpi.com/docs/software](https://dietpi.com/docs/software/)  
(`dietpi-software` → Browse/Search → **yt-dlp**)

Zusätzlich auf dem Pi:

```bash
sudo apt update
sudo apt install -y ffmpeg
```

Optional, für eingebettete Metadaten und Thumbnails:

```bash
sudo apt install -y atomicparsley python3-mutagen
```

SSH auf dem Pi muss erreichbar sein (DietPi: OpenSSH aktivieren).
Der PC mit PiLoad.exe muss im selben LAN sein.

# PiLoad

Schlichtes GUI plus DietPi-Agent, um **yt-dlp** auf einem Raspberry Pi im LAN anzustoßen.

## Ablauf

1. Agent auf dem Pi installieren (`agent/install.sh`).
2. Im Browser `http://<PI-IP>:8787` öffnen **oder** diese Oberfläche auf Live-Pi stellen.
3. URL einfügen — yt-dlp startet mit festen, sinnvollen Defaults.

## Optimale yt-dlp-Defaults

Je nach Preset (Auto 1080p / Beste Quelle / 1080 / 720 / nur Audio) plus immer:

- Video+Audio mergen
- Metadaten und Kapitel einbetten
- DE/EN-Untertitel (ohne Live-Chat)
- SponsorBlock-Marken
- 4 parallele Fragmente
- stabile Dateinamen
- Ziel: `/mnt/dietpi_userdata/downloads/%(title)s [%(id)s].%(ext)s`

## DietPi installieren

```bash
# Repo klonen, dann:
sh agent/install.sh
```

Manuell:

```bash
sudo apt install -y python3 ffmpeg
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
  -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
python3 agent/server.py --host 0.0.0.0 --port 8787 --dir /mnt/dietpi_userdata/downloads
```

Optional Token:

```bash
PILOAD_TOKEN=geheim python3 agent/server.py --token geheim
```

API:

- `GET /api/health`
- `POST /api/download` `{ "url", "quality", "outputDir", "playlist" }`
- `GET /api/jobs`

Der Agent erlaubt CORS, damit ein Rechner im LAN die Oberfläche hosten kann.

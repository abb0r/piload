#!/bin/sh
set -eu
DIR="${1:-/opt/piload}"
DOWNLOADS="${2:-/mnt/dietpi_userdata/downloads}"

echo "PiLoad → $DIR"
sudo mkdir -p "$DIR" "$DOWNLOADS"
sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y python3 ffmpeg
if ! command -v yt-dlp >/dev/null 2>&1; then
  sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
  sudo chmod a+rx /usr/local/bin/yt-dlp
fi
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
sudo cp "$HERE/server.py" "$HERE/index.html" "$DIR/"
sudo chmod +x "$DIR/server.py"

SERVICE=/etc/systemd/system/piload.service
sudo tee "$SERVICE" >/dev/null <<EOF
[Unit]
Description=PiLoad yt-dlp agent
After=network-online.target

[Service]
WorkingDirectory=$DIR
ExecStart=/usr/bin/python3 $DIR/server.py --host 0.0.0.0 --port 8787 --dir $DOWNLOADS
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now piload.service
echo "Bereit: http://$(hostname -I | awk '{print $1}'):8787"

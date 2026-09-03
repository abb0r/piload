#!/usr/bin/env python3
"""PiLoad agent — tiny HTTP front for yt-dlp on DietPi."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

PRESETS = {
    "auto": [
        "-f",
        "bv*[height<=1080]+ba/b[height<=1080]/b",
        "-S",
        "res:1080,ext:mp4:m4a",
        "--merge-output-format",
        "mp4",
    ],
    "best": ["-f", "bv*+ba/b", "--merge-output-format", "mkv"],
    "1080": [
        "-f",
        "bv*[height<=1080]+ba/b[height<=1080]",
        "--merge-output-format",
        "mp4",
    ],
    "720": [
        "-f",
        "bv*[height<=720]+ba/b[height<=720]",
        "--merge-output-format",
        "mp4",
    ],
    "audio": ["-f", "ba/b", "-x", "--audio-format", "mp3", "--audio-quality", "0"],
}

COMMON = [
    "--embed-metadata",
    "--embed-chapters",
    "--embed-subs",
    "--sub-langs",
    "en.*,de.*,-live_chat",
    "--write-auto-subs",
    "--sponsorblock-mark",
    "all",
    "--concurrent-fragments",
    "4",
    "--no-mtime",
    "--restrict-filenames",
    "--newline",
    "--no-warnings",
]

JOBS: dict[str, dict] = {}
LOCK = threading.Lock()
CONFIG = {"dir": str(Path.home() / "downloads"), "token": "", "yt": "yt-dlp"}


def json_bytes(payload, code=200):
    body = json.dumps(payload).encode("utf-8")
    return code, body, "application/json; charset=utf-8"


def public_job(job: dict) -> dict:
    return {
        "id": job["id"],
        "url": job["url"],
        "title": job["title"],
        "quality": job["quality"],
        "status": job["status"],
        "progress": job["progress"],
        "speed": job.get("speed", "—"),
        "eta": job.get("eta", "—"),
        "log": job.get("log", [])[-40:],
        "createdAt": job["createdAt"],
        "error": job.get("error"),
        "output": job.get("output"),
    }


def parse_progress(line: str, job: dict) -> None:
    text = line.strip()
    if not text:
        return
    job["log"].append(text[:400])
    if job["log"].__len__() > 80:
        job["log"] = job["log"][-80:]
    if "[download]" in text and "%" in text:
        try:
            pct = text.split("%", 1)[0].split()[-1]
            job["progress"] = max(job["progress"], min(99, float(pct)))
        except ValueError:
            pass
        parts = text.split()
        for i, part in enumerate(parts):
            if part == "at" and i + 1 < len(parts):
                job["speed"] = parts[i + 1]
            if part == "ETA" and i + 1 < len(parts):
                job["eta"] = parts[i + 1]


def run_job(job_id: str) -> None:
    with LOCK:
        job = JOBS[job_id]
        job["status"] = "running"
        url = job["url"]
        quality = job["quality"]
        out_dir = Path(job["outputDir"])
        playlist = job["playlist"]

    out_dir.mkdir(parents=True, exist_ok=True)
    template = str(out_dir / "%(title)s [%(id)s].%(ext)s")
    cmd = [CONFIG["yt"], *PRESETS.get(quality, PRESETS["auto"]), *COMMON, "-o", template]
    if not playlist:
        cmd.append("--no-playlist")
    cmd.append(url)

    try:
        proc = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        assert proc.stdout is not None
        for line in proc.stdout:
            with LOCK:
                parse_progress(line, JOBS[job_id])
        code = proc.wait()
        with LOCK:
            job = JOBS[job_id]
            if code == 0:
                job["status"] = "done"
                job["progress"] = 100
                job["speed"] = "—"
                job["eta"] = "00:00"
                job["log"].append("fertig")
            else:
                job["status"] = "error"
                job["error"] = f"yt-dlp exit {code}"
    except FileNotFoundError:
        with LOCK:
            JOBS[job_id]["status"] = "error"
            JOBS[job_id]["error"] = "yt-dlp nicht gefunden — siehe README"
    except Exception as exc:
        with LOCK:
            JOBS[job_id]["status"] = "error"
            JOBS[job_id]["error"] = str(exc)


def yt_version() -> str:
    try:
        out = subprocess.check_output([CONFIG["yt"], "--version"], text=True, timeout=8)
        return out.strip()
    except Exception:
        return "nicht installiert"


def free_gb(path: str):
    try:
        usage = shutil.disk_usage(path)
        return round(usage.free / 1024 / 1024 / 1024, 1)
    except OSError:
        return None


INDEX_HTML = Path(__file__).with_name("index.html")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("[piload] " + (fmt % args) + "\n")

    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "Authorization, Content-Type")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

    def _auth_ok(self) -> bool:
        token = CONFIG["token"]
        if not token:
            return True
        header = self.headers.get("Authorization", "")
        return header == f"Bearer {token}"

    def do_OPTIONS(self):
        self.send_response(204)
        self._cors()
        self.end_headers()

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path in ("/", "/index.html"):
            if INDEX_HTML.exists():
                data = INDEX_HTML.read_bytes()
                self.send_response(200)
                self._cors()
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                return
        if parsed.path == "/api/health":
            if not self._auth_ok():
                return self._send(*json_bytes({"error": "unauthorized"}, 401))
            payload = {
                "ok": True,
                "hostname": socket.gethostname(),
                "ytDlp": yt_version(),
                "python": f"{sys.version_info.major}.{sys.version_info.minor}",
                "downloadDir": CONFIG["dir"],
                "freeGb": free_gb(CONFIG["dir"]),
            }
            return self._send(*json_bytes(payload))
        if parsed.path == "/api/jobs":
            if not self._auth_ok():
                return self._send(*json_bytes({"error": "unauthorized"}, 401))
            with LOCK:
                jobs = [public_job(j) for j in JOBS.values()]
            jobs.sort(key=lambda j: j["createdAt"], reverse=True)
            return self._send(*json_bytes(jobs))
        self._send(*json_bytes({"error": "not found"}, 404))

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path != "/api/download":
            return self._send(*json_bytes({"error": "not found"}, 404))
        if not self._auth_ok():
            return self._send(*json_bytes({"error": "unauthorized"}, 401))
        length = int(self.headers.get("Content-Length", "0") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            return self._send(*json_bytes({"error": "invalid json"}, 400))
        url = str(body.get("url") or "").strip()
        if not url.startswith("http"):
            return self._send(*json_bytes({"error": "url required"}, 400))
        quality = str(body.get("quality") or "auto")
        if quality not in PRESETS:
            quality = "auto"
        output_dir = str(body.get("outputDir") or CONFIG["dir"])
        playlist = bool(body.get("playlist"))
        job_id = uuid.uuid4().hex[:10]
        job = {
            "id": job_id,
            "url": url,
            "title": url,
            "quality": quality,
            "status": "queued",
            "progress": 0,
            "speed": "—",
            "eta": "—",
            "log": [f"queued preset={quality}"],
            "createdAt": int(time.time() * 1000),
            "outputDir": output_dir,
            "playlist": playlist,
        }
        with LOCK:
            JOBS[job_id] = job
        threading.Thread(target=run_job, args=(job_id,), daemon=True).start()
        self._send(*json_bytes(public_job(job)))

    def _send(self, code, body, content_type):
        self.send_response(code)
        self._cors()
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser(description="PiLoad DietPi agent")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8787)
    parser.add_argument("--dir", default=str(Path.home() / "downloads"))
    parser.add_argument("--token", default=os.environ.get("PILOAD_TOKEN", ""))
    parser.add_argument("--yt-dlp", default=os.environ.get("YT_DLP", "yt-dlp"))
    args = parser.parse_args()
    CONFIG["dir"] = args.dir
    CONFIG["token"] = args.token
    CONFIG["yt"] = args.yt_dlp
    Path(args.dir).mkdir(parents=True, exist_ok=True)
    httpd = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"PiLoad listening on http://{args.host}:{args.port}", flush=True)
    print(f"downloads -> {args.dir}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()

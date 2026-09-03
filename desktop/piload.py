#!/usr/bin/env python3
"""PiLoad — Windows GUI that runs yt-dlp on a DietPi box over SSH."""

from __future__ import annotations

import json
import os
import queue
import re
import shlex
import sys
import threading
import time
import uuid
import urllib.error
import urllib.request
import webbrowser
from pathlib import Path

import customtkinter as ctk
import paramiko

APP_DIR = Path(os.environ.get("APPDATA") or Path.home() / ".piload") / "PiLoad"
SETTINGS = APP_DIR / "settings.json"
VERSION = "0.1.6"
REPO_URL = "https://github.com/abb0r/piload"

PRESETS = {
    "best": [
        "-f",
        "bv*+ba/b",
        "-S",
        "res,vcodec:h264,acodec:m4a,ext:mp4:m4a",
        "--merge-output-format",
        "mp4",
    ],
    "1080": [
        "-f",
        "bv*[height<=1080]+ba/b[height<=1080]/b",
        "-S",
        "res:1080,vcodec:h264,acodec:m4a,ext:mp4:m4a",
        "--merge-output-format",
        "mp4",
    ],
    "720": [
        "-f",
        "bv*[height<=720]+ba/b[height<=720]/b",
        "-S",
        "res:720,vcodec:h264,acodec:m4a,ext:mp4:m4a",
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

QUALITY_PROFILES = [
    (
        "best",
        "Best Quality",
        "Highest available video and audio, merged to MP4.\n"
        "Prefers H.264 + AAC so the file plays widely without recoding.",
    ),
    (
        "1080",
        "1080p",
        "Best video up to 1080p plus best audio, merged to MP4.\n"
        "Prefers H.264 + AAC. Higher resolutions are ignored.",
    ),
    (
        "720",
        "720p",
        "Best video up to 720p plus best audio, merged to MP4.\n"
        "Prefers H.264 + AAC. Higher resolutions are ignored.",
    ),
    (
        "audio",
        "Audio only",
        "Best audio track only, converted to MP3 at maximum quality.\n"
        "No video is downloaded.",
    ),
]

BG = "#0c0d0f"
SURFACE = "#15171b"
ELEVATED = "#1c1f25"
FG = "#eceef1"
MUTED = "#9aa1ab"
ACCENT = "#d7dde6"
ACCENT_FG = "#0c0d0f"
BORDER = "#2a2e36"
OK = "#7d9b86"
BAD = "#c07a72"


class HoverTip:
    def __init__(self, widget, text: str) -> None:
        self.widget = widget
        self.text = text
        self.tip = None
        widget.bind("<Enter>", self._show)
        widget.bind("<Leave>", self._hide)

    def _show(self, _event=None) -> None:
        if self.tip is not None:
            return
        self.tip = ctk.CTkToplevel(self.widget)
        self.tip.overrideredirect(True)
        self.tip.attributes("-topmost", True)
        self.tip.configure(fg_color=ELEVATED)
        ctk.CTkLabel(
            self.tip,
            text=self.text,
            text_color=FG,
            justify="left",
            wraplength=340,
            font=ctk.CTkFont(size=13),
        ).pack(padx=12, pady=10)
        self.tip.update_idletasks()
        x = self.widget.winfo_rootx()
        y = self.widget.winfo_rooty() + self.widget.winfo_height() + 8
        self.tip.geometry(f"+{x}+{y}")

    def _hide(self, _event=None) -> None:
        if self.tip is not None:
            self.tip.destroy()
            self.tip = None


def app_icon_path() -> Path | None:
    candidates = []
    if getattr(sys, "frozen", False):
        meipass = Path(getattr(sys, "_MEIPASS", ""))
        candidates.append(meipass / "piload.ico")
        candidates.append(Path(sys.executable).with_name("piload.ico"))
    here = Path(__file__).resolve().parent
    candidates.append(here / "piload.ico")
    for path in candidates:
        if path.is_file():
            return path
    return None


def load_settings() -> dict:
    defaults = {
        "host": "192.168.1.42",
        "port": "22",
        "user": "dietpi",
        "auth": "password",
        "key_path": "",
        "output_dir": "/mnt/dietpi_userdata/downloads",
        "quality": "best",
        "playlist": False,
        "save_password": False,
        "password": "",
    }
    try:
        data = json.loads(SETTINGS.read_text(encoding="utf-8"))
        defaults.update({k: data[k] for k in defaults if k in data})
        if defaults.get("quality") not in PRESETS:
            defaults["quality"] = "best"
    except (OSError, json.JSONDecodeError):
        pass
    return defaults


def save_settings(data: dict) -> None:
    APP_DIR.mkdir(parents=True, exist_ok=True)
    SETTINGS.write_text(json.dumps(data, indent=2), encoding="utf-8")


def version_key(value: str) -> tuple:
    nums = [int(part) for part in re.split(r"[^\d]+", value) if part]
    return tuple(nums) if nums else (0,)


def latest_ytdlp_release() -> str:
    req = urllib.request.Request(
        "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest",
        headers={"User-Agent": f"PiLoad/{VERSION}", "Accept": "application/vnd.github+json"},
    )
    with urllib.request.urlopen(req, timeout=12) as resp:
        data = json.loads(resp.read().decode("utf-8"))
    tag = str(data.get("tag_name") or "").strip()
    return tag[1:] if tag.lower().startswith("v") else tag


def build_command(url: str, quality: str, output_dir: str, playlist: bool) -> str:
    out = output_dir.rstrip("/") + "/%(title)s [%(id)s].%(ext)s"
    parts = ["yt-dlp", *PRESETS.get(quality, PRESETS["best"]), *COMMON, "-o", out]
    if not playlist:
        parts.append("--no-playlist")
    parts.append(url)
    return " ".join(shlex.quote(p) for p in parts)


class SshSession:
    def __init__(self, cfg: dict, password: str = "") -> None:
        self.cfg = cfg
        self.password = password

    def connect(self) -> paramiko.SSHClient:
        client = paramiko.SSHClient()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        kwargs = {
            "hostname": self.cfg["host"].strip(),
            "port": int(self.cfg["port"] or 22),
            "username": self.cfg["user"].strip(),
            "timeout": 12,
            "allow_agent": False,
            "look_for_keys": False,
        }
        if self.cfg.get("auth") == "key" and self.cfg.get("key_path"):
            kwargs["key_filename"] = self.cfg["key_path"]
        else:
            kwargs["password"] = self.password
        client.connect(**kwargs)
        return client

    def probe(self) -> str:
        client = self.connect()
        try:
            _, stdout, stderr = client.exec_command(
                "hostname; yt-dlp --version",
                timeout=20,
            )
            out = stdout.read().decode("utf-8", "replace").strip()
            err = stderr.read().decode("utf-8", "replace").strip()
            if stdout.channel.recv_exit_status() != 0:
                raise RuntimeError(err or "yt-dlp did not respond")
            return out.replace("\n", " · ")
        finally:
            client.close()

    def ytdlp_version(self) -> str:
        client = self.connect()
        try:
            _, stdout, stderr = client.exec_command("yt-dlp --version", timeout=20)
            out = stdout.read().decode("utf-8", "replace").strip()
            err = stderr.read().decode("utf-8", "replace").strip()
            if stdout.channel.recv_exit_status() != 0 or not out:
                raise RuntimeError(err or "yt-dlp did not respond")
            return out.split()[0]
        finally:
            client.close()

    def update_ytdlp(self) -> str:
        client = self.connect()
        try:
            _, stdout, stderr = client.exec_command("yt-dlp -U", timeout=180)
            out = stdout.read().decode("utf-8", "replace").strip()
            err = stderr.read().decode("utf-8", "replace").strip()
            text = "\n".join(part for part in (out, err) if part).strip()
            return text or "yt-dlp update finished"
        finally:
            client.close()

    def run(self, command: str, on_line) -> int:
        client = self.connect()
        try:
            _, stdout, stderr = client.exec_command(command, timeout=None)
            stdout.channel.settimeout(1.0)
            while True:
                if stdout.channel.recv_ready() or stdout.channel.recv_stderr_ready():
                    chunk = b""
                    if stdout.channel.recv_ready():
                        chunk += stdout.channel.recv(4096)
                    if stdout.channel.recv_stderr_ready():
                        chunk += stdout.channel.recv_stderr(4096)
                    text = chunk.decode("utf-8", "replace")
                    for line in text.splitlines():
                        if line.strip():
                            on_line(line.rstrip())
                if stdout.channel.exit_status_ready():
                    rest = stdout.read() + stderr.read()
                    if rest:
                        for line in rest.decode("utf-8", "replace").splitlines():
                            if line.strip():
                                on_line(line.rstrip())
                    return stdout.channel.recv_exit_status()
                time.sleep(0.08)
        finally:
            client.close()


PROGRESS_RE = re.compile(r"(\d+(?:\.\d+)?)%")


class App(ctk.CTk):
    def __init__(self) -> None:
        super().__init__()
        self.title("PiLoad")
        self.geometry("980x720")
        self.minsize(820, 620)
        ctk.set_appearance_mode("dark")
        ctk.set_default_color_theme("dark-blue")
        self.configure(fg_color=BG)
        icon = app_icon_path()
        if icon is not None:
            try:
                self.iconbitmap(str(icon))
            except Exception:
                pass

        self.settings = load_settings()
        self.password = ""
        self.events: queue.Queue = queue.Queue()
        self.jobs: list[dict] = []
        self.quality = self.settings.get("quality", "best")
        if self.quality not in PRESETS:
            self.quality = "best"
        self._ytdlp_checked = False

        self._build()
        self.after(120, self._drain)

    def _build(self) -> None:
        header = ctk.CTkFrame(self, fg_color=SURFACE, corner_radius=0)
        header.pack(fill="x")
        title_row = ctk.CTkFrame(header, fg_color="transparent")
        title_row.pack(fill="x", padx=18, pady=(18, 16))
        ctk.CTkLabel(
            title_row,
            text="PiLoad",
            text_color=FG,
            font=ctk.CTkFont(size=32, weight="bold"),
        ).pack(side="left")
        ctk.CTkLabel(
            title_row,
            text=VERSION,
            text_color=MUTED,
            font=ctk.CTkFont(size=14),
        ).pack(side="left", padx=(10, 0), pady=(14, 0))

        self.status = ctk.CTkLabel(self, text="SSH not tested", text_color=MUTED, anchor="w")
        self.status.pack(fill="x", padx=22, pady=(12, 0))

        self.tabs = ctk.CTkTabview(
            self,
            fg_color=SURFACE,
            segmented_button_fg_color=ELEVATED,
            segmented_button_selected_color="#3a4048",
            segmented_button_selected_hover_color="#454c56",
            segmented_button_unselected_color=ELEVATED,
            text_color=FG,
        )
        self.tabs.pack(fill="both", expand=True, padx=18, pady=16)
        self.tab_dl = self.tabs.add("Download")
        self.tab_q = self.tabs.add("Queue")
        self.tab_s = self.tabs.add("Setup")
        self._build_download()
        self._build_queue()
        self._build_setup()

    def _build_download(self) -> None:
        ctk.CTkLabel(self.tab_dl, text="Video URLs (one per line)", text_color=MUTED, anchor="w").pack(
            fill="x", padx=12, pady=(12, 4)
        )
        self.url = ctk.CTkTextbox(self.tab_dl, height=120, fg_color=BG, text_color=FG)
        self.url.pack(fill="x", padx=12)

        row = ctk.CTkFrame(self.tab_dl, fg_color="transparent")
        row.pack(fill="x", padx=12, pady=12)
        self.q_buttons = {}
        for key, label, tip in QUALITY_PROFILES:
            btn = ctk.CTkButton(
                row,
                text=label,
                width=128,
                fg_color=ELEVATED if key != self.quality else ACCENT,
                text_color=FG if key != self.quality else ACCENT_FG,
                command=lambda k=key: self._set_quality(k),
            )
            btn.pack(side="left", padx=4)
            HoverTip(btn, tip)
            self.q_buttons[key] = btn

        ctk.CTkLabel(self.tab_dl, text="Folder on the Pi", text_color=MUTED, anchor="w").pack(
            fill="x", padx=12, pady=(8, 4)
        )
        self.output = ctk.CTkEntry(self.tab_dl, fg_color=BG, text_color=FG)
        self.output.insert(0, self.settings["output_dir"])
        self.output.pack(fill="x", padx=12)

        self.playlist = ctk.CTkCheckBox(self.tab_dl, text="Download entire playlist", text_color=MUTED)
        if self.settings.get("playlist"):
            self.playlist.select()
        self.playlist.pack(anchor="w", padx=12, pady=10)

        self.notice = ctk.CTkLabel(self.tab_dl, text="", text_color=MUTED, anchor="w")
        self.notice.pack(fill="x", padx=12)
        ctk.CTkButton(
            self.tab_dl,
            text="Download via SSH",
            fg_color=ACCENT,
            text_color=ACCENT_FG,
            hover_color="#c4cad3",
            command=self.start_download,
        ).pack(anchor="e", padx=12, pady=12)

    def _build_queue(self) -> None:
        self.queue_box = ctk.CTkTextbox(self.tab_q, fg_color=BG, text_color=FG)
        self.queue_box.pack(fill="both", expand=True, padx=12, pady=12)
        self._render_jobs()

    def _build_setup(self) -> None:
        grid = ctk.CTkFrame(self.tab_s, fg_color="transparent")
        grid.pack(fill="x", padx=12, pady=12)

        self.host = self._labeled_entry(grid, "Host / IP", self.settings["host"], 0)
        self.port = self._labeled_entry(grid, "SSH port", self.settings["port"], 1)
        self.user = self._labeled_entry(grid, "User", self.settings["user"], 2)
        self.key_path = self._labeled_entry(grid, "Key file (optional)", self.settings["key_path"], 3)

        ctk.CTkLabel(grid, text="Password", text_color=MUTED, anchor="w").grid(
            row=8, column=0, sticky="w", pady=(8, 2)
        )
        self.pw = ctk.CTkEntry(grid, show="*", fg_color=BG, text_color=FG)
        if self.settings.get("save_password") and self.settings.get("password"):
            self.pw.insert(0, self.settings["password"])
        self.pw.grid(row=9, column=0, columnspan=2, sticky="ew")
        self.save_pw = ctk.CTkCheckBox(grid, text="Save SSH password", text_color=MUTED)
        if self.settings.get("save_password"):
            self.save_pw.select()
        self.save_pw.grid(row=10, column=0, sticky="w", pady=(8, 0))
        grid.grid_columnconfigure(0, weight=1)
        grid.grid_columnconfigure(1, weight=1)

        btns = ctk.CTkFrame(self.tab_s, fg_color="transparent")
        btns.pack(fill="x", padx=12)
        ctk.CTkButton(
            btns,
            text="Test connection",
            fg_color=ELEVATED,
            command=self.test_connection,
        ).pack(side="left")
        ctk.CTkButton(
            btns,
            text="Save settings",
            fg_color=ELEVATED,
            command=self.persist,
        ).pack(side="left", padx=8)

        meta = ctk.CTkFrame(self.tab_s, fg_color="transparent")
        meta.pack(side="bottom", fill="x", padx=12, pady=16)
        ctk.CTkLabel(meta, text=f"Version {VERSION}", text_color=MUTED, anchor="w").pack(anchor="w")
        link = ctk.CTkButton(
            meta,
            text=REPO_URL.replace("https://", ""),
            fg_color="transparent",
            hover_color=ELEVATED,
            text_color=ACCENT,
            anchor="w",
            command=lambda: webbrowser.open(REPO_URL),
        )
        link.pack(anchor="w")

    def _labeled_entry(self, parent, label, value, row):
        ctk.CTkLabel(parent, text=label, text_color=MUTED, anchor="w").grid(
            row=row * 2, column=0, sticky="w", pady=(8, 2)
        )
        entry = ctk.CTkEntry(parent, fg_color=BG, text_color=FG)
        entry.insert(0, value)
        entry.grid(row=row * 2 + 1, column=0, columnspan=2, sticky="ew")
        return entry

    def _set_quality(self, key: str) -> None:
        self.quality = key
        for k, btn in self.q_buttons.items():
            if k == key:
                btn.configure(fg_color=ACCENT, text_color=ACCENT_FG)
            else:
                btn.configure(fg_color=ELEVATED, text_color=FG)

    def persist(self) -> None:
        self.settings.update(
            {
                "host": self.host.get().strip(),
                "port": self.port.get().strip() or "22",
                "user": self.user.get().strip(),
                "auth": "key" if self.key_path.get().strip() else "password",
                "key_path": self.key_path.get().strip(),
                "output_dir": self.output.get().strip(),
                "quality": self.quality,
                "playlist": bool(self.playlist.get()),
                "save_password": bool(self.save_pw.get()),
                "password": self.pw.get() if self.save_pw.get() else "",
            }
        )
        save_settings(self.settings)
        self.status.configure(text="Settings saved", text_color=OK)

    def _cfg(self) -> dict:
        self.password = self.pw.get()
        return {
            "host": self.host.get().strip(),
            "port": self.port.get().strip() or "22",
            "user": self.user.get().strip(),
            "auth": "key" if self.key_path.get().strip() else "password",
            "key_path": self.key_path.get().strip(),
        }

    def test_connection(self) -> None:
        self.status.configure(text="Testing SSH…", text_color=MUTED)

        def work():
            try:
                detail = SshSession(self._cfg(), self.pw.get()).probe()
                self.events.put(("status", "ok", detail))
            except Exception as exc:
                self.events.put(("status", "fail", str(exc)))

        threading.Thread(target=work, daemon=True).start()

    def _check_ytdlp(self, cfg: dict, password: str) -> str:
        session = SshSession(cfg, password)
        installed = session.ytdlp_version()
        try:
            latest = latest_ytdlp_release()
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError, OSError) as exc:
            return f"yt-dlp {installed} (could not check GitHub: {exc})"
        if latest and version_key(installed) >= version_key(latest):
            return f"yt-dlp {installed} is current"
        update = session.update_ytdlp()
        try:
            installed = session.ytdlp_version()
        except Exception:
            pass
        return f"yt-dlp update {installed}: {update.splitlines()[-1][:180]}"

    def start_download(self) -> None:
        raw = self.url.get("1.0", "end")
        urls = [line.strip() for line in raw.splitlines() if line.strip()]
        if not urls:
            self.notice.configure(text="Please paste at least one video URL.")
            return
        if not self.host.get().strip() or not self.user.get().strip():
            self.notice.configure(text="SSH details missing — see Setup.")
            self.tabs.set("Setup")
            return
        self.persist()
        quality = self.quality
        output_dir = self.output.get().strip()
        playlist = bool(self.playlist.get())
        cfg = self._cfg()
        password = self.pw.get()
        batch = []
        for index, url in enumerate(urls):
            job = {
                "id": uuid.uuid4().hex[:8],
                "url": url,
                "status": "queued" if index else "running",
                "progress": 0,
                "log": [],
            }
            self.jobs.insert(0, job)
            batch.append(job)
        self._render_jobs()
        self.tabs.set("Queue")
        self.url.delete("1.0", "end")
        count = len(batch)
        self.notice.configure(
            text=f"{count} job started over SSH." if count == 1 else f"{count} jobs started over SSH."
        )

        def work():
            if not self._ytdlp_checked:
                self.events.put(("status", "ok", "Checking yt-dlp version…"))
                try:
                    detail = self._check_ytdlp(cfg, password)
                    self.events.put(("status", "ok", detail))
                except Exception as exc:
                    self.events.put(("status", "fail", f"yt-dlp check failed: {exc}"))
                self._ytdlp_checked = True
            for job in batch:
                self.events.put(("start", job["id"]))
                cmd = build_command(job["url"], quality, output_dir, playlist)
                self.events.put(("line", job["id"], cmd))

                def on_line(line: str, job_id=job["id"]):
                    self.events.put(("line", job_id, line))

                try:
                    code = SshSession(cfg, password).run(cmd, on_line)
                    self.events.put(("done", job["id"], code))
                except Exception as exc:
                    self.events.put(("error", job["id"], str(exc)))

        threading.Thread(target=work, daemon=True).start()

    def _job(self, job_id: str):
        for job in self.jobs:
            if job["id"] == job_id:
                return job
        return None

    def _render_jobs(self) -> None:
        lines = []
        if not self.jobs:
            lines.append("No jobs yet.\nProgress appears here once a download is running over SSH.")
        for job in self.jobs:
            lines.append(f"[{job['status']}] {job['progress']}%  {job['url']}")
            lines.extend("    " + item for item in job["log"][-8:])
            lines.append("")
        self.queue_box.configure(state="normal")
        self.queue_box.delete("1.0", "end")
        self.queue_box.insert("1.0", "\n".join(lines))
        self.queue_box.configure(state="disabled")

    def _drain(self) -> None:
        changed = False
        try:
            while True:
                item = self.events.get_nowait()
                kind = item[0]
                if kind == "status":
                    _, state, detail = item
                    color = OK if state == "ok" else BAD
                    prefix = "connected" if state == "ok" else "error"
                    self.status.configure(text=f"{prefix}: {detail}", text_color=color)
                elif kind == "start":
                    _, job_id = item
                    job = self._job(job_id)
                    if job:
                        job["status"] = "running"
                        changed = True
                elif kind == "line":
                    _, job_id, line = item
                    job = self._job(job_id)
                    if job:
                        job["log"].append(line[:240])
                        job["log"] = job["log"][-40:]
                        found = PROGRESS_RE.search(line)
                        if found:
                            job["progress"] = min(99, int(float(found.group(1))))
                        changed = True
                elif kind == "done":
                    _, job_id, code = item
                    job = self._job(job_id)
                    if job:
                        job["status"] = "done" if code == 0 else "error"
                        job["progress"] = 100 if code == 0 else job["progress"]
                        job["log"].append("finished" if code == 0 else f"yt-dlp exit {code}")
                        changed = True
                elif kind == "error":
                    _, job_id, message = item
                    job = self._job(job_id)
                    if job:
                        job["status"] = "error"
                        job["log"].append(message)
                        changed = True
        except queue.Empty:
            pass
        if changed:
            self._render_jobs()
        self.after(120, self._drain)


def main() -> None:
    App().mainloop()


if __name__ == "__main__":
    main()

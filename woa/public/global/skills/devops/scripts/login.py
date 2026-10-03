#!/usr/bin/env python3
"""Automated bk_ticket acquisition + bkoauth exchange, zero agent involvement.

Flow:
1. Launch system Edge with a dedicated profile, CDP debugging enabled.
2. Navigate to the DevOps console and poll the bk_ticket cookie every 2s.
3. If not logged in, keep the window open until the user completes login
   (the script never sees or touches credentials - only the resulting cookie).
4. On ticket capture: exchange -> check -> refresh -> persist to config.json.

Only the Python standard library is used (CDP over a raw WebSocket client).
Run:  python scripts/login.py            (normal, waits for login if needed)
      python scripts/login.py --headless (no window, only works if the cookie
                                          is still alive in the profile)
"""

from __future__ import annotations

import argparse
import base64
import datetime
import hashlib
import json
import os
import shutil
import socket
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

SKILL_DIR = Path(__file__).resolve().parent.parent
CONFIG_PATH = SKILL_DIR / "config.json"
PROFILE_DIR = SKILL_DIR / ".edge-profile"
AUTH_BASE = "http://apigw.o.woa.com/auth_api"
CONSOLE_URL = "https://devops.woa.com/console/"
TICKET_DOMAIN = "devops.woa.com"
TICKET_NAME = "bk_ticket"
POLL_INTERVAL = 2.0
MAX_LOGIN_WAIT = 600.0  # keep the browser open up to 10 minutes for login


# ---------------------------------------------------------------- utilities


def mask(value: str | None) -> str:
    if not value:
        return "(empty)"
    return f"{value[:6]}...{value[-4:]}(len={len(value)})"


def load_config() -> dict:
    return json.loads(CONFIG_PATH.read_text(encoding="utf-8"))


def save_config(updates: dict) -> None:
    data = load_config()
    data.update(updates)
    CONFIG_PATH.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    try:
        CONFIG_PATH.chmod(0o600)
    except OSError:
        pass


def expires_utc(expires_in) -> str:
    try:
        secs = int(expires_in)
        if secs > 0:
            return (
                datetime.datetime.now(datetime.timezone.utc)
                + datetime.timedelta(seconds=secs)
            ).strftime("%Y-%m-%dT%H:%M:%SZ")
    except (TypeError, ValueError):
        pass
    return ""


# ------------------------------------------------------------ raw websocket
# Minimal RFC6455 client - avoids any third-party dependency.


class CdpSocket:
    """Tiny WebSocket client speaking just enough CDP to read cookies."""

    def __init__(self, host: str, port: int, path: str = "/") -> None:
        self._sock = socket.create_connection((host, port), timeout=10)
        self._buf = b""
        key = base64.b64encode(os.urandom(16)).decode()
        req = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {host}:{port}\r\n"
            f"Upgrade: websocket\r\n"
            f"Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            f"Sec-WebSocket-Version: 13\r\n\r\n"
        )
        self._sock.sendall(req.encode())
        while b"\r\n\r\n" not in self._buf:
            chunk = self._sock.recv(4096)
            if not chunk:
                raise ConnectionError("websocket handshake failed")
            self._buf += chunk
        head, self._buf = self._buf.split(b"\r\n\r\n", 1)
        if b" 101 " not in head.split(b"\r\n")[0]:
            raise ConnectionError(f"websocket upgrade rejected: {head[:120]!r}")

    def _read_exact(self, n: int) -> bytes:
        while len(self._buf) < n:
            chunk = self._sock.recv(4096)
            if not chunk:
                raise ConnectionError("socket closed")
            self._buf += chunk
        out, self._buf = self._buf[:n], self._buf[n:]
        return out

    def send_json(self, obj: dict) -> None:
        payload = json.dumps(obj).encode()
        header = bytearray([0x81])  # FIN + text frame
        mask_bit = 0x80
        length = len(payload)
        if length < 126:
            header.append(mask_bit | length)
        elif length < 65536:
            header.append(mask_bit | 126)
            header += struct.pack(">H", length)
        else:
            header.append(mask_bit | 127)
            header += struct.pack(">Q", length)
        mask_key = os.urandom(4)
        header += mask_key
        masked = bytes(b ^ mask_key[i % 4] for i, b in enumerate(payload))
        self._sock.sendall(bytes(header) + masked)

    def recv_json(self, timeout: float = 5.0) -> dict:
        deadline = time.time() + timeout
        while True:
            remaining = deadline - time.time()
            if remaining <= 0:
                raise TimeoutError("websocket recv timeout")
            self._sock.settimeout(remaining)
            fin, opcode, payload = self._recv_frame()
            if opcode == 8:  # close
                raise ConnectionError("websocket closed by peer")
            if opcode == 9:  # ping -> pong
                self._send_pong(payload)
                continue
            if opcode == 10:  # pong
                continue
            # Continuation frames are not produced by CDP messages of our size.
            while not fin:
                fin, _op, more = self._recv_frame()
                payload += more
            return json.loads(payload.decode("utf-8", "replace"))

    def _send_pong(self, payload: bytes) -> None:
        header = bytearray([0x8A])  # FIN + pong
        length = len(payload)
        if length < 126:
            header.append(length)
        elif length < 65536:
            header.append(126)
            header += struct.pack(">H", length)
        else:
            header.append(127)
            header += struct.pack(">Q", length)
        self._sock.sendall(bytes(header) + payload)

    def _recv_frame(self) -> tuple[bool, int, bytes]:
        head = self._read_exact(2)
        fin = bool(head[0] & 0x80)
        opcode = head[0] & 0x0F
        length = head[1] & 0x7F
        if length == 126:
            length = struct.unpack(">H", self._read_exact(2))[0]
        elif length == 127:
            length = struct.unpack(">Q", self._read_exact(8))[0]
        if head[1] & 0x80:  # server never masks, but be tolerant
            mask_key = self._read_exact(4)
            data = self._read_exact(length)
            data = bytes(b ^ mask_key[i % 4] for i, b in enumerate(data))
        else:
            data = self._read_exact(length)
        return fin, opcode, data

    def call(self, method: str, params: dict | None = None, msg_id: int = 1) -> dict:
        self.send_json({"id": msg_id, "method": method, "params": params or {}})
        deadline = time.time() + 15
        while time.time() < deadline:
            msg = self.recv_json(timeout=max(0.1, deadline - time.time()))
            if msg.get("id") == msg_id:
                if "error" in msg:
                    raise RuntimeError(f"CDP {method}: {msg['error']}")
                return msg.get("result", {})
        raise TimeoutError(f"CDP {method}: no response")

    def close(self) -> None:
        try:
            self._sock.close()
        except OSError:
            pass


# ------------------------------------------------------------- edge control


def find_edge() -> str:
    for p in (
        r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
        r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
        shutil.which("msedge") or "",
    ):
        if p and Path(p).is_file():
            return p
    raise SystemExit("错误：找不到系统 Edge。")


def edge_free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def launch_edge(port: int, headless: bool) -> subprocess.Popen:
    exe = find_edge()
    cmd = [
        exe,
        f"--remote-debugging-port={port}",
        f"--user-data-dir={PROFILE_DIR}",
        "--no-first-run",
        "--no-default-browser-check",
        "--remote-allow-origins=*",
        # Minimal auth window: app mode strips tabs/address bar/toolbars,
        # fixed 800x640, and extensions stay out (they may hijack/redirect
        # the login page and break automated capture).
        "--window-size=800,640",
        "--disable-extensions",
    ]
    if headless:
        cmd += ["--headless=new", CONSOLE_URL]
    else:
        cmd += [f"--app={CONSOLE_URL}"]
    proc = subprocess.Popen(
        cmd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        stdin=subprocess.DEVNULL,
    )
    for _ in range(10):
        try:
            with urllib.request.urlopen(
                f"http://127.0.0.1:{port}/json/version", timeout=2
            ) as resp:
                resp.read()
            break
        except Exception:  # noqa: BLE001
            time.sleep(0.5)
    return proc


def cdp_ws_url(port: int) -> str:
    with urllib.request.urlopen(
        f"http://127.0.0.1:{port}/json/version", timeout=5
    ) as resp:
        info = json.loads(resp.read().decode("utf-8", "replace"))
    return info["webSocketDebuggerUrl"]


def read_ticket_via_cdp(port: int, timeout: float = 5.0) -> str | None:
    """Return the bk_ticket cookie value, or None if absent/expired."""
    url = cdp_ws_url(port)
    parsed = urllib.parse.urlparse(url)
    host = parsed.hostname or "127.0.0.1"
    ws_port = parsed.port or port
    sock = CdpSocket(host, ws_port, parsed.path or "/")
    try:
        result = sock.call(
            "Storage.getCookies", {"urls": [CONSOLE_URL]}, msg_id=1
        )
        for cookie in result.get("cookies", []):
            name = cookie.get("name", "")
            domain = cookie.get("domain") or ""
            if name == TICKET_NAME and ("woa.com" in domain or not domain):
                value = cookie.get("value") or ""
                if value:
                    return value
        return None
    finally:
        sock.close()


def navigate_to_console(port: int) -> None:  # kept for API compat, unused
    return None


# ------------------------------------------------------------ bkoauth calls


def _http(method: str, url: str, params: dict) -> tuple[int, str]:
    if method == "GET":
        req = urllib.request.Request(url + "?" + urllib.parse.urlencode(params))
    else:
        req = urllib.request.Request(
            url,
            data=urllib.parse.urlencode(params).encode("utf-8"),
            method=method,
        )
        req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001
        return -1, str(e)


def api_call(step: str, method: str, path: str, params: dict) -> dict:
    code, body = _http(method, f"{AUTH_BASE}{path}", params)
    try:
        parsed = json.loads(body)
    except json.JSONDecodeError:
        print(f"[{step}] HTTP {code} non-json: {body[:200]}")
        return {}
    ok = parsed.get("result") is True
    print(
        f"[{step}] HTTP {code} result={parsed.get('result')} "
        f"code={parsed.get('code')} msg={str(parsed.get('message') or '')[:120]}"
    )
    if not ok:
        return {}
    return parsed.get("data") or {}


def exchange_ticket(app_code: str, app_secret: str, rtx: str, ticket: str) -> dict:
    return api_call("exchange", "POST", "/token/", {
        "app_code": app_code,
        "app_secret": app_secret,
        "grant_type": "authorization_code",
        "rtx": rtx,
        "bk_ticket": ticket,
        "env_name": "prod",
        "need_new_token": 0,
    })


def check_token(app_code: str, app_secret: str, access_token: str) -> dict:
    return api_call("check", "GET", "/check_token/", {
        "app_code": app_code,
        "app_secret": app_secret,
        "access_token": access_token,
    })


def refresh_token(app_code: str, app_secret: str, refresh_token_value: str) -> dict:
    return api_call("refresh", "GET", "/refresh_token/", {
        "app_code": app_code,
        "app_secret": app_secret,
        "grant_type": "refresh_token",
        "refresh_token": refresh_token_value,
        "need_new_token": 0,
        "env_name": "prod",
    })


# ------------------------------------------------------------------- flow


def persist_tokens(access: str, refresh: str, expires: str) -> None:
    save_config({
        "access_token": access,
        "auth": {
            "access_token": access,
            "refresh_token": refresh,
            "access_expires_utc": expires,
        },
    })


def wait_for_ticket(port: int, headless: bool) -> str | None:
    deadline = time.time() + MAX_LOGIN_WAIT
    attempt = 0
    while time.time() < deadline:
        attempt += 1
        try:
            ticket = read_ticket_via_cdp(port)
        except Exception as e:  # noqa: BLE001
            print(f"[poll#{attempt}] CDP read failed: {e}")
            ticket = None
        if ticket:
            print(f"[poll#{attempt}] captured bk_ticket {mask(ticket)}")
            return ticket
        if attempt == 1:
            if headless:
                print("headless: no live ticket in profile, cannot open login page")
                return None
            print(
                "未登录（或票据失效）。浏览器窗口已打开蓝盾控制台，\n"
                "请在窗口中完成登录（扫码/账号均可），脚本每 2 秒自动检测…"
            )
        time.sleep(POLL_INTERVAL)
    return None


def main() -> int:
    parser = argparse.ArgumentParser(description="蓝盾自动登录换票（脚本自闭环）")
    parser.add_argument(
        "--headless",
        action="store_true",
        help="无窗口模式：仅当 profile 里票据仍存活时有效",
    )
    args = parser.parse_args()

    cfg = load_config()
    app_code = str(cfg.get("app_code") or "").strip()
    app_secret = str(cfg.get("app_secret") or "").strip()
    rtx = str(cfg.get("user_id") or "").strip()
    if not (app_code and app_secret and rtx):
        raise SystemExit("错误：config.json 缺 app_code / app_secret / user_id。")

    port = edge_free_port()
    print(f"[edge] launching on CDP port {port} (profile: {PROFILE_DIR.name})")
    proc = launch_edge(port, headless=args.headless)
    try:
        ticket = wait_for_ticket(port, headless=args.headless)
        if not ticket:
            print("未能在等待窗口内拿到 bk_ticket，退出。")
            return 1

        data = exchange_ticket(app_code, app_secret, rtx, ticket)
        access = str(data.get("access_token") or "")
        refresh = str(data.get("refresh_token") or "")
        if not (access and refresh):
            print("换票失败：未拿到 access_token/refresh_token。")
            return 2
        print(f"[exchange] access={mask(access)} refresh={mask(refresh)}")

        persist_tokens(access, refresh, expires_utc(data.get("expires_in")))

        check = check_token(app_code, app_secret, access)
        if check:
            print(f"[check] ok, expires_in={check.get('expires_in')}")
        else:
            print("[check] 失败（token 已保存，但校验未过，请反馈日志）")
            return 3

        rdata = refresh_token(app_code, app_secret, refresh)
        new_access = str(rdata.get("access_token") or "")
        if new_access:
            new_refresh = str(rdata.get("refresh_token") or "")
            persist_tokens(
                new_access, new_refresh or refresh, expires_utc(rdata.get("expires_in"))
            )
            print(f"[refresh] ok, access={mask(new_access)}")
        else:
            print("[refresh] 失败（换票成果已保存，续期链路待修）")

        print("ALL GREEN - 登录换票三件套闭环完成，config.json 已更新。")
        return 0
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()


if __name__ == "__main__":
    sys.exit(main())

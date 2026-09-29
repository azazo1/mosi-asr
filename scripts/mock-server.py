#!/usr/bin/env python3
"""Moss API 模拟服务, 用于本地调试与冒烟测试, 不会产生真实调用.

用法:
    python3 scripts/mock-server.py [端口]

默认监听 127.0.0.1:18080, 把配置里的 api.base_url 指向它即可离线跑通全流程.
"""

import json
import re
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

TASKS = {}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _read_form(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        text = body.decode("utf-8", "replace")
        fields = {}
        for m in re.finditer(r'name="([^"]+)"\r\n\r\n(.*?)\r\n--', text, re.S):
            fields[m.group(1)] = m.group(2)
        fields["_has_file"] = "filename=" in text
        return fields

    def do_POST(self):
        form = self._read_form()
        if form.get("stream") == "true":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            events = [
                {"type": "task.created", "task_id": "mock-stream", "status": "PROCESSING"},
                {"type": "transcript.text.delta", "delta": "这是"},
                {"type": "transcript.text.delta", "delta": "一段流式"},
                {"type": "transcript.segment.done", "start": 0.0, "end": 2.5, "text": "这是一段流式", "speaker": "S01"},
                {"type": "transcript.text.done", "text": "这是一段流式结果"},
            ]
            for e in events:
                self.wfile.write(("data: " + json.dumps(e, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
            return

        if form.get("async") == "true":
            TASKS["mock-task-1"] = True
            payload = {"id": "mock-task-1", "task_id": "mock-task-1", "status": "PENDING", "retry_after": 1}
        else:
            payload = {
                "task": "transcribe",
                "duration": 6.5,
                "text": "甲说了话乙回了话",
                "segments": [
                    {"start": 0.0, "end": 3.0, "text": "甲说了话", "speaker": "S01"},
                    {"start": 3.0, "end": 6.5, "text": "乙回了话", "speaker": "S02"},
                ],
                "echo": {"model": form.get("model"), "diarize": form.get("diarize"),
                         "file": form.get("_has_file"), "keyterms": form.get("keyterms")},
            }
        raw = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.startswith("/v1/audio/tasks/"):
            payload = {"id": "mock-task-1", "status": "SUCCESS", "text": "异步任务结果",
                       "segments": [{"start": 0, "end": 2, "text": "异步任务结果", "speaker": "S01"}]}
            raw = json.dumps(payload, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        self.send_response(404)
        self.end_headers()


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 18080
    HTTPServer(("127.0.0.1", port), Handler).serve_forever()

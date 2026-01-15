#!/usr/bin/env python3
"""
mitmproxy addon for capturing Seafile protocol traffic in detail.
Saves all requests and responses to JSON files for analysis.
"""

import json
import os
import time
import binascii
import zlib
from datetime import datetime
from mitmproxy import ctx, http

CAPTURE_DIR = os.environ.get("CAPTURE_DIR", "/captures")

class SeafileCapture:
    def __init__(self):
        self.request_counter = 0
        self.session_dir = None
        self.flows = []

    def start_session(self):
        """Create a new capture session directory."""
        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        self.session_dir = os.path.join(CAPTURE_DIR, f"session_{timestamp}")
        os.makedirs(self.session_dir, exist_ok=True)
        ctx.log.info(f"Capture session started: {self.session_dir}")

    def get_session_dir(self):
        if not self.session_dir:
            self.start_session()
        return self.session_dir

    def format_headers(self, headers):
        """Convert headers to dict."""
        return dict(headers.items())

    def try_decode_body(self, body, content_type=""):
        """Try to decode body content."""
        if not body:
            return {"type": "empty", "data": None}

        # Try JSON
        try:
            return {"type": "json", "data": json.loads(body.decode('utf-8'))}
        except:
            pass

        # Try plain text
        try:
            text = body.decode('utf-8')
            if text.isprintable() or '\n' in text:
                return {"type": "text", "data": text}
        except:
            pass

        # Check for zlib compressed data
        if len(body) > 2 and body[0:2] == b'\x78\x9c':
            try:
                decompressed = zlib.decompress(body)
                return {
                    "type": "zlib_compressed",
                    "compressed_size": len(body),
                    "decompressed_size": len(decompressed),
                    "data": self.try_decode_body(decompressed)
                }
            except:
                pass

        # Check for pack-fs format (40-byte hex ID + 4-byte size + zlib data)
        if len(body) >= 44:
            try:
                # Try to parse as pack-fs format
                hex_id = body[:40].decode('ascii')
                if all(c in '0123456789abcdef' for c in hex_id):
                    size = int.from_bytes(body[40:44], 'big')
                    entries = []
                    offset = 0
                    while offset + 44 <= len(body):
                        entry_id = body[offset:offset+40].decode('ascii')
                        entry_size = int.from_bytes(body[offset+40:offset+44], 'big')
                        if offset + 44 + entry_size <= len(body):
                            entry_data = body[offset+44:offset+44+entry_size]
                            try:
                                decompressed = zlib.decompress(entry_data)
                                try:
                                    parsed = json.loads(decompressed.decode('utf-8'))
                                    entries.append({
                                        "fs_id": entry_id,
                                        "size": entry_size,
                                        "decompressed": parsed
                                    })
                                except:
                                    entries.append({
                                        "fs_id": entry_id,
                                        "size": entry_size,
                                        "decompressed_text": decompressed.decode('utf-8', errors='replace')
                                    })
                            except:
                                entries.append({
                                    "fs_id": entry_id,
                                    "size": entry_size,
                                    "raw_hex": binascii.hexlify(entry_data[:100]).decode() + ("..." if len(entry_data) > 100 else "")
                                })
                        offset += 44 + entry_size
                    if entries:
                        return {
                            "type": "pack_fs_format",
                            "total_size": len(body),
                            "entries": entries
                        }
            except:
                pass

        # Binary data - return hex and length
        return {
            "type": "binary",
            "size": len(body),
            "hex_preview": binascii.hexlify(body[:200]).decode() + ("..." if len(body) > 200 else ""),
            "full_hex": binascii.hexlify(body).decode() if len(body) < 10000 else None
        }

    def request(self, flow: http.HTTPFlow) -> None:
        """Called when a request is received."""
        self.request_counter += 1
        flow.metadata["capture_id"] = self.request_counter
        flow.metadata["start_time"] = time.time()

    def response(self, flow: http.HTTPFlow) -> None:
        """Called when a response is received."""
        capture_id = flow.metadata.get("capture_id", 0)
        start_time = flow.metadata.get("start_time", time.time())
        duration = time.time() - start_time

        # Build capture record
        record = {
            "id": capture_id,
            "timestamp": datetime.now().isoformat(),
            "duration_ms": round(duration * 1000, 2),
            "request": {
                "method": flow.request.method,
                "url": flow.request.pretty_url,
                "path": flow.request.path,
                "host": flow.request.host,
                "port": flow.request.port,
                "http_version": flow.request.http_version,
                "headers": self.format_headers(flow.request.headers),
                "content_length": len(flow.request.content) if flow.request.content else 0,
                "body": self.try_decode_body(flow.request.content,
                    flow.request.headers.get("content-type", ""))
            },
            "response": {
                "status_code": flow.response.status_code,
                "reason": flow.response.reason,
                "http_version": flow.response.http_version,
                "headers": self.format_headers(flow.response.headers),
                "content_length": len(flow.response.content) if flow.response.content else 0,
                "body": self.try_decode_body(flow.response.content,
                    flow.response.headers.get("content-type", ""))
            }
        }

        self.flows.append(record)

        # Save individual request file
        session_dir = self.get_session_dir()
        filename = f"{capture_id:04d}_{flow.request.method}_{flow.request.path.replace('/', '_')[:50]}.json"
        filepath = os.path.join(session_dir, filename)

        with open(filepath, 'w') as f:
            json.dump(record, f, indent=2, default=str)

        # Log summary
        ctx.log.info(f"[{capture_id}] {flow.request.method} {flow.request.path} -> {flow.response.status_code}")

    def done(self):
        """Called when mitmproxy is shutting down."""
        if self.flows:
            session_dir = self.get_session_dir()
            summary_file = os.path.join(session_dir, "session_summary.json")
            with open(summary_file, 'w') as f:
                json.dump({
                    "total_requests": len(self.flows),
                    "flows": self.flows
                }, f, indent=2, default=str)
            ctx.log.info(f"Session saved: {len(self.flows)} requests captured")


addons = [SeafileCapture()]

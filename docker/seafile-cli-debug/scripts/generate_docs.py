#!/usr/bin/env python3
"""
Generate Seafile Protocol Documentation from captured traffic.
Reads JSON files from capture sessions and generates Markdown documentation.
"""

import os
import json
import glob
from datetime import datetime
from collections import defaultdict

CAPTURE_DIR = os.environ.get("CAPTURE_DIR", "/captures")


def load_session(session_dir):
    """Load all captured requests from a session."""
    flows = []
    for filepath in sorted(glob.glob(os.path.join(session_dir, "*.json"))):
        if "session_summary" in filepath:
            continue
        try:
            with open(filepath, 'r') as f:
                flows.append(json.load(f))
        except:
            pass
    return flows


def group_by_endpoint(flows):
    """Group flows by endpoint pattern."""
    groups = defaultdict(list)
    for flow in flows:
        path = flow['request']['path']
        method = flow['request']['method']

        # Normalize paths (replace UUIDs, commit IDs, etc.)
        normalized = path
        # UUID pattern
        import re
        normalized = re.sub(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}',
                           '{repo_id}', normalized)
        # SHA1 pattern (40 hex chars)
        normalized = re.sub(r'/[a-f0-9]{40}(?=/|$|\?)', '/{commit_id}', normalized)

        key = f"{method} {normalized}"
        groups[key].append(flow)

    return groups


def format_body(body_info, max_length=2000):
    """Format body info for documentation."""
    if not body_info:
        return "*Empty*"

    body_type = body_info.get('type', 'unknown')

    if body_type == 'empty':
        return "*Empty*"

    if body_type == 'json':
        data = body_info.get('data', {})
        formatted = json.dumps(data, indent=2)
        if len(formatted) > max_length:
            formatted = formatted[:max_length] + "\n... (truncated)"
        return f"```json\n{formatted}\n```"

    if body_type == 'text':
        text = body_info.get('data', '')
        if len(text) > max_length:
            text = text[:max_length] + "\n... (truncated)"
        return f"```\n{text}\n```"

    if body_type == 'pack_fs_format':
        entries = body_info.get('entries', [])
        lines = [f"**Binary pack-fs format** ({body_info.get('total_size', 0)} bytes total)"]
        lines.append("")
        lines.append("| FS ID | Compressed Size | Content Type |")
        lines.append("|-------|-----------------|--------------|")
        for entry in entries[:10]:
            fs_id = entry.get('fs_id', '')[:20] + "..."
            size = entry.get('size', 0)
            if 'decompressed' in entry:
                content = str(type(entry['decompressed']).__name__)
            else:
                content = "binary"
            lines.append(f"| `{fs_id}` | {size} | {content} |")
        if len(entries) > 10:
            lines.append(f"| ... | ... | ({len(entries) - 10} more entries) |")
        return "\n".join(lines)

    if body_type == 'zlib_compressed':
        nested = body_info.get('data', {})
        return f"**Zlib compressed** ({body_info.get('compressed_size', 0)} → {body_info.get('decompressed_size', 0)} bytes)\n\n{format_body(nested)}"

    if body_type == 'binary':
        size = body_info.get('size', 0)
        hex_preview = body_info.get('hex_preview', '')[:100]
        return f"**Binary data** ({size} bytes)\n\n```\nHex preview: {hex_preview}...\n```"

    return f"*Unknown type: {body_type}*"


def generate_endpoint_doc(method_path, flows):
    """Generate documentation for a single endpoint."""
    lines = []

    # Get first flow as example
    flow = flows[0]
    req = flow['request']
    resp = flow['response']

    # Endpoint header
    lines.append(f"### {method_path}")
    lines.append("")

    # Request info
    lines.append("**Request:**")
    lines.append("")
    lines.append(f"- Method: `{req['method']}`")
    lines.append(f"- URL: `{req['url']}`")

    # Headers
    important_headers = ['Authorization', 'Seafile-Repo-Token', 'Content-Type', 'Accept']
    req_headers = {k: v for k, v in req['headers'].items() if k in important_headers}
    if req_headers:
        lines.append("- Headers:")
        for k, v in req_headers.items():
            # Mask tokens
            if 'token' in k.lower() or 'authorization' in k.lower():
                v = v[:30] + "..." if len(v) > 30 else v
            lines.append(f"  - `{k}: {v}`")

    # Request body
    if req.get('content_length', 0) > 0:
        lines.append("")
        lines.append("**Request Body:**")
        lines.append("")
        lines.append(format_body(req.get('body')))

    lines.append("")

    # Response info
    lines.append("**Response:**")
    lines.append("")
    lines.append(f"- Status: `{resp['status_code']} {resp.get('reason', '')}`")

    # Response headers
    resp_headers = {k: v for k, v in resp['headers'].items()
                   if k.lower() in ['content-type', 'content-length', 'transfer-encoding']}
    if resp_headers:
        lines.append("- Headers:")
        for k, v in resp_headers.items():
            lines.append(f"  - `{k}: {v}`")

    # Response body
    lines.append("")
    lines.append("**Response Body:**")
    lines.append("")
    lines.append(format_body(resp.get('body')))

    lines.append("")
    lines.append("---")
    lines.append("")

    return "\n".join(lines)


def generate_protocol_doc(session_dir):
    """Generate full protocol documentation."""
    flows = load_session(session_dir)

    if not flows:
        return "No captured flows found."

    groups = group_by_endpoint(flows)

    doc = []

    # Header
    doc.append("# Seafile Sync Protocol - Reverse Engineered Documentation")
    doc.append("")
    doc.append(f"*Generated: {datetime.now().isoformat()}*")
    doc.append("")
    doc.append("This document describes the Seafile desktop/mobile client sync protocol as observed from")
    doc.append("captured network traffic against a production Seafile server.")
    doc.append("")

    # Table of contents
    doc.append("## Table of Contents")
    doc.append("")

    # Group endpoints by category
    categories = {
        'Authentication': [],
        'Server Info': [],
        'Library Operations': [],
        'Sync Protocol': [],
        'File Operations': [],
        'Other': []
    }

    for endpoint in sorted(groups.keys()):
        if 'auth-token' in endpoint or 'account' in endpoint:
            categories['Authentication'].append(endpoint)
        elif 'server-info' in endpoint or 'protocol-version' in endpoint:
            categories['Server Info'].append(endpoint)
        elif '/repos/' in endpoint and 'seafhttp' not in endpoint:
            categories['Library Operations'].append(endpoint)
        elif 'seafhttp' in endpoint:
            categories['Sync Protocol'].append(endpoint)
        else:
            categories['Other'].append(endpoint)

    for cat, endpoints in categories.items():
        if endpoints:
            doc.append(f"- [{cat}](#{cat.lower().replace(' ', '-')})")
            for ep in endpoints:
                doc.append(f"  - [{ep}](#{ep.lower().replace(' ', '-').replace('/', '').replace('{', '').replace('}', '').replace('?', '')})")
    doc.append("")

    # Generate each category
    for cat, endpoints in categories.items():
        if not endpoints:
            continue

        doc.append(f"## {cat}")
        doc.append("")

        for endpoint in endpoints:
            doc.append(generate_endpoint_doc(endpoint, groups[endpoint]))

    return "\n".join(doc)


def main():
    # Find most recent session
    sessions = sorted(glob.glob(os.path.join(CAPTURE_DIR, "session_*")))

    if not sessions:
        print("No capture sessions found.")
        print(f"Run 'seaf-debug.sh capture-all' first to capture protocol traffic.")
        return

    latest_session = sessions[-1]
    print(f"Generating documentation from: {latest_session}")

    doc = generate_protocol_doc(latest_session)

    # Save to file
    output_file = os.path.join(CAPTURE_DIR, "SEAFILE-PROTOCOL.md")
    with open(output_file, 'w') as f:
        f.write(doc)

    print(f"Documentation saved to: {output_file}")
    print(f"\nPreview (first 100 lines):")
    print("-" * 60)
    for line in doc.split('\n')[:100]:
        print(line)


if __name__ == "__main__":
    main()

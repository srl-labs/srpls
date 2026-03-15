#!/usr/bin/env python3
"""
Collects interface names from running containerlab SR Linux nodes
via sr_cli (docker exec), then writes per-platform interface lists to files.

Prerequisites: containerlab, docker, python3
Usage:
    python3 collect-interfaces.py
"""

import json
import re
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent
OUTPUT_DIR = SCRIPT_DIR / "interfaces"


def run(cmd, **kwargs):
    """Run a command and return the result."""
    return subprocess.run(cmd, capture_output=True, text=True, **kwargs)


def get_nodes():
    """Get node names and types from the running topology."""
    result = run(["sudo", "containerlab", "inspect", "--details", "--format", "json"])
    if result.returncode != 0:
        print(f"ERROR: Failed to inspect topology:\n{result.stderr}")
        sys.exit(1)

    data = json.loads(result.stdout)
    nodes = {}
    containers = data.get("containers", [])
    if not containers:
        for v in data.values():
            if isinstance(v, list):
                containers.extend(v)
    for container in containers:
        labels = container.get("Labels", {})
        name = labels["clab-node-longname"]
        shortname = labels["clab-node-type"]
        nodes[name] = {"platform": labels["clab-node-name"], "shortname": shortname}
    return nodes


def wait_for_node(node, max_retries=30):
    """Wait until a node responds to sr_cli."""
    for i in range(max_retries):
        result = run(["docker", "exec", node, "sr_cli", "--", "show version"])
        if result.returncode == 0:
            return True
        time.sleep(5)
    return False


def collect_interfaces(node):
    """Query a node for all interface names via sr_cli."""
    result = run([
        "docker", "exec", node,
        "sr_cli", "--output-format", "json", "--", "show interface brief",
    ])

    if result.returncode != 0:
        return []

    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError:
        return []

    names = []
    for iface in data.get("IfBrief", []):
        name = iface.get("Port")
        if name:
            names.append(name)
    return sorted(names, key=interface_sort_key)


def interface_sort_key(name):
    """Sort interfaces: system, mgmt, lo, ethernet (by slot/port numbers)."""
    order = {"system": 0, "mgmt": 1, "lo": 2, "ethernet": 3, "irb": 4}
    for prefix, rank in order.items():
        if name.startswith(prefix):
            nums = [int(x) for x in re.findall(r"\d+", name)]
            return (rank, *nums)
    return (99, name)


def collect_node(container_name, node_info):
    """Wait for a node and collect its interfaces."""
    platform = node_info["platform"]
    if not wait_for_node(container_name):
        print(f"    WARNING: {platform} not reachable, skipping")
        return node_info, []

    ifaces = collect_interfaces(container_name)
    if ifaces:
        print(f"    {platform}: found {len(ifaces)} interfaces")
    else:
        print(f"    WARNING: {platform}: no interfaces found")
    return node_info, ifaces


def main():
    nodes = get_nodes()
    print(f"==> Found {len(nodes)} nodes")
    print("==> Collecting interfaces from all nodes...")

    OUTPUT_DIR.mkdir(exist_ok=True)

    with ThreadPoolExecutor() as pool:
        futures = {
            pool.submit(collect_node, cname, info): info
            for cname, info in nodes.items()
        }

        for future in as_completed(futures):
            node_info, ifaces = future.result()
            if ifaces:
                out = OUTPUT_DIR / f"{node_info['platform']}.json"
                out.write_text(json.dumps({
                    "platform": node_info["platform"],
                    "shortname": node_info["shortname"],
                    "interfaces": ifaces,
                }, indent=2) + "\n")

    written = sorted(OUTPUT_DIR.glob("*.json"))
    if not written:
        print("ERROR: No interfaces collected from any node")
        sys.exit(1)

    print(f"==> Wrote {len(written)} files to {OUTPUT_DIR}/")
    print("==> Done!")


if __name__ == "__main__":
    main()
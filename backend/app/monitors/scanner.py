"""Network IP scanner.

Discovers live hosts on a subnet using a concurrent ping sweep, then enriches
each live host with its MAC address (read from the local ARP/neighbour table -
no root required) and reverse-DNS hostname.

Works on Raspberry Pi OS / Linux without elevated privileges: pinging populates
the kernel ARP cache, which we then read from ``ip neigh`` or ``/proc/net/arp``.
"""

from __future__ import annotations

import ipaddress
import logging
import re
import socket
import subprocess
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional

from . import ping as ping_mod

log = logging.getLogger(__name__)

# Hard cap so an over-broad subnet can't launch a runaway scan.
MAX_HOSTS = 1024


@dataclass
class ScannedHost:
    ip: str
    is_up: bool
    mac: Optional[str] = None
    hostname: Optional[str] = None
    latency_ms: Optional[float] = None


def default_subnet() -> str:
    """Best-effort guess of the local /24 subnet (e.g. 192.168.0.0/24)."""
    ip = "192.168.0.1"
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
    except Exception:
        pass
    try:
        net = ipaddress.ip_network(ip + "/24", strict=False)
        return str(net)
    except ValueError:
        return "192.168.0.0/24"


def _hosts_for(subnet: str) -> List[str]:
    net = ipaddress.ip_network(subnet, strict=False)
    return [str(h) for h in net.hosts()]


def _read_arp_table() -> Dict[str, str]:
    """Return {ip: mac} from the system neighbour/ARP table."""
    table: Dict[str, str] = {}
    # Preferred: `ip neigh`
    try:
        out = subprocess.run(
            ["ip", "neigh"], capture_output=True, text=True, timeout=5
        ).stdout
        for line in out.splitlines():
            parts = line.split()
            if "lladdr" in parts:
                ip = parts[0]
                mac = parts[parts.index("lladdr") + 1]
                if _valid_mac(mac):
                    table[ip] = mac.lower()
    except Exception:
        pass
    # Fallback: /proc/net/arp
    if not table:
        try:
            arp = Path("/proc/net/arp")
            if arp.exists():
                for line in arp.read_text().splitlines()[1:]:
                    cols = line.split()
                    if len(cols) >= 4 and _valid_mac(cols[3]):
                        table[cols[0]] = cols[3].lower()
        except Exception:
            pass
    return table


def _valid_mac(mac: str) -> bool:
    return bool(re.fullmatch(r"([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}", mac or "")) and mac != "00:00:00:00:00:00"


def _reverse_dns(ip: str) -> Optional[str]:
    try:
        return socket.gethostbyaddr(ip)[0]
    except Exception:
        return None


def scan_subnet(
    subnet: str,
    method: str = "icmp",
    timeout: float = 1.0,
    concurrency: int = 64,
) -> List[ScannedHost]:
    """Scan ``subnet`` and return the live hosts with MAC/hostname enrichment."""
    hosts = _hosts_for(subnet)
    if len(hosts) > MAX_HOSTS:
        raise ValueError(
            f"서브넷이 너무 큽니다 ({len(hosts)}개 호스트). 최대 {MAX_HOSTS}개까지 지원합니다."
        )

    def _probe(ip: str) -> ScannedHost:
        res = ping_mod.check(ip, method, port=None, timeout=timeout)
        return ScannedHost(ip=ip, is_up=res.is_up, latency_ms=res.latency_ms)

    workers = max(1, min(concurrency, len(hosts)))
    results: List[ScannedHost] = []
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for host in pool.map(_probe, hosts):
            if host.is_up:
                results.append(host)

    # Enrich live hosts (ARP table is now warm from the sweep).
    arp = _read_arp_table()
    for host in results:
        host.mac = arp.get(host.ip)
        host.hostname = _reverse_dns(host.ip)

    log.info("Scan of %s: %d/%d hosts up", subnet, len(results), len(hosts))
    return results

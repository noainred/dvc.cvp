"""Router / access-point monitoring.

Every router is at minimum monitored for reachability (ICMP). When the router
exposes SNMP (common on prosumer gear; many consumer routers like ipTIME can
enable it) we additionally read system description, uptime and WAN interface
counters to compute live throughput.

SNMP support requires the optional ``pysnmp-lextudio`` package and is fully
optional - everything degrades gracefully to ping-only.
"""

from __future__ import annotations

import logging
import time
from typing import Any, Dict, Optional, Tuple

from . import ping as ping_mod

log = logging.getLogger(__name__)

# Standard SNMP OIDs
_OID_SYS_DESCR = "1.3.6.1.2.1.1.1.0"
_OID_SYS_UPTIME = "1.3.6.1.2.1.1.3.0"  # TimeTicks (1/100 s)
_OID_IF_IN = "1.3.6.1.2.1.2.2.1.10"  # ifInOctets.<idx>
_OID_IF_OUT = "1.3.6.1.2.1.2.2.1.16"  # ifOutOctets.<idx>

# In-memory store of previous WAN counters for throughput calculation:
#   {router_host: (timestamp, in_octets, out_octets)}
_prev_counter: Dict[str, Tuple[float, int, int]] = {}


def _snmp_get(host: str, community: str, oid: str, port: int = 161, timeout: int = 2) -> Optional[str]:
    try:
        from pysnmp.hlapi import (  # type: ignore
            CommunityData,
            ContextData,
            ObjectIdentity,
            ObjectType,
            SnmpEngine,
            UdpTransportTarget,
            getCmd,
        )
    except Exception:
        return None

    try:
        iterator = getCmd(
            SnmpEngine(),
            CommunityData(community, mpModel=1),
            UdpTransportTarget((host, port), timeout=timeout, retries=1),
            ContextData(),
            ObjectType(ObjectIdentity(oid)),
        )
        error_indication, error_status, _, var_binds = next(iterator)
        if error_indication or error_status:
            return None
        for _, val in var_binds:
            return str(val)
    except Exception as exc:  # pragma: no cover - network dependent
        log.debug("snmp get failed for %s %s: %s", host, oid, exc)
    return None


def get_status(cfg: Dict[str, Any]) -> Dict[str, Any]:
    host = cfg.get("host", "")
    out: Dict[str, Any] = {
        "host": host,
        "admin_url": cfg.get("admin_url") or (f"http://{host}" if host else None),
        "reachable": False,
        "latency_ms": None,
        "snmp": False,
    }
    if not host:
        return out

    res = ping_mod.check(host, "icmp", timeout=float(cfg.get("timeout_seconds", 2)))
    out["reachable"] = res.is_up
    out["latency_ms"] = res.latency_ms

    if not cfg.get("snmp_enabled"):
        return out

    community = cfg.get("snmp_community", "public")
    port = int(cfg.get("snmp_port", 161))

    descr = _snmp_get(host, community, _OID_SYS_DESCR, port)
    if descr is None:
        # SNMP unavailable / library missing
        return out

    out["snmp"] = True
    out["sys_descr"] = descr
    uptime_ticks = _snmp_get(host, community, _OID_SYS_UPTIME, port)
    if uptime_ticks and uptime_ticks.isdigit():
        out["uptime_s"] = int(uptime_ticks) // 100

    wan_idx = cfg.get("wan_if_index")
    if wan_idx is not None:
        in_oct = _snmp_get(host, community, f"{_OID_IF_IN}.{wan_idx}", port)
        out_oct = _snmp_get(host, community, f"{_OID_IF_OUT}.{wan_idx}", port)
        if (in_oct or "").isdigit() and (out_oct or "").isdigit():
            now = time.time()
            cur = (now, int(in_oct), int(out_oct))
            prev = _prev_counter.get(host)
            _prev_counter[host] = cur
            if prev:
                dt = now - prev[0]
                if dt > 0:
                    # handle 32-bit counter wrap
                    d_in = (cur[1] - prev[1]) % (2**32)
                    d_out = (cur[2] - prev[2]) % (2**32)
                    out["wan_down_mbps"] = round(d_in * 8 / dt / 1_000_000, 2)
                    out["wan_up_mbps"] = round(d_out * 8 / dt / 1_000_000, 2)
            out["wan_in_octets"] = cur[1]
            out["wan_out_octets"] = cur[2]

    return out

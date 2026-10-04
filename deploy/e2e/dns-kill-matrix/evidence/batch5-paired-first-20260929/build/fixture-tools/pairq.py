# Read-only: paired-secondary DNS answers. SOA and www A of s1-kill.test and the
# peer catalog SOA, over UDP and TCP, to the secondary (192.0.2.10), the native
# primary (192.0.2.11) and 127.0.0.1. With "loop OUTFILE SECONDS" it is a compact
# sampler (both pair addresses every ~2 s); default prints one JSON line per query.
import socket, struct, random, json, datetime, sys, time
CATALOG = "catalog-c000020b.celikpanel.invalid"


def skip_name(d, o):
    while True:
        l = d[o]
        if l & 0xC0 == 0xC0:
            return o + 2
        if l == 0:
            return o + 1
        o += 1 + l


def recv_exact(s, n):
    data = b""
    while len(data) < n:
        c = s.recv(n - len(data))
        if not c:
            raise OSError("tcp closed")
        data += c
    return data


def q(name, qtype, tcp, server, timeout=3):
    tid = random.randrange(65536)
    msg = struct.pack(">HHHHHH", tid, 0, 1, 0, 0, 0) + b"".join(
        bytes([len(p)]) + p.encode() for p in name.split(".")) + b"\0" + struct.pack(">HH", qtype, 1)
    if tcp:
        s = socket.create_connection((server, 53), timeout)
        s.settimeout(timeout)
        s.sendall(struct.pack(">H", len(msg)) + msg)
        ln = struct.unpack(">H", recv_exact(s, 2))[0]
        data = recv_exact(s, ln)
        s.close()
    else:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(timeout)
        s.sendto(msg, (server, 53))
        data, _ = s.recvfrom(4096)
        s.close()
    rid, flags, qd, an, ns, ar = struct.unpack(">HHHHHH", data[:12])
    off = skip_name(data, 12) + 4
    out = []
    for _ in range(an):
        off = skip_name(data, off)
        t, c, ttl, rl = struct.unpack(">HHIH", data[off:off + 10])
        off += 10
        if t == 1 and qtype == 1:
            out.append(socket.inet_ntoa(data[off:off + rl]))
        if t == 6 and qtype == 6:
            p = skip_name(data, off)
            p = skip_name(data, p)
            out.append(struct.unpack(">I", data[p:p + 4])[0])
        off += rl
    return {"id_match": rid == tid, "aa": bool(flags & 0x0400), "tc": bool(flags & 0x0200),
            "rcode": flags & 0xF, "answers": out}


QUERIES = [("s1-kill.test", 6), ("www.s1-kill.test", 1), (CATALOG, 6)]


def one_round(servers, timeout=3):
    rows = []
    for server in servers:
        for name, qtype in QUERIES:
            for tcp in (False, True):
                r = {"server": server, "qname": name, "qtype": "SOA" if qtype == 6 else "A",
                     "transport": "tcp" if tcp else "udp"}
                try:
                    r.update(q(name, qtype, tcp, server, timeout))
                except Exception as e:
                    r["error"] = f"{type(e).__name__}: {e}"
                rows.append(r)
    return rows


if len(sys.argv) >= 2 and sys.argv[1] == "loop":
    out, seconds = sys.argv[2], float(sys.argv[3])
    end = time.time() + seconds
    with open(out, "a", buffering=1) as f:
        while time.time() < end:
            ts = datetime.datetime.now(datetime.timezone.utc).isoformat()
            compact = []
            for r in one_round(["192.0.2.10", "192.0.2.11"], timeout=1):
                if "error" in r:
                    v = "ERR(" + r["error"].split(":")[0] + ")"
                else:
                    v = ("aa" if r["aa"] else "noaa") + "/rc" + str(r["rcode"]) + "/" + ",".join(map(str, r["answers"]))
                short = {"s1-kill.test": "soa", "www.s1-kill.test": "www", CATALOG: "cat"}[r["qname"]]
                compact.append(f"{r['server'].split('.')[-1]}:{short}:{r['transport']}={v}")
            f.write(ts + " " + " ".join(compact) + "\n")
            time.sleep(2)
else:
    print("wall=" + datetime.datetime.now(datetime.timezone.utc).isoformat())
    for r in one_round(["192.0.2.10", "192.0.2.11", "127.0.0.1"]):
        print(json.dumps(r))

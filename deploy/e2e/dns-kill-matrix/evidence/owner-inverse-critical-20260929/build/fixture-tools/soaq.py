# Read-only: SOA query for the fixture zone over UDP and TCP; prints serial per server/transport.
import socket, struct, random, json, datetime
ZONE = "s1-kill.test"
def skip_name(data, off):
    while True:
        l = data[off]
        if l & 0xC0 == 0xC0: return off + 2
        if l == 0: return off + 1
        off += 1 + l
def read_name(data, off):
    labels = []; jumps = 0
    while True:
        l = data[off]
        if l & 0xC0 == 0xC0:
            off = ((l & 0x3F) << 8) | data[off+1]; jumps += 1
            if jumps > 20: raise ValueError("pointer loop")
            continue
        if l == 0: break
        labels.append(data[off+1:off+1+l].decode()); off += 1 + l
    return ".".join(labels) + "."
def q(tcp, server):
    tid = random.randrange(65536)
    msg = struct.pack(">HHHHHH", tid, 0, 1, 0, 0, 0) + b"".join(bytes([len(p)])+p.encode() for p in ZONE.split(".")) + b"\0" + struct.pack(">HH", 6, 1)
    if tcp:
        s = socket.create_connection((server, 53), 5); s.sendall(struct.pack(">H", len(msg)) + msg)
        ln = struct.unpack(">H", s.recv(2))[0]; data = b""
        while len(data) < ln: data += s.recv(ln - len(data))
    else:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.settimeout(5); s.sendto(msg, (server, 53)); data, _ = s.recvfrom(4096)
    rid, flags, qd, an, ns, ar = struct.unpack(">HHHHHH", data[:12])
    off = skip_name(data, 12) + 4
    soa = None
    for _ in range(an):
        off = skip_name(data, off)
        t, c, ttl, rl = struct.unpack(">HHIH", data[off:off+10]); off += 10
        if t == 6:
            mname = read_name(data, off); p = skip_name(data, off); rname = read_name(data, p); p = skip_name(data, p)
            serial, refresh, retry, expire, minimum = struct.unpack(">IIIII", data[p:p+20])
            soa = {"mname": mname, "rname": rname, "serial": serial, "refresh": refresh, "retry": retry, "expire": expire, "minimum": minimum, "ttl": ttl}
        off += rl
    return {"qname": ZONE, "qtype": "SOA", "transport": "tcp" if tcp else "udp", "server": server, "id_match": rid == tid, "aa": bool(flags & 0x0400), "tc": bool(flags & 0x0200), "rcode": flags & 0xF, "soa": soa}
print("wall=" + datetime.datetime.now(datetime.timezone.utc).isoformat())
for server in ["10.0.2.15", "192.0.2.10", "127.0.0.1"]:
    for tcp in (False, True):
        try: print(json.dumps(q(tcp, server)))
        except Exception as e: print(json.dumps({"qname": ZONE, "qtype": "SOA", "transport": "tcp" if tcp else "udp", "server": server, "error": str(e)}))

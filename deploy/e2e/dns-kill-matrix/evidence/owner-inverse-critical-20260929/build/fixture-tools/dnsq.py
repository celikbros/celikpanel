import socket, struct, random, json
def q(name, tcp, server):
    tid = random.randrange(65536)
    hdr = struct.pack(">HHHHHH", tid, 0x0000, 1, 0, 0, 0)
    qn = b"".join(bytes([len(p)])+p.encode() for p in name.split("."))+b"\0"
    msg = hdr+qn+struct.pack(">HH", 1, 1)
    if tcp:
        s=socket.create_connection((server,53),5); s.sendall(struct.pack(">H",len(msg))+msg)
        ln=struct.unpack(">H",s.recv(2))[0]; data=b""
        while len(data)<ln: data+=s.recv(ln-len(data))
    else:
        s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.settimeout(5); s.sendto(msg,(server,53)); data,_=s.recvfrom(4096)
    rid, flags, qd, an, ns, ar = struct.unpack(">HHHHHH", data[:12])
    # parse answers A only
    off=12
    while data[off]!=0: off+=1+data[off]
    off+=5
    answers=[]
    for _ in range(an):
        if data[off]&0xC0==0xC0: off+=2
        else:
            while data[off]!=0: off+=1+data[off]
            off+=1
        t,c,ttl,rl=struct.unpack(">HHIH",data[off:off+10]); off+=10
        if t==1: answers.append(socket.inet_ntoa(data[off:off+rl]))
        off+=rl
    return {"transport":"tcp" if tcp else "udp","server":server,"id_match":rid==tid,"aa":bool(flags&0x0400),"tc":bool(flags&0x0200),"rcode":flags&0xF,"answers":answers}
import subprocess
for server in ["10.0.2.15","192.0.2.10","127.0.0.1"]:
    for tcp in (False, True):
        try: print(json.dumps(q("www.s1-kill.test",tcp,server)))
        except Exception as e: print(json.dumps({"transport":"tcp" if tcp else "udp","server":server,"error":str(e)}))

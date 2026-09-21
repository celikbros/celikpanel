#!/usr/bin/env python3
"""Current-producer archive admission, with real canonical tar payloads."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

HERE=Path(__file__).resolve().parent

def load(name):
    spec=importlib.util.spec_from_file_location("policy_test_"+name,HERE/(name+".py"))
    value=importlib.util.module_from_spec(spec);sys.modules[spec.name]=value;spec.loader.exec_module(value)
    return value

archive=load("candidate_archive")
origin=load("worker_fixture_origin")
baseline=load("current_worker_baseline")

class BoundProducerPolicyTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)

    def bundle(self, version, policy, commit=None):
        files={name:("fixture "+name+"\n").encode() for name in archive.REQUIRED}
        files.update({"release.version":b"1\n","release.commit":((commit or "a"*40)+"\n").encode(),"release.tree":b"b"*40+b"\n"})
        if policy is not None:files["deploy/release-sequence-policy"]=policy
        files["SHA256SUMS"]="".join(hashlib.sha256(value).hexdigest()+"  ./"+name+"\n" for name,value in sorted(files.items())).encode()
        path=self.root/"candidate.tar.gz"
        with tarfile.open(path,"w:gz") as tar:
            root="celikpanel-"+version
            item=tarfile.TarInfo(root);item.type=tarfile.DIRTYPE;tar.addfile(item)
            for name,raw in files.items():
                item=tarfile.TarInfo(root+"/"+name);item.size=len(raw);item.mode=0o644;tar.addfile(item,io.BytesIO(raw))
        return path,hashlib.sha256(path.read_bytes()).hexdigest()

    def policy(self, current, previous, previous_commit="d"*40):
        return ("format=celikpanel-release-sequence-policy-v1\nversion=v0.1.0-alpha."+str(current)+"\ncurrent="+str(current)+"\nprevious="+str(previous)+"\nprevious_version=v0.1.0-alpha."+str(previous)+"\nprevious_commit="+previous_commit+"\n").encode()

    def test_exact_current_producer_policy_is_bound_to_tar_checksum_inventory(self):
        for current,previous in ((81,80),(82,81)):
            version="v0.1.0-alpha."+str(current);raw=self.policy(current,previous)
            path,sha=self.bundle(version,raw)
            expected={"version":version,"current":current,"previous":previous,"previous_version":"v0.1.0-alpha."+str(previous)}
            candidate=archive.inspect_archive(path,sha,release_policy=expected)
            self.assertEqual(candidate["release_policy"]["sha256"],candidate["files"]["deploy/release-sequence-policy"])
            self.assertEqual(candidate["release_policy"]["sha256"],hashlib.sha256(raw).hexdigest())

    def test_version_stamp_does_not_hide_old_or_skipped_native_policy(self):
        for raw in (None,self.policy(80,79),self.policy(82,80),self.policy(82,81).replace(b"current=82",b"current=80"),self.policy(82,81).replace(b"\n",b"\r\n")):
            with self.subTest(raw=raw):
                path,sha=self.bundle("v0.1.0-alpha.82",raw)
                # Unpublished historical experiments remain inspectable.
                self.assertNotIn("release_policy",archive.inspect_archive(path,sha))
                with self.assertRaises(ValueError):archive.inspect_archive(path,sha,release_policy=origin.RELEASE_POLICY)

    def test_exact_previous_commit_can_be_required(self):
        path,sha=self.bundle("v0.1.0-alpha.82",self.policy(82,81))
        archive.inspect_archive(path,sha,release_policy={**origin.RELEASE_POLICY,"previous_commit":"d"*40})
        with self.assertRaises(ValueError):archive.inspect_archive(path,sha,release_policy={**origin.RELEASE_POLICY,"previous_commit":"e"*40})

    def test_origin_rejects_mislabeled_real_archive_before_signing_or_copy(self):
        path,_=self.bundle("v0.1.0-alpha.82",self.policy(80,79))
        who={"nonce":"a"*64}
        pem=b"fixture material\n"
        names={"worker-origin-"+name+".pem":hashlib.sha256(pem).hexdigest() for name in ("signing","public","ca-key","ca","tls-key","tls")}
        material=json.dumps({"schema":origin.SCHEMA,"identity":who,"files":names}).encode()
        def read(path,*args):return material if path.name=="keys.json" else pem
        with patch.object(origin,"identity",return_value=(None,self.root,None,None,who)), patch.object(origin,"read_file",side_effect=read), patch.object(origin,"run") as run, patch.object(origin,"private_write") as write:
            with self.assertRaisesRegex(ValueError,"release policy"):
                origin.prepare(self.root,"debian13",path,"v0.1.0-alpha.82","a"*40,82,self.root)
            run.assert_not_called();write.assert_not_called()

    def test_baseline_rejects_real_mislabeled_archive_before_guest_access(self):
        path,sha=self.bundle(baseline.VERSION,self.policy(80,79),baseline.COMMIT)
        with patch.object(baseline.lab,"guarded_script") as run, patch.object(baseline.lab,"put_file") as put:
            with self.assertRaisesRegex(ValueError,"release policy"):
                baseline.start(self.root,{}, {},"debian13",True,archive_path=path,archive_sha256=sha,public_key_sha256="c"*64)
            run.assert_not_called();put.assert_not_called()

if __name__=="__main__":unittest.main()

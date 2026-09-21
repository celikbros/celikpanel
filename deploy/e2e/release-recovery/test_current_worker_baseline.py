#!/usr/bin/env python3
"""Host contract checks; these do not claim native installation acceptance."""
import ast
import hashlib
import importlib.util
import json
import io
import os
import tarfile
from pathlib import Path
import stat
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

SPEC=importlib.util.spec_from_file_location("test_current_baseline_module",Path(__file__).with_name("current_worker_baseline.py"))
baseline=importlib.util.module_from_spec(SPEC);sys.modules[SPEC.name]=baseline;SPEC.loader.exec_module(baseline)


class CurrentWorkerBaselineTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        self.record={"schema":baseline.lab.SCHEMA,"nonce":"a"*64,"cell_id":"release-recovery__1234567890abcdef"}
        self.node={"qemu_command":["qemu-system-x86_64","-uuid","12345678-1234-1234-1234-123456789abc"]}
        self.plan={"nodes":{"debian13":self.node}}
        self.candidate={"version":baseline.VERSION,"commit":baseline.COMMIT,"archive_sha256":"b"*64,
                        "files":{"deploy/enroll-signed-release-trust.sh":"c"*64}}

    def start(self,**changes):
        kwargs={"archive_path":self.root/"unused.tar.gz","archive_sha256":"b"*64,"public_key_sha256":"c"*64}
        kwargs.update(changes)
        return baseline.start(self.root,self.record,self.plan,"debian13",True,**kwargs)

    def mocks(self):
        return (patch.object(baseline.archive_tools,"inspect_archive",return_value=self.candidate),
                patch.object(baseline.archive_tools,"verify_committed_source",return_value={"commit":baseline.COMMIT}),
                patch.object(baseline.lab,"guarded_script",return_value=types.SimpleNamespace(stdout="")),
                patch.object(baseline.lab,"put_file",return_value=("unused","d"*64)))

    def test_start_is_once_only_even_when_transport_loses_reply(self):
        with self.mocks()[0],self.mocks()[1],self.mocks()[2] as run,self.mocks()[3]:
            self.start()
            launches=[call for call in run.call_args_list if "systemd-run" in call.args[4]]
            self.assertEqual(len(launches),1)
            with self.assertRaises(FileExistsError):self.start()
            self.assertEqual(len([call for call in run.call_args_list if "systemd-run" in call.args[4]]),1)
        sealed=baseline.intent_path(self.root,"debian13")
        self.assertEqual(stat.S_IMODE(sealed.stat().st_mode),0o600)
        value=json.loads(sealed.read_text())
        self.assertEqual(value["identity"],baseline.identity(self.record,"debian13",self.node))
        self.assertNotIn("password",sealed.read_text())

    def test_non_fixture_public_key_rejected_before_any_guest_access(self):
        with patch.object(baseline.lab,"guarded_script") as run:
            with self.assertRaises(ValueError):self.start(public_key_path="/etc/celikpanel/release-signing-ed25519.pem")
            run.assert_not_called()

    def test_wrong_feature_predecessor_refused(self):
        self.candidate["commit"]="e"*40
        with self.mocks()[0],self.mocks()[1],self.mocks()[2] as run,self.mocks()[3]:
            with self.assertRaises(ValueError):self.start()
            run.assert_not_called()
        self.assertFalse(baseline.intent_path(self.root,"debian13").exists())

    def test_read_status_is_bound_to_exact_installed_intent(self):
        with self.mocks()[0],self.mocks()[1],self.mocks()[2],self.mocks()[3]:self.start()
        intent=baseline.load_intent(self.root,self.record,self.plan,"debian13")
        result={"schema":baseline.RESULT_SCHEMA,"identity":intent["identity"],"archive_sha256":"b"*64,
                "intent_sha256":hashlib.sha256(baseline.encoded(intent)).hexdigest(),"verified":True}
        with patch.object(baseline.lab,"guarded_script",return_value=types.SimpleNamespace(stdout=json.dumps({"installation":result}))) as run:
            observed=baseline.status(self.root,self.record,self.plan,"debian13")
            self.assertTrue(observed["installation"]["verified"])
            self.assertNotIn("systemd-run",run.call_args.args[4])
        result["identity"]={**result["identity"],"nonce":"f"*64}
        with patch.object(baseline.lab,"guarded_script",return_value=types.SimpleNamespace(stdout=json.dumps({"installation":result}))):
            with self.assertRaises(ValueError):baseline.status(self.root,self.record,self.plan,"debian13")

    def test_real_tar_metadata_survives_private_umask(self):
        packed=self.root/"minimal-runtime.tar.gz"
        prefix="celikpanel-v0.1.0-alpha.81"
        files={"recovery-runtime/runtime.manifest":(b"manifest\n",0o644),
               "recovery-runtime/bin/recovery":(b"executable\n",0o755),
               "recovery-runtime/deploy/recovery/runtime-entry.sh":(b"#!/bin/sh\n",0o755)}
        dirs=("","recovery-runtime","recovery-runtime/bin","recovery-runtime/deploy","recovery-runtime/deploy/recovery")
        with tarfile.open(packed,"w:gz") as bundle:
            for path in dirs:
                member=tarfile.TarInfo(prefix+("/"+path if path else ""));member.type=tarfile.DIRTYPE;member.mode=0o755
                bundle.addfile(member)
            for path,(raw,mode) in files.items():
                member=tarfile.TarInfo(prefix+"/"+path);member.mode=mode;member.size=len(raw)
                bundle.addfile(member,io.BytesIO(raw))
        candidate={"root_name":prefix,"files":{name:hashlib.sha256(value[0]).hexdigest() for name,value in files.items()}}
        source=self.root/"extracted"
        previous=os.umask(0o077)
        try:baseline.extract_current_archive(source,packed,candidate,baseline.archive_tools)
        finally:os.umask(previous)
        with tarfile.open(packed,"r:gz") as bundle:
            for member in bundle:
                relative=baseline.archive_tools.member_path(member.name,prefix)
                path=source/relative
                self.assertEqual(stat.S_IMODE(path.stat().st_mode),member.mode,relative)
                if member.isfile():self.assertEqual(path.read_bytes(),bundle.extractfile(member).read())
        self.assertEqual(stat.S_IMODE(self.root.stat().st_mode),0o700)

    def test_guest_driver_compiles_and_uses_original_install_and_enrollment(self):
        code=baseline.guest_driver(self.record,"debian13",self.node,"b"*64)
        tree=ast.parse(code)
        compile(tree,"generated-current-baseline","exec")
        literals=[node.value for node in ast.walk(tree) if isinstance(node,ast.Constant)]
        self.assertIn("install.sh",literals)
        self.assertIn("deploy/enroll-signed-release-trust.sh",literals)
        self.assertIn(b"format=celikpanel-release-sequence-floor-v1\nsequence=81\nversion=v0.1.0-alpha.81\n",literals)
        self.assertNotIn("--self-update-worker",literals)
        self.assertNotIn("SKIP_ADMIN",literals)
        self.assertNotIn("DEMO",literals)


if __name__=="__main__":unittest.main()

import copy
import json
from pathlib import Path
import unittest
from verify_agent_compatibility import verify

class AgentCompatibilityEvidence(unittest.TestCase):
    def setUp(self):self.e=json.loads(Path(__file__).with_name('AGENT-COMPATIBILITY-BE.json').read_text())
    def test_native_record(self):self.assertEqual(verify(self.e)['native_admission'],'verified')
    def test_negative_evidence(self):
        edits=[
            lambda r:r['scope'].update(whole_application_rollback=True),
            lambda r:r['scope'].update(arch_mail_workload=True),
            lambda r:r['arch'].update(source_commit='b'*40),
            lambda r:r['debian13']['contract'].update(agent_sha256='b'*64),
            lambda r:r['debian13']['cases']['unmarked'].update(exit=0),
            lambda r:r['debian13']['cases']['edited-agent'].update(exit=0),
            lambda r:r['debian13']['cases']['linked-agent'].update(stderr='failed'),
            lambda r:r['debian13'].update(timer_enabled_active=False),
            lambda r:r['debian13']['workload_after'].clear(),
            lambda r:r['debian13']['served'].update(imap='b'*64),
            lambda r:r['arch']['cases']['unmarked'].update(candidate_exit=0),
            lambda r:r['arch']['cases']['unmarked'].update(compatibility_exit=3),
            lambda r:r['arch']['wants_after'].clear(),
            lambda r:r['arch'].update(publication_log='PASS'),
            lambda r:r['debian13'].update(management_absent=False),
        ]
        for edit in edits:
            with self.subTest(edit=edits.index(edit)):
                r=copy.deepcopy(self.e);edit(r)
                with self.assertRaises((ValueError,AssertionError,KeyError)):verify(r)

if __name__=='__main__':unittest.main()

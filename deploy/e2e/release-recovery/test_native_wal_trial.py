"""Native controller boundary tests; no process attach or update is executed."""
import importlib.util
from pathlib import Path
import sys
import unittest
from unittest import mock

HERE=Path(__file__).resolve().parent
s=importlib.util.spec_from_file_location('native_wal_controller_test',HERE/'native_wal_trial.py')
m=importlib.util.module_from_spec(s);sys.modules[s.name]=m;s.loader.exec_module(m)

class NativeControllerTests(unittest.TestCase):
    def test_mutations_require_execute_before_touching_lab(self):
        for mode in ('prepare', 'arm', 'start'):
            with self.subTest(mode=mode), mock.patch.object(m.lab, 'checked_root') as guard:
                with self.assertRaises(SystemExit) as stopped:
                    m.main(['--work-root', '/unused', '--node', 'arch', '--mode', mode])
                self.assertEqual(stopped.exception.code, 2)
                guard.assert_not_called()

    def test_every_executed_helper_requires_a_pinned_digest(self):
        pinned = {Path(name).name: 'a' * 64 for name in m.ASSETS}
        self.assertEqual(m.guest.validate_helpers(pinned), pinned)
        for missing in pinned:
            incomplete = dict(pinned)
            del incomplete[missing]
            with self.subTest(missing=missing), self.assertRaises(ValueError):
                m.guest.validate_helpers(incomplete)
        for invalid in ({}, None, {**pinned, 'extra.py': 'a' * 64},
                        {**pinned, 'native_trace.py': 'A' * 64},
                        {**pinned, 'guest_wal_checkpoint.py': None}):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                m.guest.validate_helpers(invalid)

    def test_events_cannot_claim_cut_without_prior_proof(self):
        for sequence in (['kill_sent'],['armed','kill_sent'],['armed','gate_released','kill_requested'],
                         ['armed','gate_released','wal_checkpoint_verified','kill_sent'],
                         ['armed','armed'],['trace_finished'],['trace_finished','armed']):
            with self.subTest(sequence=sequence),self.assertRaises(ValueError):m.validate_event_order(sequence)
    def test_partial_attempt_is_preserved_without_false_cut(self):
        sequence=['armed','gate_released','wal_checkpoint_verified','kill_requested','kill_sent']
        self.assertEqual(m.validate_event_order([]),[])
        for n in range(1,len(sequence)+1):
            for terminal in ([],['trace_finished']):
                self.assertEqual(m.validate_event_order(sequence[:n]+terminal),sequence[:n]+terminal)
    def test_cut_result_cannot_outpace_its_recorded_proof(self):
        sequence=['armed','gate_released','wal_checkpoint_verified','kill_requested','kill_sent']
        for n in range(len(sequence)):
            with self.subTest(n=n), self.assertRaises(ValueError):
                m.validate_event_order(sequence[:n], {'status': 'cut-sent'})
        self.assertEqual(m.validate_event_order(sequence, {'status': 'cut-sent'}), sequence)
        self.assertEqual(m.validate_event_order(sequence[:4], {'status': 'inconclusive'}), sequence[:4])
        for invalid in ('cut-sent', {}, {'status': 'success'}):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                m.validate_event_order(sequence, invalid)

    def test_target_unit_is_derived_from_exact_operation(self):
        operation='a'*32
        paths=m.guest.names(operation)
        self.assertEqual(paths['worker'],'celikpanel-self-update-'+operation+'.service')
        for invalid in ('../x','celikpanel-panel','a'*31,'A'*32,'a'*32+'\n'):
            with self.subTest(invalid=invalid),self.assertRaises(ValueError):m.guest.names(invalid)
    def test_gate_entrypoint_has_no_arbitrary_target(self):
        value={'operation_id':'a'*32,'identity':{'nonce':'b'*64,'vm_uuid':'uuid','cell_id':'cell','node':'arch'}}
        command=m.argv(value,'gate')
        self.assertEqual(command[:4],['/usr/bin/python3','-I','/root/celikpanel-release-recovery-lab/guest_native_wal_trial.py','--mode'])
        for unsupported in ('--hostname', '--pid', '--command'):
            self.assertNotIn(unsupported, command)

if __name__=='__main__':unittest.main()

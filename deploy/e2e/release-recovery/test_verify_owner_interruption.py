import copy
import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('tested_owner_cut',Path(__file__).with_name('verify_owner_interruption.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)

class OwnerInterruptionTests(unittest.TestCase):
    def setUp(self):
        self.op='a'*32;self.runtime='b'*64
        receipts={str(n):{'sha256':str(n)*64,'bytes':273} for n in (1,2,3)}
        who={'node':'debian13','vm_uuid':'sealed'};marker={'snapshot':'exact','transaction_token_sha256':'c'*64}
        before={'snapshot':'exact','operation_id':self.op,'receipts':receipts,'identity':who,'boot_id':'boot','marker':marker}
        owner=dict(receipts,**{'owner.Ab12':{'sha256':'d'*64,'bytes':275}})
        cli={'request_id':self.op,'observation':'known','phase':'recovery_required','terminal_proof':'none','automatic_recovery':'paused_retry_limit'}
        after=dict(before,receipts=owner,lock_free=True,cli=cli,at='2026-09-22T00:00:00Z')
        later=dict(after,at='2026-09-22T00:00:32Z')
        unit='celikpanel-lab-owner-interrupt-'+self.op+'.service'
        events=[{'event':name,'operation_id':self.op,'identity':who} for name in ('armed','freeze_requested','freeze_observed','checkpoint_verified','kill_requested','kill_sent','released')]
        events[0]['checkpoint']='owner_admission_published'
        events[3].update(worker={'unit':unit,'cgroup':'/system.slice/'+unit,'boot_id':'boot'},owner_receipts=owner,transaction=marker,snapshot_proof={'snapshot':'exact','runtime':{'runtime_manifest_sha256':self.runtime}})
        for index in (1,2,4,5):events[index]['worker']=copy.deepcopy(events[3]['worker'])
        events[-1].update(kill_sent=True,checkpoint_verified=True)
        self.value={'schema':'celikpanel/native-owner-interruption/v1','before':before,'after':after,'later':later,'events':events,
                    'unit_result':{'Result':'signal','ExecMainCode':'2','ExecMainStatus':'9','MainPID':'0'}}
        self.result={'request_id':self.op,'automatic_receipts_preserved':receipts,'selected_runtime_sha256':self.runtime}
    def check(self):return f.check(self.value,self.result)
    def test_complete_admission_cut(self):self.check()
    def test_wrong_or_active_owner_process(self):
        self.value['unit_result']['MainPID']='77'
        self.assertRaises(ValueError,self.check)
    def test_terminal_or_unknown_not_counted_as_pause(self):
        for field,value in [('observation','unavailable'),('terminal_proof','rollback_verified'),('request_id','e'*32)]:
            with self.subTest(field=field):
                item=copy.deepcopy(self.value);item['after']['cli'][field]=value
                self.assertRaises(ValueError,f.check,item,self.result)
    def test_other_runtime_or_vm_rejected(self):
        self.value['events'][3]['snapshot_proof']['runtime']['runtime_manifest_sha256']='e'*64
        self.assertRaises(ValueError,self.check)
    def test_killed_process_must_match_frozen_proof(self):
        self.value['events'][5]['worker']['boot_id']='other'
        self.assertRaises(ValueError,self.check)
    def test_missing_cut_rejected(self):
        self.value['events'][-1]['kill_sent']=False
        self.assertRaises(ValueError,self.check)
    def test_short_timer_observation_rejected(self):
        self.value['later']['at']='2026-09-22T00:00:02Z'
        self.assertRaises(ValueError,self.check)
    def test_automatic_reset_or_duplicate_owner_rejected(self):
        self.value['later']=copy.deepcopy(self.value['later'])
        self.value['later']['receipts']['owner.More']={'sha256':'f'*64}
        self.assertRaises(ValueError,self.check)

if __name__=='__main__':unittest.main()

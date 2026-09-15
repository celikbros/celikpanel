import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('exchange_boot_evidence',Path(__file__).with_name('exchange_recovery_evidence.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)


def fixtures():
    tx={'snapshot':'snapshot','transaction_token_sha256':'token'}
    identity={'node':'debian13'}
    exchange={'identity':identity,'operation_id':'op','transaction':tx,'kit':{'manifest_sha256':'runtime'},
              'worker':{'boot_id':'old','unit':'update.service'}}
    worker={'unit':m.UNIT,'boot_id':'old','invocation_id':'before'}
    held=dict(tx,schema='celikpanel/recovery-checkpoint/v1',checkpoint='payload_restored',boot_id='old',
              transaction_operation='rollback',recovery_unit=m.UNIT,invocation_id='before',runtime_manifest_sha256='runtime')
    last=dict(held,checkpoint='schedulers_restored',boot_id='new',invocation_id='after')
    recovery={'identity':identity,'operation_id':'op','intent':dict(tx,action='reboot',checkpoint='payload_restored',runtime_manifest_sha256='runtime'),
              'reboot_proof':{'worker':worker,'checkpoint':held,'checkpoint_sha256':'checkpoint'}}
    reset={'identity':identity,'operation_id':'op','command':'system_reset','action':'registered-QEMU-reset-submitted-once',
           'scope':'registered-disposable-QEMU-only','before_boot_id':'old','checkpoint_sha256':'checkpoint',
           'native_cut':dict(tx,checkpoint_sha256=hashlib.sha256(json.dumps(exchange,sort_keys=True,separators=(',',':')).encode()).hexdigest())}
    final={'identity':identity,'operation_id':'op','terminal_checkpoint':last,'boot_id':'new','transaction_markers':[],'expected_version':38}
    before=[{'_BOOT_ID':'old','UNIT':'update.service','MESSAGE':'status=9/KILL','__REALTIME_TIMESTAMP':'1'},
            {'_BOOT_ID':'old','UNIT':'update.service','MESSAGE':'Triggering OnFailure=','__REALTIME_TIMESTAMP':'2'},
            {'_BOOT_ID':'old','_SYSTEMD_UNIT':m.UNIT,'_SYSTEMD_INVOCATION_ID':'before','MESSAGE':'Verified snapshot / snapshot','__REALTIME_TIMESTAMP':'3'}]
    after=[{'_BOOT_ID':'new','_SYSTEMD_UNIT':m.UNIT,'_SYSTEMD_INVOCATION_ID':'after','MESSAGE':'Verified snapshot / snapshot','__REALTIME_TIMESTAMP':'4'},
           {'_BOOT_ID':'new','_SYSTEMD_UNIT':m.UNIT,'_SYSTEMD_INVOCATION_ID':'after','MESSAGE':'==> Rollback complete /','__REALTIME_TIMESTAMP':'5'}]
    return before,after,exchange,recovery,reset,final


class EvidenceTests(unittest.TestCase):
    def test_two_boots_same_transaction_native_completion(self):
        proof=m.verify(*fixtures());self.assertEqual(proof['status'],'verified')
        self.assertNotEqual(proof['before_boot_id'],proof['after_boot_id'])

    def test_boot_failure_is_preserved_when_later_native_retry_completes(self):
        data=fixtures();failed={'_BOOT_ID':'new','UNIT':m.UNIT,'MESSAGE':'Failed with result exit-code','__REALTIME_TIMESTAMP':'4'}
        data[1].insert(0,failed)
        self.assertEqual(m.verify(*data)['intermediate_failure_records'],[failed])

    def test_submission_or_same_boot_cannot_claim_reboot(self):
        data=fixtures();data[-1]['terminal_checkpoint']['boot_id']='old';data[-1]['boot_id']='old'
        with self.assertRaises(ValueError):m.verify(*data)

    def test_foreign_transaction_or_invocation_is_refused(self):
        for field,value in (('snapshot','foreign'),('invocation_id','before'),('runtime_manifest_sha256','other')):
            data=fixtures();data[-1]['terminal_checkpoint'][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):m.verify(*data)

    def test_missing_journal_or_wrong_boot_not_success(self):
        for index in range(3):
            data=list(fixtures());del data[0][index]
            with self.subTest(index=index),self.assertRaises(ValueError):m.verify(*data)
        data=fixtures();data[1][-1]['_BOOT_ID']='old'
        with self.assertRaises(ValueError):m.verify(*data)

    def test_markers_or_changed_exchange_refuse_completion(self):
        data=fixtures();data[-1]['transaction_markers']=['active']
        with self.assertRaises(ValueError):m.verify(*data)
        data=fixtures();data[2]['changed']=True
        with self.assertRaises(ValueError):m.verify(*data)


if __name__=='__main__':unittest.main()

import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_unselected_boot import verify

class MailBootEvidenceTest(unittest.TestCase):
    def setUp(self):self.r=json.loads(Path(__file__).with_name('MAIL-UNSELECTED-BOOT-BE.json').read_text())
    def test_record(self):self.assertEqual(verify(self.r)['automatic_boot_continuation'],'verified')
    def test_scope(self):
        for key in self.r['scope']:
            r=copy.deepcopy(self.r);r['scope'][key]=not r['scope'][key]
            with self.assertRaises(ValueError):verify(r)
    def test_preservation(self):
        for key in ['management_absent','prior_jobs_unchanged','native_config_unchanged','old_generation_preserved','before_image_unchanged','unselected_stage_preserved','timer_enabled_active']:
            r=copy.deepcopy(self.r);r['result'][key]=False
            with self.assertRaises(ValueError):verify(r)
    def test_operation_source_and_budget(self):
        for key,value in [('commit','a'*40),('request','a'*32),('owner','b'*32),('generation','a'*64),('attempt',1),('attempt',True)]:
            r=copy.deepcopy(self.r);r['result'][key]=value
            with self.assertRaises(ValueError):verify(r)
    def test_boot_listener_and_dispatch(self):
        for kind in ['same_boot','other_journal','no_finish','wrong_listener','wrong_executable','wrong_binary','no_stage']:
            r=copy.deepcopy(self.r)
            if kind=='same_boot':r['result']['boot_after']=r['result']['boot_before']
            if kind=='other_journal':r['result']['native_journal'][0]['_BOOT_ID']='a'*32
            if kind=='no_finish':r['result']['native_journal']=[x for x in r['result']['native_journal'] if not x['MESSAGE'].startswith('Finished ')]
            if kind=='wrong_listener':r['result']['served']['imap993']='a'*64
            if kind=='wrong_executable':r['result']['native_unit']=r['result']['native_unit'].replace(r['kit_manifest']['generation'],'a'*64)
            if kind=='wrong_binary':r['kit_manifest']['binary_sha256']='a'*64
            if kind=='no_stage':r['before']['retained_stages']=[]
            with self.assertRaises(ValueError):verify(r)
    def test_hashed_logs_still_need_actual_events(self):
        for key in ['kill','enrollment']:
            r=copy.deepcopy(self.r);text='No accepted event';r['logs'][key]={'text':text,'sha256':hashlib.sha256(text.encode()).hexdigest()}
            with self.assertRaises(ValueError):verify(r)
    def test_preparation_failure_retention(self):
        self.r['retained_preparations']=[]
        with self.assertRaises(ValueError):verify(self.r)

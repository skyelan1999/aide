"""Completed job names cannot substitute for complete runtime receipts."""
import copy
import itertools
import unittest
import release_evidence as gate


class EvidenceTests(unittest.TestCase):
    def test_every_browser_cell_required_and_bound_to_source(self):
        r={'status':'pass','commit':'a'*40,'cases':[{'id':'-'.join(map(str,v)), 'status':'pass','checks':['assertion']*5} for v in itertools.product(('chromium','firefox','webkit'),('zh-CN','en'),('blue','comfort'),('light','dark'),(1440,390))]}
        gate.browser(r,'a'*40)
        for mutate in ('missing','skip','source','duplicate','empty'):
            x=copy.deepcopy(r)
            if mutate=='missing':x['cases'].pop()
            if mutate=='skip':x['cases'][0]['status']='skip'
            if mutate=='source':x['commit']='b'*40
            if mutate=='duplicate':x['cases'][0]=x['cases'][1]
            if mutate=='empty':x['cases'][0]['checks']=[]
            with self.subTest(mutate=mutate),self.assertRaises(ValueError):gate.browser(x,'a'*40)

    def test_all_go_names_need_one_completion_and_skips_are_separate(self):
        r={'status':'pass','server':{'status':'pass','inventory':['TestA','FuzzB'],'inventoryCount':2,'passed':['TestA'],'skipped':['FuzzB'],'shards':[{'exit':0,'selected':['TestA','FuzzB'],'results':{'TestA':'pass','FuzzB':'skip'},'missing':[]}]}}
        gate.go_suite(r)
        for mutate in ('missing','duplicate','failure','unaccounted','mislabel'):
            x=copy.deepcopy(r);s=x['server']
            if mutate=='missing':del s['shards'][0]['results']['TestA']
            if mutate=='duplicate':s['shards'].append(s['shards'][0])
            if mutate=='failure':s['shards'][0]['exit']=1
            if mutate=='unaccounted':s['skipped']=[]
            if mutate=='mislabel':s['passed'].append('FuzzB')
            with self.subTest(mutate=mutate),self.assertRaises(ValueError):gate.go_suite(x)

    def test_host_must_be_native_and_all_commands_pass(self):
        r={'status':'pass','commit':'a'*40,'host':'macos','machine':'arm64','checks':[{'status':'pass'}]*8}
        r['checks']=[dict(check) for check in r['checks']]
        r['checks'][0]['goTests']=[{'Package':p,'Test':'TestFixture','Action':'pass'} for p in ('aide/cmd/aide-local-mounts','aide/internal/hostbridge','aide/internal/localmounts','aide/internal/messaging')]
        gate.host(r,'a'*40,'macos-15')
        with self.assertRaises(ValueError):gate.host(r,'a'*40,'macos-15-intel')
        r['checks'].pop()
        with self.assertRaises(ValueError):gate.host(r,'a'*40,'macos-15')

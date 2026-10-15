#!/usr/bin/env python3
"""Opt-in disposable Go App/browser fixture; publishes loopback port18787 only."""
import importlib.util
from pathlib import Path
import os

root = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('snapshot', root / 'scripts/test_go_snapshot.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
original = module.subprocess.run
def run(args, **kw):
    if args[:2] == ['docker', 'run']:
        args = list(args)
        args[args.index('--network') + 1] = 'bridge'
        args[2:2] = ['--publish', '127.0.0.1:18787:18787']
    return original(args, **kw)
module.subprocess.run = run
module.print = lambda *args, **kw: print(*(value.replace('network none', 'browser bridge; loopback18787 only') if isinstance(value, str) else value for value in args), **kw)
module.COMMAND = module.COMMAND.replace('go test -mod=vendor -race -count=1 -timeout=18m ./...', 'AIDE_TOPIC_BROWSER_FIXTURE=1 go test -v -mod=vendor -race -count=1 -timeout=9m -run ^TestKnowledgeConclusionTopicsLiveBrowser$ ./internal/server')
if os.environ.get('AIDE_TOPIC_BODY_FIXTURE') == '1':
    module.COMMAND = module.COMMAND.replace('AIDE_TOPIC_BROWSER_FIXTURE=1', 'AIDE_TOPIC_BODY_FIXTURE=1 AIDE_TOPIC_BROWSER_FIXTURE=1')
module.main()

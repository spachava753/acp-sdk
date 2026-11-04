#!/usr/bin/env python3
"""Independent line-oriented cross-check of the richer balanced Rust inventory.

No Rust checkout/runtime is required by normal Go tests. This explicit provenance
check requires the pinned checkout and uses only Python's standard library.
"""
import argparse
import collections
import hashlib
import json
import re
import subprocess
import tomllib
from pathlib import Path

p=argparse.ArgumentParser()
p.add_argument('--rust-root',type=Path,required=True)
p.add_argument('--inventory',type=Path,required=True)
a=p.parse_args(); root=a.rust_root.resolve(); d=json.loads(a.inventory.read_text())
revision=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip()
assert revision==d['upstream_revision'],('revision mismatch',revision)
members=tomllib.loads((root/'Cargo.toml').read_text())['workspace']['members']
paths=sorted(p for member in members for p in (root/member).rglob('*.rs') if 'target' not in p.parts)
expected_files={f['source_path']:f['sha256'] for f in d['files']}
assert {p.relative_to(root).as_posix() for p in paths}==set(expected_files),'file census differs'
found=[]; docs=[]; macros=[]; supplemental=[]
for path in paths:
    source=path.relative_to(root).as_posix(); content=path.read_text(); lines=content.splitlines()
    assert hashlib.sha256(content.encode()).hexdigest()==expected_files[source],('file hash changed',source)
    # This pin has no block docs; fail rather than silently mishandle any future ones.
    assert '/**' not in content and '/*!' not in content,('new block-doc handling required',source)
    for i,s in enumerate(lines):
        if re.match(r'^\s*#\[(?:[A-Za-z_]\w*::)*test(?:\]|\()',s):
            function=None
            for j in range(i+1,min(i+30,len(lines))):
                m=re.match(r'^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+(\w+)',lines[j])
                if m:
                    function=m[1]; found.append((source,j+1,function));break
            assert function,('unlinked test attribute',source,i+1)
        m=re.match(r'^\s*macro_rules!\s*(\w+)',s)
        if m: macros.append((source,i+1,m[1]))
        probe=re.match(r'^\s*fn\s+((?:v2_)?on_[A-Za-z_]*accepts_non_static_callback_future)\b',s)
        if source.endswith('/tests/session_ordering.rs') and probe: supplemental.append((source,i+1,probe[1]))
        probe=re.match(r'^\s*fn\s+(spawn_child_from_generic_context)\b',s)
        if source.endswith('/tests/protocol_v2.rs') and probe: supplemental.append((source,i+1,probe[1]))
    opened=None
    for i,s in enumerate(lines):
        m=re.match(r'^\s*(?:///|//!)\s*(`{3,}|~{3,})(.*)',s)
        if not m: continue
        if opened is None: opened=(i+1,m[1],m[2].strip())
        elif m[1][0]==opened[1][0] and len(m[1])>=len(opened[1]):
            docs.append((source,opened[0],opened[2]));opened=None
    assert opened is None,('unclosed doc fence',source,opened)
actual={(e['source'],e['line'],e['function']) for e in d['entries']}
assert len(actual)==len(d['entries']),'duplicate canonical function record'
assert len(found)==len(set(found)),'duplicate independently indexed registration'
assert set(found)==actual,{'missing':sorted(set(found)-actual),'unexpected':sorted(actual-set(found))}
assert set(docs)=={(x['source'],x['line'],x['info']) for x in d['doctests']},'doctest fence census differs'
assert set(macros)=={(x['source'],x['source_line'],x['name']) for x in d['macros']},'macro definition census differs'
assert set(supplemental)=={(e['source'],e['line'],e['function']) for e in d.get('supplemental',[])},'supplemental compile-contract census differs'
assert not (set(supplemental)&actual),'supplemental contract double-counted as registered test'
for e in d['entries']+d['doctests']+d.get('supplemental',[]):
    assert e['id']==f"{e['source']}:{e['line']}:{e['function']}",('bad canonical id',e['id'])
    assert e['status'] in ['ported','partially-ported','not-applicable','needs-parent-review'],('bad status',e['id'])
    assert e['reason'].strip(),('empty reason',e['id'])
print(json.dumps({'result':'PASS','upstream_revision':revision,'source_files':len(paths),
                  'test_function_declarations':len(found),'all_doc_fences':len(docs),
                  'macro_definitions':len(macros),'supplemental_compiler_contracts':len(supplemental),
                  'function_status_counts':dict(collections.Counter(x['status'] for x in d['entries'])),
                  'doc_status_counts':dict(collections.Counter(x['status'] for x in d['doctests'])),
                  'semantic_reconciliation_complete':False},indent=2))

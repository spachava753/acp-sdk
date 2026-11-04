#!/usr/bin/env python3
"""Source census for the pinned Rust workspace; no Cargo/parser installation required.

This is a lexical source inventory, NOT a claim of compiler-expanded cargo test --list.
It masks Rust comments/strings, balances delimiters, records every test attribute, doc
fence, macro and loop, and retains source hashes so omissions are detectable. Semantic
portability/expanded case coverage is reconciled separately with domain mappings.
"""
from __future__ import annotations

import argparse
import collections
import hashlib
import json
import re
import subprocess
import tomllib
from pathlib import Path

PIN = "050cd94c081056ace37a7b89abfed7f6220b47eb"


def mask_rust(s):
    out = list(s)
    docs = []
    i = 0
    def blank(a, b):
        for n in range(a, b):
            if s[n] != "\n": out[n] = " "
    while i < len(s):
        if s.startswith("//", i):
            j = s.find("\n", i)
            if j < 0: j = len(s)
            if s.startswith("//!", i) or (s.startswith("///", i) and not s.startswith("////", i)):
                docs.append((i, j, s[i+3:j]))
            blank(i, j); i = j; continue
        if s.startswith("/*", i):
            j = i + 2; depth = 1
            while j < len(s) and depth:
                if s.startswith("/*", j): depth += 1; j += 2
                elif s.startswith("*/", j): depth -= 1; j += 2
                else: j += 1
            if s.startswith("/*!", i) or (s.startswith("/**", i) and not s.startswith("/***", i)):
                docs.append((i, j, s[i+3:j-2]))
            blank(i, j); i = j; continue
        raw = re.match(r'(?:b|c)?r(#{0,255})"', s[i:]) if (i == 0 or not (s[i-1].isalnum() or s[i-1] == '_')) else None
        if raw:
            end = '"' + raw[1]
            j = s.find(end, i + raw.end())
            if j < 0: raise ValueError(f"unclosed raw string at {i}")
            j += len(end); blank(i, j); i = j; continue
        if s[i] == '"':
            j = i + 1
            while j < len(s):
                if s[j] == '\\': j += 2
                elif s[j] == '"': j += 1; break
                else: j += 1
            blank(i, j); i = j; continue
        if s[i] == "'":
            char = re.match(r"'(?:\\(?:u\{[0-9a-fA-F_]+\}|x[0-9a-fA-F]{2}|.)|[^'\\\n])'", s[i:])
            if char:
                j = i + char.end(); blank(i, j); i = j; continue
        i += 1
    return ''.join(out), docs


def balanced(mask):
    pairs, stack = {}, []
    opens = {'(': ')', '[': ']', '{': '}'}
    for i, c in enumerate(mask):
        if c in opens: stack.append((c, i))
        elif c in ')]}':
            if not stack or opens[stack[-1][0]] != c:
                raise ValueError(f"unbalanced delimiter {c} at {i}")
            _, start = stack.pop(); pairs[start] = i; pairs[i] = start
    if stack: raise ValueError(f"unclosed delimiters: {stack[-3:]}")
    return pairs


def line(s, pos): return s.count('\n', 0, pos) + 1


def attributes(s, mask, pairs):
    found = []
    for m in re.finditer(r'#(!?)\s*\[', mask):
        b = mask.find('[', m.start()); e = pairs[b] + 1
        found.append({'start': m.start(), 'end': e, 'inner': bool(m[1]),
                      'line': line(s, m.start()), 'text': s[m.start():e]})
    return found


def leading_attrs(mask, attrs, pos):
    selected = []
    cursor = pos
    while True:
        candidates = [a for a in attrs if not a['inner'] and a['end'] <= cursor and not mask[a['end']:cursor].strip()]
        if not candidates: return list(reversed(selected))
        a = max(candidates, key=lambda a: a['start']); selected.append(a); cursor = a['start']


def top_split(s, mask, pairs, start, end):
    chunks, a, i = [], start, start
    while i < end:
        if mask[i] in '([{': i = pairs[i] + 1; continue
        if mask[i] == ',':
            if s[a:i].strip(): chunks.append(s[a:i].strip())
            a = i + 1
        i += 1
    if s[a:end].strip(): chunks.append(s[a:end].strip())
    return chunks


def scan_file(path, root):
    s = path.read_text(); mask, docs = mask_rust(s); pairs = balanced(mask); attrs = attributes(s, mask, pairs)
    modules = []
    for m in re.finditer(r'(?m)^[ \t]*(?:pub(?:\([^\n)]*\))?\s+)?mod\s+([A-Za-z_]\w*)\s*([;{])', mask):
        p = m.end() - 1
        modules.append({'name': m[1], 'start': m.start(), 'body': p if m[2] == '{' else None,
                        'end': pairs[p]+1 if m[2] == '{' else m.end(),
                        'attrs': leading_attrs(mask, attrs, m.start())})
    # Include qualifiers in the header start so leading attributes remain adjacent.
    funcs = []
    pat = r'(?m)^[ \t]*(?:(?:pub(?:\([^\n)]*\))?|async|unsafe|const|default)\s+)*fn\s+([A-Za-z_]\w*)\s*'
    for m in re.finditer(pat, mask):
        b = mask.find('{', m.end()); sem = mask.find(';', m.end())
        if b < 0 or (sem >= 0 and sem < b): continue
        own = leading_attrs(mask, attrs, m.start())
        containers = [x for x in modules if x['body'] is not None and x['body'] < m.start() < x['end']]
        containers.sort(key=lambda x: x['start'])
        funcs.append({'name': m[1], 'qualified_name': '::'.join([x['name'] for x in containers] + [m[1]]),
                      'start': m.start(), 'body': b, 'end': pairs[b]+1, 'attrs': own, 'modules': containers})
    root_inner = [a for a in attrs if a['inner'] and not any(x['body'] is not None and x['body'] < a['start'] < x['end'] for x in modules)]
    constants = {}
    for m in re.finditer(r'\b(?:const|static)\s+([A-Za-z_]\w*)[^;=]*=\s*&?\s*\[', mask):
        b = mask.rfind('[', m.start(), m.end()); e = pairs[b]
        constants[m[1]] = {'source_line': line(s, m.start()), 'values': top_split(s, mask, pairs, b+1, e)}
    def cfg_for(pos, own):
        container_attrs = []
        for mod in modules:
            if mod['body'] is not None and mod['body'] < pos < mod['end']:
                container_attrs.extend(mod['attrs'])
                container_attrs.extend(a for a in attrs if a['inner'] and mod['body'] < a['start'] < mod['end'] and not any(z['body'] is not None and mod['body'] < z['body'] < a['start'] < z['end'] < mod['end'] for z in modules))
        return [a['text'] for a in root_inner+container_attrs+own if re.match(r'#!?\[cfg(?:_|\()', a['text'])]
    loops = []
    for m in re.finditer(r'\bfor\s+([^;{}]+?)\s+in\s+([^;{}]+?)\{', mask, re.S):
        header = s[m.start():m.end()-1].strip()
        iterable_start = m.start(2); iterable_end = m.end(2)
        iterable = s[iterable_start:iterable_end].strip()
        rec = {'source_line': line(s, m.start()), 'header': header, 'iterable': iterable, 'values': None,
               'value_count': None, 'resolution': 'semantic-review-required', '_offset': m.start()}
        # Array literal or reference to an array literal (including iteration adapters).
        a = mask.find('[', iterable_start, iterable_end)
        const = re.match(r'([A-Za-z_]\w*)', iterable)
        if const and const[1] in constants:
            rec['values'] = list(constants[const[1]]['values']); rec['constant'] = const[1]
            rec['constant_source_line'] = constants[const[1]]['source_line']; rec['resolution'] = 'constant-array'
            if a >= 0 and '.chain' in iterable:
                rec['values'] += top_split(s, mask, pairs, a+1, pairs[a]); rec['resolution'] = 'constant-array-plus-chain-array'
        elif a >= 0 and iterable[:iterable.find('[')].strip() in ('', '&'):
            rec['values'] = top_split(s, mask, pairs, a+1, pairs[a]); rec['resolution'] = 'literal-array'
        elif re.fullmatch(r'\d+\s*\.\.= ?\s*\d+', iterable):
            lo, hi = map(int, re.findall(r'\d+', iterable)); rec['values'] = list(range(lo,hi+1)); rec['resolution'] = 'literal-range'
        elif re.fullmatch(r'\d+\s*\.\.\s*\d+', iterable):
            lo, hi = map(int, re.findall(r'\d+', iterable)); rec['values'] = list(range(lo,hi)); rec['resolution'] = 'literal-range'
        if rec['values'] is not None: rec['value_count'] = len(rec['values'])
        loops.append(rec)
    macro_defs = []
    for m in re.finditer(r'\bmacro_rules!\s*([A-Za-z_]\w*)\s*\{', mask):
        b = m.end()-1
        macro_defs.append({'name': m[1], 'source_line': line(s,m.start()), 'end_line': line(s,pairs[b]),
                           'contains_test_attributes': bool(re.search(r'#\[(?:[\w]+::)*test\b', mask[b:pairs[b]])),
                           '_start': m.start(), '_end': pairs[b]+1})
    tests = []
    for f in funcs:
        testattrs = [a for a in f['attrs'] if re.match(r'#\[(?:[\w]+::)*test(?:\s*[\](])', a['text'])]
        if not testattrs: continue
        body = s[f['body']:f['end']]; bmask = mask[f['body']:f['end']]
        calls = []
        for helper in funcs:
            if helper is f or any(re.match(r'#\[(?:[\w]+::)*test\b', a['text']) for a in helper['attrs']): continue
            if re.search(r'(?<![.\w:])'+re.escape(helper['name'])+r'\s*(?:::\s*<[^;{}]+>)?\s*\(', bmask):
                calls.append({'rust_function': helper['qualified_name'], 'source_line': line(s,helper['start']),
                              'end_line': line(s,helper['end']-1),
                              'loop_source_lines': [l['source_line'] for l in loops if helper['body'] < l['_offset'] < helper['end']]})
        tests.append({'rust_test': f['qualified_name'], 'source_line': line(s,f['start']), 'end_line': line(s,f['end']-1),
                      'test_attributes': [a['text'] for a in testattrs], 'attributes': [a['text'] for a in f['attrs']],
                      'cfg': cfg_for(f['start'], f['attrs']),
                      'source_sha256': hashlib.sha256(s[f['start']:f['end']].encode()).hexdigest(),
                      'loop_subcases': [{k:v for k,v in l.items() if k != '_offset'} for l in loops if f['body'] < l['_offset'] < f['end']],
                      'helper_calls': calls,
                      'macro_calls': [{'name': m[1], 'source_line': line(s,f['body']+m.start()),
                                       'invocation':s[f['body']+m.start():pairs[f['body']+m.end()-1]+1]}
                                      for m in re.finditer(r'([A-Za-z_]\w*(?:::\w+)*)!\s*[({\[]', bmask)
                                      if m[1].split('::')[-1] not in ['assert','assert_eq','assert_ne','format','panic','vec','matches','expect','expect_file','include_str','json','println','eprintln','bail','anyhow']],
                      '_offset': f['start']})
    # Preserve every doc fence, including ignored/non-Rust examples. No silently discarded docs.
    fences = []
    opened = None
    for start,end,content in docs:
        # Block-doc offsets are approximate per content line, but precise enclosing comment span is retained.
        for n, raw_line in enumerate(content.splitlines() or ['']):
            t = re.sub(r'^\s*\* ?', '', raw_line).strip()
            fm = re.match(r'(`{3,}|~{3,})(.*)', t)
            if fm:
                if opened is None:
                    info=fm[2].strip()
                    opened={'source_line':line(s,start)+n,'fence':fm[1],'info':info,'lines':[],'comment_source_line':line(s,start)}
                elif fm[1][0] == opened['fence'][0] and len(fm[1]) >= len(opened['fence']):
                    code='\n'.join(opened.pop('lines')); opened['code']=code; opened['end_line']=line(s,start)+n
                    info=opened['info']; opened['rustdoc_candidate']=not info or bool(re.match(r'(rust|no_run|ignore|compile_fail|should_panic|edition\d|test_harness)',info))
                    opened['cfg']=cfg_for(start,[]); opened['contains_assertions']=bool(re.search(r'\bassert(?:_eq|_ne)?!|compile_fail',code+' '+info))
                    fences.append(opened); opened=None
            elif opened is not None: opened['lines'].append(re.sub(r'^ ?', '',raw_line))
    # #[doc = ...] content is retained for explicit review rather than silently ignored.
    docattrs=[{'source_line':a['line'],'text':a['text']} for a in attrs if re.match(r'#!?\[doc\s*=',a['text'])]
    seen_test_attrs={a['start'] for f in funcs for a in f['attrs'] if re.match(r'#\[(?:[\w]+::)*test(?:\s*[\](])', a['text'])}
    orphan=[{'source_line':a['line'],'text':a['text']} for a in attrs if re.match(r'#\[(?:[\w]+::)*test(?:\s*[\](])', a['text']) and a['start'] not in seen_test_attrs]
    source_path=path.relative_to(root).as_posix()
    supplements=[]
    for f in funcs:
        is_lifetime_probe=source_path.endswith('/tests/session_ordering.rs') and f['qualified_name'].startswith('callback_future_lifetimes::') and ('accepts_non_static_callback_future' in f['name'])
        is_generic_probe=source_path.endswith('/tests/protocol_v2.rs') and f['name']=='spawn_child_from_generic_context'
        if is_lifetime_probe or is_generic_probe:
            supplements.append({'id':source_path+':'+str(line(s,f['start']))+':'+f['name'],
                                'source':source_path,'function':f['name'],'line':line(s,f['start']),
                                'end_line':line(s,f['end']-1),'module':f['qualified_name'].rpartition('::')[0],
                                'cfg':cfg_for(f['start'],f['attrs']),'status':'not-applicable','classification':'rust-compile-contract',
                                'domain_hint':'session','reason':
                                ('Unannotated intentional compile-time regression in callback_future_lifetimes: a static callback returns a future carrying an arbitrary shorter lifetime; the named '+f['name']+' session/proxy callback API must accept it. Go has no Rust lifetime/Future bound checker or matching high-level callback builder API; no runtime emulation is claimed.' if is_lifetime_probe else
                                 'Unannotated allow(dead_code) generic compile probe: spawn_connection on a raw parent returns P::Connection<R::Counterpart> for an arbitrary Protocol builder. Go has no protocol-associated connection type or child-spawn builder API; not a runtime test and not faked with unmanaged goroutines.')})
    helper_value_refs=[]
    for f in funcs:
        if source_path.endswith('/tests/auto_traits.rs') and f['name']=='returned_futures_are_send':
            for helper in funcs:
                if helper['name'] in ['futures_v1','futures_agent','futures_v2']:
                    helper_value_refs.append({'parent_test_id':source_path+':'+str(line(s,f['start']))+':'+f['name'],
                                              'source_line':line(s,helper['start']),'end_line':line(s,helper['end']-1),
                                              'function':helper['name'],'reference_kind':'compile-time function value; never executed',
                                              'cfg':cfg_for(helper['start'],helper['attrs'])})
    return {'source_path':source_path, 'sha256':hashlib.sha256(s.encode()).hexdigest(),
            'line_count':s.count('\n')+bool(s and not s.endswith('\n')),
            'tests':tests,'doc_fences':fences,'doc_attributes':docattrs,
            'supplemental_compiler_contracts':supplements,'helper_function_value_references':helper_value_refs,
            'macros':[{k:v for k,v in m.items() if not k.startswith('_')} for m in macro_defs],
            'all_loops':[{k:v for k,v in l.items() if k != '_offset'} for l in loops],
            'orphan_test_attributes':orphan,
            'external_modules':[{'module':m['name'],'source_line':line(s,m['start']),
                                 'attributes':[a['text'] for a in m['attrs']],
                                 'inline_modules':[x['name'] for x in modules if x['body'] is not None and x['body'] < m['start'] < x['end']],
                                 'cfg':cfg_for(m['start'],m['attrs'])} for m in modules if m['body'] is None],
            'file_cfg':[a['text'] for a in root_inner if re.match(r'#!?\[cfg',a['text'])]}


def initial_domain(p, name):
    crate=p.split('/')[1]
    if crate != 'agent-client-protocol':
        if crate == 'agent-client-protocol-test': return 'session'
        if crate == 'agent-client-protocol-derive': return 'wire'
        if crate == 'agent-client-protocol-cookbook': return 'session'
        return 'parent-review-noncore'
    f=Path(p).name
    if f=='protocol_v2.rs':
        if name in ['mcp_over_acp_v1_variants_are_jsonrpc_mapped','mcp_carriers_preserve_inner_outcomes_in_both_protocol_versions']: return 'schema'
        if name in ['v1_responder_keeps_null_request_ids','v1_and_raw_agents_answer_entity_cancel_with_method_not_found']: return 'wire'
        return 'session'
    if f in ['protocol_compat.rs','application_dispatch_v2.rs','proxy_protocol_router_v2.rs']: return 'session'
    if p.endswith('/src/jsonrpc.rs') and name in [
        'v2_builder_exposes_typed_context_to_user_callbacks','proxy_builders_negotiate_proxy_initialize_like_agents',
        'v2_proxy_rejects_explicitly_prewrapped_initialize_request','v2_proxy_builder_exposes_typed_context_to_user_callbacks',
        'raw_connection_spawns_v2_builder_with_typed_child_callback','raw_connection_returns_raw_context_for_raw_builder']:
        return 'session'
    if f=='transport_actor.rs' and name=='byte_writer_flushes_then_continues_pending_shutdown_without_reflushing': return 'lifecycle'
    if f in ['derive_macros.rs','auto_traits.rs']: return 'wire'
    if f=='mcp_message_deserialization.rs' or '/schema/' in p or f.startswith(('schema_', 'proxy_schema')): return 'schema'
    if '/mcp_server/' in p or f.startswith(('session_', 'v1_connection', 'v1_feature', 'protocol_negotiation','mcp_','native_mcp','prepared_', 'meta_propagation')) or f in ['component.rs','acp_agent.rs'] or '/role/acp.rs' in p or f=='jsonrpc_advanced.rs': return 'session'
    if f in ['raw_error.rs','raw_id.rs','transport_actor.rs'] or f.startswith(('error_wire','request_id','jsonrpc_error','jsonrpc_edge','jsonrpc_hello','jsonrpc_request_ids','jsonrpc_unhandled','jsonrpc_dropped','jsonrpc_batch','jsonrpc_connection','jsonrpc_deep','match_dispatch')) or f=='error.rs': return 'wire'
    if f=='log_privacy.rs': return 'parent-review-log-privacy'
    return 'lifecycle'


def doc_owner(source, at):
    if source.startswith('src/agent-client-protocol-derive/'): return 'wire'
    if source=='src/agent-client-protocol/src/jsonrpc.rs':
        if at in [396,3944]: return 'inventory'
        if at in [1194,1632,1678,3697,6490,7230,7332]: return 'lifecycle'
        if at in [3754,3776,3837,3953,4016,5717,5743,5761,6635,6788,6817,6916,6982]: return 'session'
        return 'wire'
    if '/concepts/cancellation.rs' in source or '/concepts/connections.rs' in source: return 'lifecycle'
    if '/concepts/error_handling.rs' in source or source.endswith('/util/typed.rs') or source.endswith('/jsonrpc/protocol.rs'): return 'wire'
    if '/concepts/' in source or source.endswith(('/lib.rs','/v1.rs','/v2.rs','/session.rs','/session/v2.rs','/jsonrpc/v1_connection.rs','/jsonrpc/v2_connection.rs')) or '/mcp_server/' in source: return 'session'
    return initial_domain(source,'doctest')


def module_contexts(files, crates):
    by_path={f['source_path']:f for f in files}
    edges=[]
    for f in files:
        p=Path(f['source_path']); crate_root=Path(*p.parts[:2])
        is_root=p.name in ('lib.rs','main.rs') or ('tests' in p.parts and p.parent==crate_root/'tests') or ('examples' in p.parts and p.parent==crate_root/'examples')
        base=p.parent if is_root or p.name=='mod.rs' else p.parent/p.stem
        for m in f['external_modules']:
            override=next((re.search(r'path\s*=\s*"([^"]+)"',a) for a in m['attributes'] if re.search(r'path\s*=\s*"([^"]+)"',a)),None)
            if override:
                candidate=str(p.parent/override[1])
                parts=[]
                for part in Path(candidate).parts:
                    if part=='..': parts.pop()
                    elif part!='.': parts.append(part)
                candidates=[str(Path(*parts))]
            else:
                b=base.joinpath(*m['inline_modules'])
                candidates=[str(b/(m['module']+'.rs')),str(b/m['module']/'mod.rs')]
            targets=[x for x in candidates if x in by_path]
            m['resolved_source_paths']=targets
            for target in targets: edges.append({'from':f['source_path'],'to':target,'module':m['module'],'line':m['source_line'],'cfg':m['cfg']})
    incoming=collections.defaultdict(list)
    for e in edges: incoming[e['to']].append(e)
    def walk(path,visited=()):
        if path in visited: return []
        parents=incoming[path]
        if not parents: return [[]]
        return [chain+[e] for e in parents for chain in walk(e['from'],visited+(path,))]
    for f in files:
        f['inherited_module_contexts']=walk(f['source_path'])
    return edges


def inventory_exclusion(entry):
    crate=entry['crate']; name=entry['function']; path=entry['source']
    descriptions={
      'agent-client-protocol-http': ('absent-go-http-api', 'HTTP/SSE/WebSocket transport behavior. Go has no HTTP/SSE/WebSocket SDK transport API; an equivalent JSON-RPC assertion over stdio does not exercise this transport-specific case'),
      'agent-client-protocol-conductor': ('absent-go-conductor-runtime', 'the Rust conductor orchestration/trace/debugging application. Go carries proxy schema/RPC types but has no conductor chain runner, process orchestration, trace recorder or conductor application API'),
      'agent-client-protocol-polyfill': ('absent-go-polyfill-runtime', 'native-MCP-over-ACP to HTTP polyfill behavior. Go has no polyfill runtime, HTTP MCP bridge or matching proxy implementation API'),
      'agent-client-protocol-rmcp': ('absent-go-rmcp-integration', 'the Rust rmcp MCP-server adapter/queued-tool integration. Go has protocol payload types but no rmcp trait adapter or native MCP server execution surface'),
      'agent-client-protocol-trace-viewer': ('non-acp-application', 'the standalone HTML trace viewer embedding API. No corresponding Go SDK viewer/application API exists'),
      'yopo': ('non-acp-application', 'the yopo one-shot client application/CLI behavior, not the Go SDK client transport/handler API'),
    }
    if entry.get('kind')=='test-function' and '/examples/' in path:
        return 'v2-example-application', f'`{name}` at {path}:{entry["line"]} is an inline example-application test under an explicit Cargo example target requiring unstable_protocol_v2. It checks draft-v2 rendering/session-coordination/agent application behavior rather than Go SDK v1; target metadata and source are retained. This does not exclude a v1 test merely because an unrelated feature gate mentions v2.'
    if path.endswith('/tests/log_privacy.rs'):
        properties={
          'dispatch_and_error_response_warnings_do_not_echo_private_errors':'WARN tracing captures real dispatch/error warnings, omits private marker from logs while retaining it in protocol error responses; two invalid/handler-failed notification cases and three request cases',
          'debug_logs_omit_session_setup_payloads':'DEBUG tracing names session/new but omits MCP environment/header names and values from session setup logs',
          'trace_logs_redact_env_and_header_values':'TRACE tracing records sent/received wire events, retains API_KEY/X-Api-Token/LOGIN_TOKEN names, replaces their values with [REDACTED]',
        }
        return 'absent-go-tracing-subscriber-api', f'`{name}` at {path}:{entry["line"]} checks {properties[name]}. Go exposes no tracing subscriber, configurable DEBUG/TRACE levels or matching message-log events. Wire error/notification correctness is independently assigned; lack of a log capture surface is not a passing privacy test.'
    if entry.get('kind')=='rustdoc-fence' and not entry['rustdoc_candidate']:
        return 'not-rust-code', f'Fence at {path}:{entry["line"]} is explicitly `{entry["info"]}` documentation/diagram/command/configuration, not a Rust doctest. Preserved in the doc census, not counted as executed runtime coverage.'
    if entry.get('kind')=='rustdoc-fence' and 'compile_fail' in entry['info']:
        return 'rust-compile-contract', f'Fence at {path}:{entry["line"]} explicitly expects Rust compilation failure ({entry["info"]}); its trait/ownership/type-checker rejection has no Go runtime counterpart. Code is retained for review rather than claimed as a passed Go test.'
    if crate in descriptions:
        category,detail=descriptions[crate]
        return category, f'`{name}` at {path}:{entry["line"]} exercises {detail}. Not claimed covered by an unrelated core-v1 port.'
    return None


def build(root):
    rev=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip()
    if rev != PIN: raise ValueError(f'expected {PIN}, found {rev}')
    workspace=tomllib.loads((root/'Cargo.toml').read_text())
    members=workspace['workspace']['members']; files=[]; crates=[]
    for member in members:
        manifest=tomllib.loads((root/member/'Cargo.toml').read_text())
        crates.append({'path':member,'name':manifest['package']['name'],'version':manifest['package'].get('version'),
                       'features':manifest.get('features',{}),'lib':manifest.get('lib',{}),
                       'declared_targets':{k:manifest[k] for k in ['bin','example','test','bench'] if k in manifest},
                       'manifest_sha256':hashlib.sha256((root/member/'Cargo.toml').read_bytes()).hexdigest()})
        for p in sorted((root/member).rglob('*.rs')):
            if any(x in p.parts for x in ['target','.git']): continue
            files.append(scan_file(p,root))
    edges=module_contexts(files,crates)
    for f in files:
        crate=next(c for c in crates if f['source_path'].startswith(c['path']+'/'))
        relative=Path(f['source_path']).relative_to(crate['path'])
        if relative.parts[0]=='examples':
            example=relative.parts[1].removesuffix('.rs')
            target=next((x for x in crate['declared_targets'].get('example',[]) if x['name']==example),{})
            f['cargo_target']={'kind':'example','name':example,'test':target.get('test',False),'required_features':target.get('required-features',[])}
        elif relative.parts[0]=='tests':
            name=relative.parts[1].removesuffix('.rs')
            target=next((x for x in crate['declared_targets'].get('test',[]) if x['name']==name),{})
            f['cargo_target']={'kind':'integration-test','name':name,'test':target.get('test',True),'required_features':target.get('required-features',[])}
        else: f['cargo_target']={'kind':'crate-unit-or-doc','test':True,'required_features':[]}
    scenarios=[]
    for f in files:
        crate=f['source_path'].split('/')[1]
        for t in f['tests']:
            t.pop('_offset',None)
            scope='needs-semantic-review'
            cfg=' '.join(t['cfg'])
            if 'unstable_protocol_v2' in cfg: scope='v2-feature-gated-needs-semantic-review'
            elif crate=='agent-client-protocol' or crate=='agent-client-protocol-test': scope='v1-or-shared-candidate'
            function=t['rust_test'].split('::')[-1]
            entry={'id':f['source_path']+':'+str(t['source_line'])+':'+function, 'kind':'test-function',
                   'source':f['source_path'],'function':function,'line':t['source_line'],
                   'source_path':f['source_path'],'crate':crate,**t,
                   'protocol_scope':scope,'domain_hint':initial_domain(f['source_path'],t['rust_test']),
                   'classification':scope,'status':'needs-parent-review','reason':'Source-indexed; domain mapping must supply semantic coverage/disposition. Feature gates alone do not decide v1 behavior.', 'go_tests':[],
                   'inherited_module_contexts':f['inherited_module_contexts'],'cargo_target':f['cargo_target']}
            excluded=inventory_exclusion(entry)
            if excluded:
                entry['classification'],entry['reason']=excluded; entry['status']='not-applicable'
            scenarios.append(entry)
        for d in f['doc_fences']:
            function='doctest@'+str(d['source_line'])
            entry={'id':f['source_path']+':'+str(d['source_line'])+':'+function, 'kind':'rustdoc-fence',
                   'source':f['source_path'],'function':function,'line':d['source_line'],
                   'source_path':f['source_path'],'crate':crate,**d,
                   'domain_hint':doc_owner(f['source_path'],d['source_line']),
                   'status':'needs-parent-review','classification':'delegated-runtime-example',
                   'reason':f'Fence `{d["info"] or "rust-default"}` retained for domain semantic mapping; no_run means compile-only execution by rustdoc, not proof that its described runtime behavior is unimportant. Ignore flags do not silently exclude conceptual scenarios.',
                   'go_tests':[],'inherited_module_contexts':f['inherited_module_contexts']}
            if 'ignore' in d['info']: entry['classification']='ignored-example-needs-semantic-review'
            if 'no_run' in d['info']: entry['classification']='no-run-example-needs-semantic-review'
            excluded=inventory_exclusion(entry)
            if excluded:
                entry['classification'],entry['reason']=excluded; entry['status']='not-applicable'
            scenarios.append(entry)
    macro_records=[]
    for f in files:
        for macro in f['macros']:
            related=[{'test_id':e['id'],'source_line':call['source_line'],'invocation':call['invocation']}
                     for e in scenarios if e['kind']=='test-function'
                     for call in e['macro_calls'] if call['name'].split('::')[-1]==macro['name']]
            is_assertion=f['source_path'].endswith('/tests/protocol_v2.rs')
            semantic_cases={
                'assert_client_request':'14 explicitly V2 client request/result pairs; containing test 811 owns their V2-only disposition.',
                'assert_agent_request':'3 explicitly V2 agent request/result pairs (permission, elicitation, MCP); containing test 811 owns their V2-only disposition.',
                'assert_message_mapping':'2 explicitly V1 mcp/message cases (ClientNotification and AgentRequest); containing test 1042 is mapped by schema.',
                'assert_response_mapping':'1 explicitly V1 MCP ClientResponse tools:[] success carrier; containing test 1042 is mapped by schema.',
                'assert_carriers':'V1 and V2 invocations, each expanding 3 opaque successes plus one inner error preserving explicit-null data/vendor fields/outer metadata; containing test 1093 has partial V1/V2 dispositions.',
            }
            macro_records.append({'id':f['source_path']+':'+str(macro['source_line'])+':macro!'+macro['name'],
                                  'source':f['source_path'],**macro,'invoked_from_tests':related,
                                  'status':'not-applicable',
                                  'classification':'assertion-macro-subcases' if is_assertion else 'rust-helper-or-code-generation',
                                  'review_status':'source-reviewed; concrete invocations belong to containing test mappings',
                                  'semantic_cases':semantic_cases.get(macro['name'],'Concrete semantics and deep-chain repetition remain in enclosing test invocation/loop anchors.'),
                                  'reason':'The Rust macro definition is not an extra standalone runtime test or Go macro API. Its assertion invocations are retained as exact anchors under the containing test IDs; their V1/V2/mixed semantic dispositions must be read there, not treated as skipped scenarios.' if is_assertion else 'This Rust macro generates message/trait/handler/cfg support, not test functions. Concrete semantics and deep-chain repetition remain visible in enclosing test invocation/loop anchors; the macro definition itself is not a standalone Go runtime test.'})
    counts={'rust_files':len(files),'workspace_crates':len(crates),'test_function_declarations':sum(x['kind']=='test-function' for x in scenarios),
            'rustdoc_candidates':sum(x['kind']=='rustdoc-fence' and x['rustdoc_candidate'] for x in scenarios),
            'all_doc_fences':sum(len(f['doc_fences']) for f in files),'doc_attributes':sum(len(f['doc_attributes']) for f in files),
            'macro_definitions':sum(len(f['macros']) for f in files),
            'macro_definitions_containing_test_attributes':sum(m['contains_test_attributes'] for f in files for m in f['macros']),
            'orphan_test_attributes':sum(len(f['orphan_test_attributes']) for f in files),
            'test_loop_anchors':sum(len(x.get('loop_subcases',[])) for x in scenarios),
            'test_helper_call_anchors':sum(len(x.get('helper_calls',[])) for x in scenarios),
            'supplemental_compiler_contracts':sum(len(f['supplemental_compiler_contracts']) for f in files),
            'helper_function_value_anchors':sum(len(f['helper_function_value_references']) for f in files)}
    file_records=[{**{k:v for k,v in f.items() if k not in ['tests','doc_fences']},
                   'test_ids':[e['id'] for e in scenarios if e['kind']=='test-function' and e['source']==f['source_path']],
                   'doc_fence_ids':[e['id'] for e in scenarios if e['kind']=='rustdoc-fence' and e['source']==f['source_path']]}
                  for f in files]
    return {'format_version':1,'upstream':{'repository':'https://github.com/agentclientprotocol/rust-sdk','commit':PIN,'license':'Apache-2.0','rust_version':'1.88.0'},
            'first_port_scope':'Rust SDK clone workspace v1/shared source scenarios; external Cargo dependency tests excluded from first port and retained as separate provenance.',
            'inventory_method':{'kind':'lexical-source-census-with-balanced-delimiters','compiler_expanded':False,
                                'limitations':['No Rust toolchain; not cargo test --list. Counts are source declarations/fences, not semantic subcase cardinality.','Feature-gated behavior and helper/macro/loop subcases require domain reconciliation, not filename-only exclusion.'],
                                'status':'source-census-verified; semantic coverage requires domain mappings and assertion review'},
            'external_dependencies':[{'name':'agent-client-protocol-schema','version':'1.15.1','location':'crates.io external dependency, not this workspace clone',
                                      'source':'Cargo.toml:40; Cargo.lock:146-155','dependency_test_inventory':'separate-external-package',
                                      'archive_url':'https://static.crates.io/crates/agent-client-protocol-schema/agent-client-protocol-schema-1.15.1.crate',
                                      'archive_sha256':'1b2316d2cd561917bc2c16e3c64771c77e24363b56d40b58fdad091d02059992',
                                      'upstream_vcs':{'repository':'https://github.com/agentclientprotocol/agent-client-protocol','revision':'0fc18323a9fdad4e5b4a20ec2f1645dde843fd9b','path':'agent-client-protocol-schema'},
                                      'separate_census':{'rust_files':32,'test_function_declarations':498,'rustdoc_candidates':2,'macro_definitions':6},
                                      'first_port_scope':'excluded-external-dependency-tests',
                                      'reason':'Parent scope decision: first port is SDK clone workspace v1/shared source only. Cargo dependency unit tests are not executed by workspace just test. These 498 declarations are external provenance, not SDK937 or first-port coverage; SDK wrapper tests importing schema types remain inventoried. External v1 schema tests may inform separately attributed additional regressions in the second commit.'}],
            'upstream_revision':PIN,'counts':counts,'crates':crates,'files':file_records,'module_edges':edges,
            'entries':[s for s in scenarios if s['kind']=='test-function'],
            'doctests':[s for s in scenarios if s['kind']=='rustdoc-fence'],
            'macros':macro_records,
            'supplemental':[x for f in files for x in f['supplemental_compiler_contracts']],
            'helper_function_value_references':[{'source':f['source_path'],**x} for f in files for x in f['helper_function_value_references']]}


def compact_inventory(full):
    result={k:v for k,v in full.items() if k not in ['entries','doctests','files','macros']}
    result['format_version']=2
    result['detail_policy']='Committed manifest retains canonical identities, dispositions, cfg/module and exact subcase source anchors. Full code/literal/helper census is reproducible and retained separately under .plan/rust-v1-port-inventory/full-upstream-inventory.json.'
    result['files']=[{k:f[k] for k in ['source_path','sha256','line_count','file_cfg','cargo_target']} for f in full['files']]
    result['entries']=[]
    for entry in full['entries']:
        keep=['id','source','function','line','end_line','status','classification','reason','cfg','domain_hint']
        out={k:entry[k] for k in keep}
        module=entry['rust_test'].rpartition('::')[0]
        if module: out['module']=module
        if entry['loop_subcases']:
            out['loops']=[{'line':x['source_line'],'case_count':x['value_count'],'resolution':x['resolution'],
                           **({'constant':x['constant'],'constant_line':x['constant_source_line']} if 'constant' in x else {})}
                          for x in entry['loop_subcases']]
        if entry['helper_calls']: out['helper_lines']=sorted({x['source_line'] for x in entry['helper_calls']})
        if entry['macro_calls']: out['macro_calls']=[{'line':x['source_line'],'name':x['name']} for x in entry['macro_calls']]
        result['entries'].append(out)
    result['doctests']=[{k:e[k] for k in ['id','source','function','line','end_line','status','classification','reason','info','rustdoc_candidate','contains_assertions','cfg','domain_hint']} for e in full['doctests']]
    result['macros']=[{**{k:m[k] for k in ['id','source','name','source_line','end_line','status','classification','reason','review_status','semantic_cases','contains_test_attributes']},
                       'invocations':[{'entry':x['test_id'],'line':x['source_line']} for x in m['invoked_from_tests']]}
                      for m in full['macros']]
    return result


def manifest_json(value):
    # One source record per line keeps the large census diffable without copying Rust bodies.
    pieces=[]
    for key,data in value.items():
        if key in ['entries','doctests','files','macros','module_edges']:
            encoded='[\n'+',\n'.join('    '+json.dumps(x,separators=(',',':'),ensure_ascii=False) for x in data)+'\n  ]'
        else: encoded=json.dumps(data,indent=2,ensure_ascii=False)
        pieces.append('  '+json.dumps(key)+': '+encoded)
    return '{\n'+',\n'.join(pieces)+'\n}\n'


def main():
    p=argparse.ArgumentParser(); p.add_argument('--rust-root',type=Path,required=True); p.add_argument('--output',type=Path); p.add_argument('--verify',type=Path); p.add_argument('--raw-output',type=Path)
    args=p.parse_args(); result=build(args.rust_root.resolve())
    if args.verify:
        old=json.loads(args.verify.read_text())
        assert [(f['source_path'],f['sha256']) for f in result['files']]==[(f['source_path'],f['sha256']) for f in old['files']], 'source file census/hash mismatch'
        assert [(s['id'],s['line']) for s in result['entries']+result['doctests']]==[(s['id'],s['line']) for s in old['entries']+old['doctests']], 'scenario identity mismatch'
        assert result['counts']==old['counts'], 'census counts changed'
        print('VERIFIED source hashes, scenario identities and lexical census counts')
    if args.raw_output:
        args.raw_output.parent.mkdir(parents=True,exist_ok=True); args.raw_output.write_text(json.dumps(result,indent=2)+'\n')
    if args.output:
        args.output.parent.mkdir(parents=True,exist_ok=True); args.output.write_text(manifest_json(compact_inventory(result)))
    print(json.dumps(result['counts'],indent=2))
    print('test declarations by crate',dict(collections.Counter(x['crate'] for x in result['entries'])))

if __name__=='__main__': main()

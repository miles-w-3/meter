#!/usr/bin/env python3
"""Offline Claude CLI probe: fake API key, loopback Messages API, synthetic data.

Run: python3 scripts/claude-offline-probe.py [path-to-claude]
Set PROBE_PARTS_JSON to a JSON list of input_json_delta strings (default []).
Set PROBE_ARGUMENTS_JSON to the expected replay arguments (default {}).
Set PROBE_RESULT_JSON to an MCP result object (default: text 'tool done').
Set PROBE_DUMP to save synthetic captured request bodies to a file.
This probes the installed CLI, not the provider end-to-end. It does not verify
real prompt-cache behavior. Do not use real credentials. The child gets a fresh
HOME and config directory and proxy settings that reject non-loopback traffic.
"""
import os,sys,json,tempfile,subprocess,threading,http.server,socketserver,time,uuid
capt=[]
parts=json.loads(os.environ.get('PROBE_PARTS_JSON','[]'))
arguments=json.loads(os.environ.get('PROBE_ARGUMENTS_JSON','{}'))
mcp_result=json.loads(os.environ.get('PROBE_RESULT_JSON','{"content":[{"type":"text","text":"tool done"}]}'))
# Match mcpToolResultFromPi's representation for the text-only probe.
result_content=mcp_result.get('content') or [{'type':'text','text':''}]
result_error=bool(mcp_result.get('isError',False))
class H(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_POST(self):
  body=self.rfile.read(int(self.headers.get('Content-Length','0'))); print('HTTP',self.path,len(body),flush=True)
  if '/messages' not in self.path:
   self.send_response(200); self.end_headers();self.wfile.write(b'{}');return
  d=json.loads(body);capt.append(d);typ='tool' if len(capt)==1 else 'text'; mid='msg_mock'+str(len(capt)); model=d.get('model','claude-sonnet-4-5')
  ev=[{'type':'message_start','message':{'id':mid,'type':'message','role':'assistant','model':model,'content':[],'stop_reason':None,'stop_sequence':None,'usage':{'input_tokens':10,'output_tokens':0}}}]
  if typ=='tool':
   ev += [{'type':'content_block_start','index':0,'content_block':{'type':'tool_use','id':'toolu_01test','name':'mcp__custom-tools__empty','input':{}}}]
   ev += [{'type':'content_block_delta','index':0,'delta':{'type':'input_json_delta','partial_json':part}} for part in parts]
   ev += [{'type':'content_block_stop','index':0}];stop='tool_use'
  else:
   ev += [{'type':'content_block_start','index':0,'content_block':{'type':'text','text':''}},{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':'done'}},{'type':'content_block_stop','index':0}];stop='end_turn'
  ev += [{'type':'message_delta','delta':{'stop_reason':stop,'stop_sequence':None},'usage':{'output_tokens':5}},{'type':'message_stop'}]
  payload=''.join('event: '+e['type']+'\ndata: '+json.dumps(e)+'\n\n' for e in ev).encode();self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Content-Length',str(len(payload)));self.end_headers();self.wfile.write(payload)
s=socketserver.ThreadingTCPServer(('127.0.0.1',0),H);threading.Thread(target=s.serve_forever,daemon=True).start();tmp=tempfile.mkdtemp();print('TMP',tmp,'PORT',s.server_address[1],flush=True)
env={k:os.environ[k] for k in ('PATH','TMPDIR','LANG','LC_ALL','SYSTEMROOT') if k in os.environ};env.update({'ANTHROPIC_API_KEY':'sk-ant-api03-local-fake','ANTHROPIC_BASE_URL':'http://127.0.0.1:'+str(s.server_address[1]),'CLAUDE_CONFIG_DIR':tmp,'HOME':tmp,'DISABLE_TELEMETRY':'1','DISABLE_ERROR_REPORTING':'1','CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC':'1','HTTP_PROXY':'http://127.0.0.1:1','HTTPS_PROXY':'http://127.0.0.1:1','ALL_PROXY':'http://127.0.0.1:1','NO_PROXY':'127.0.0.1,localhost','CLAUDE_CODE_DISABLE_AUTO_MEMORY':'1','DISABLE_AUTO_COMPACT':'1','CLAUDE_CODE_TOTAL_TOKENS_REMINDER':'off','CLAUDE_CODE_DISABLE_CLAUDE_MDS':'1','ENABLE_TOOL_SEARCH':'false','ENABLE_CLAUDEAI_MCP_SERVERS':'false'})
args=[(sys.argv[1] if len(sys.argv)>1 else os.path.expanduser('~/.local/bin/claude')),'-p','--input-format','stream-json','--output-format','stream-json','--verbose','--include-partial-messages','--model','claude-sonnet-4-5','--permission-mode','bypassPermissions','--strict-mcp-config','--setting-sources','','--disable-slash-commands','--tools','','--no-session-persistence','--system-prompt','test system','--mcp-config',json.dumps({'mcpServers':{'custom-tools':{'type':'sdk','name':'custom-tools'}}})]
p=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,env=env,cwd=tmp,bufsize=1)
def write(x): print('IN',json.dumps(x),flush=True);p.stdin.write(json.dumps(x)+'\n');p.stdin.flush()
def stderr():
 for l in p.stderr:print('ERR',l.strip(),flush=True)
threading.Thread(target=stderr,daemon=True).start()
def output():
 for l in p.stdout:
  print('OUT',l.strip()[:3000],flush=True)
  try:m=json.loads(l)
  except:continue
  if m.get('type')=='control_request':
   r=m['request'];rid=m['request_id'];print('CONTROL',r,flush=True)
   if r.get('subtype')=='mcp_message':
    msg=r['message'];meth=msg.get('method');id=msg.get('id')
    if meth=='initialize':result={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'custom-tools','version':'1'}}
    elif meth=='tools/list':result={'tools':[{'name':'empty','description':'empty test','inputSchema':{'type':'object','properties':{}}}]}
    elif meth=='tools/call':result=mcp_result
    else:result={}
    if id is None:resp={}
    else:resp={'jsonrpc':'2.0','id':id,'result':result}
    write({'type':'control_response','response':{'subtype':'success','request_id':rid,'response':{'mcp_response':resp}}})
   else:print('UNHANDLED CONTROL',r,flush=True)
threading.Thread(target=output,daemon=True).start();write({'type':'user','message':{'role':'user','content':[{'type':'text','text':'hello'}]}})
for _ in range(300):
 if p.poll()!=None or len(capt)>=2:break
 time.sleep(.1)
print('native done',len(capt),flush=True);p.terminate();p.wait(timeout=5)
p=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,env=env,cwd=tmp,bufsize=1)
threading.Thread(target=stderr,daemon=True).start();threading.Thread(target=output,daemon=True).start()
write({'type':'user','message':{'role':'user','content':[{'type':'text','text':'hello'}]},'shouldQuery':False})
time.sleep(2)
write({'type':'assistant','message':{'role':'assistant','model':'claude-sonnet-4-5','content':[{'type':'tool_use','id':'toolu_01test','name':'mcp__custom-tools__empty','input':arguments}]}})
result_block={'type':'tool_result','tool_use_id':'toolu_01test','content':result_content}
if result_error:result_block['is_error']=True
write({'type':'user','message':{'role':'user','content':[result_block]}})
for _ in range(200):
 if p.poll()!=None or len(capt)>=3:break
 time.sleep(.1)
print('CAPT',len(capt),'status',p.poll(),flush=True);p.terminate();s.shutdown()
if len(capt) != 3:
 raise SystemExit('expected three mock API requests; inspect output above')
if os.environ.get('PROBE_DUMP'):
 with open(os.environ['PROBE_DUMP'],'w') as f:json.dump(capt,f,indent=2)
a,b=capt[1],capt[2]
print('\n=== comparison (native continuation vs fresh-process replay) ===')
for key in sorted(set(a)|set(b)):
 if a.get(key)!=b.get(key):
  if key == 'messages':
   for i,(native,replay) in enumerate(zip(a[key],b[key])):
    print('message',i,'equal:',native==replay)
    if native!=replay:
     print('native:',json.dumps(native,ensure_ascii=False))
     print('replay:',json.dumps(replay,ensure_ascii=False))
  else: print(key,': differs')
print('Assistant tool input native:',a['messages'][1]['content'][0].get('input'))
print('Assistant tool input replay:',b['messages'][1]['content'][0].get('input'))

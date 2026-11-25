import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import http from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

const root = await mkdtemp(join(tmpdir(), "meter-claude-session-guard-"));
const socketPath = join(root, "workspace.sock");
let logs = "";
const child = spawn(process.execPath, ["server/main.ts"], { env: { ...process.env, METER_HOME: root, PI_PROVIDER:"fixture", PI_TRANSPORT:"unix" }, stdio:["ignore","pipe","pipe"] });
for (const pipe of [child.stdout, child.stderr]) pipe.on("data", (chunk) => logs += chunk);
async function request(path, method="GET", body) {
 return await new Promise((resolve, reject) => {
  const data = body === undefined ? undefined : JSON.stringify(body);
  const req = http.request({socketPath,path,method,headers:data===undefined ? {} : {"content-type":"application/json","content-length":Buffer.byteLength(data)}}, async (res) => {
   const chunks=[]; for await (const c of res) chunks.push(c);
   resolve({status:res.statusCode,value:JSON.parse(Buffer.concat(chunks).toString())});
  }); req.on("error",reject); req.end(data);
 });
}
try {
 for (let i=0; i<500 && !logs.includes("Workspace ready"); i++) { if (child.exitCode !== null) throw new Error(logs); await sleep(20); }
 assert.match(logs,/Workspace ready/);
 const provider="/v1/providers/pi-claude-cli";
 assert.equal((await request(provider,"PUT",{enabled:true})).status,200);
 const created=await request("/v1/conversations","POST",{cwd:root});
 assert.equal(created.status,201,JSON.stringify(created.value));
 const sent=await request(`/v1/conversations/${created.value.id}/messages`,"POST",{requestId:"guard-check",mode:"prompt",content:"fixture:slow"});
 assert.equal(sent.status,202,JSON.stringify(sent.value));
 const queued=await request(`/v1/conversations/${created.value.id}/messages`,"POST",{requestId:"guard-followup",mode:"followup",content:"fixture:slow"});
 assert.equal(queued.status,202,JSON.stringify(queued.value));
 const limits={keepaliveMinutes:30,toolWaitMinutes:30,maxParked:2};
 const attempt=await request(provider,"PUT",{sessions:limits});
 assert.equal(attempt.status,409,JSON.stringify(attempt.value));
 assert.equal(attempt.value.code,"busy");
 const status=(await request("/v1/models")).value.providers.find((p)=>p.id==="pi-claude-cli");
 assert.equal(status.sessionsBusy,true);
 assert.notDeepEqual(status.sessions,limits,"refused update must not be saved");
 let accepted;
 for (let i=0;i<200;i++) { await sleep(100); accepted=await request(provider,"PUT",{sessions:limits}); if (accepted.status===200) break; assert.equal(accepted.status,409,JSON.stringify(accepted.value)); }
 assert.equal(accepted.status,200,JSON.stringify(accepted.value));
 const done=accepted.value.providers.find((p)=>p.id==="pi-claude-cli");
 assert.equal(done.sessionsBusy,false);
 assert.deepEqual(done.sessions,limits);
 console.log("Claude session guard: active run rejected, idle update persisted");
} finally {
 child.kill("SIGTERM"); if (child.exitCode===null) await once(child,"exit").catch(()=>{});
 await rm(root,{recursive:true,force:true});
}

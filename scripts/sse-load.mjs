import {writeFile,mkdir} from 'node:fs/promises';
const base=process.env.TEST_BASE_URL||'http://localhost:8080';
const count=Number(process.env.SSE_CLIENTS||100);
if(!Number.isInteger(count)||count<1||count>200)throw new Error('SSE_CLIENTS must be 1..200');
const credentials={email:'sse-'+Date.now()+'@example.test',password:'sse-fixture-password-2026'};
async function request(path,body,cookie,headers={}){
 const response=await fetch(base+path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json',...(cookie?{Cookie:cookie}:{}),...headers},body:body===undefined?undefined:JSON.stringify(body)});
 if(!response.ok)throw new Error(path+' returned '+response.status);
 return response;
}
await request('/api/v1/auth/register',credentials);
const login=await request('/api/v1/auth/login',credentials);
const cookie=login.headers.get('set-cookie').split(';')[0];
const task=await(await request('/api/v1/research/tasks',{goal:'SSE 长连接与完整事件回放验收'},cookie,{'Idempotency-Key':crypto.randomUUID()})).json();
const latencies=[];
const streams=await Promise.all(Array.from({length:count},async()=>{
 const response=await request('/api/v1/research/tasks/'+task.task_id+'/events?after=0',undefined,cookie);
 const decoder=new TextDecoder();let pending='';const sequences=[];
 for await(const chunk of response.body){
  pending+=decoder.decode(chunk,{stream:true});
  let boundary;
  while((boundary=pending.indexOf('\n\n'))>=0){
   const message=pending.slice(0,boundary);pending=pending.slice(boundary+2);
   const data=message.split('\n').find(line=>line.startsWith('data: '));
   if(!data)continue;
   const event=JSON.parse(data.slice(6));
   if(!event.seq)throw new Error('Unexpected reset during live task');
   if(sequences.length&&BigInt(event.seq)<=BigInt(sequences.at(-1)))throw new Error('Out of order or duplicate event');
   sequences.push(event.seq);latencies.push(Math.max(0,Date.now()-Date.parse(event.timestamp)));
  }
 }
 return sequences;
}));
const canonical=JSON.stringify(streams[0]);
if(streams.some(stream=>JSON.stringify(stream)!==canonical))throw new Error('Subscribers saw different event sequences');
const snapshot=await(await request('/api/v1/research/tasks/'+task.task_id,undefined,cookie)).json();
if(snapshot.status!=='SUCCEEDED'||streams[0].at(-1)!==snapshot.event_seq)throw new Error('Missing terminal event');
latencies.sort((a,b)=>a-b);
const p95=latencies[Math.ceil(latencies.length*0.95)-1];
const result={clients:count,events_per_client:streams[0].length,p95_ms:p95,target_ms:2000,target_met:p95<=2000,via_reverse_proxy:false};
await mkdir('.cache',{recursive:true});
await writeFile('.cache/sse-load.json',JSON.stringify(result,null,2)+'\n');
console.log(JSON.stringify(result));
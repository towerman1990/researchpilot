import {spawn} from 'node:child_process';
import {mkdir, readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';

const base=process.env.TEST_BASE_URL || 'http://localhost:8080';
const executable=process.env.BROWSER_PATH || (process.platform==='win32'
  ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : '/usr/bin/google-chrome');
const profile=path.resolve('.cache','browser-smoke-'+Date.now());
await mkdir(profile,{recursive:true});
const child=spawn(executable,['--headless=new','--disable-gpu','--remote-debugging-port=0',
  '--remote-debugging-address=127.0.0.1','--no-first-run','--no-default-browser-check',
  '--user-data-dir='+profile,'about:blank'],{windowsHide:true,stdio:['ignore','ignore','pipe']});
let spawnError;
let browserLog="";
child.stderr?.on("data",data=>{browserLog=(browserLog+data.toString()).slice(-6000);});
child.on('error',error=>{spawnError=error;});
const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let socket, nextID=0;
let ownsSession=false;
const pending=new Map();
function command(method,params={}){
  const id=++nextID;
  return new Promise((resolve,reject)=>{
    const timer=setTimeout(()=>{pending.delete(id);reject(new Error('CDP timeout: '+method));},10000);
    pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value);},reject:error=>{clearTimeout(timer);reject(error);}});
    socket.send(JSON.stringify({id,method,params}));
  });
}
async function evaluate(expression){
  const result=await command('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});
  if(result.exceptionDetails)throw new Error(result.exceptionDetails.text);
  return result.result.value;
}
async function until(expression,label,timeout=20000){
  const end=Date.now()+timeout;
  while(Date.now()<end){
    if(await evaluate("Boolean("+expression+")"))return;
    await wait(150);
  }
  throw new Error('Browser assertion timed out: '+label);
}
const clickText=text=>evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)})?.click()`);
async function setInput(selector,value){
  await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('missing input');const proto=e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;Object.getOwnPropertyDescriptor(proto,'value').set.call(e,${JSON.stringify(value)});e.dispatchEvent(new Event('input',{bubbles:true}));})()`);
}
try{
  let port;
  for(let i=0;i<600;i++){
    if(spawnError)throw spawnError;
    try{port=Number((await readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]);if(port)break;}catch{}
    await wait(100);
  }
  if(!port)throw new Error('Headless browser did not start: '+browserLog.slice(-3000));
  const targets=await (await fetch('http://127.0.0.1:'+port+'/json/list')).json();
  const target=targets.find(item=>item.type==='page');
  socket=new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
  socket.onmessage=event=>{
    const message=JSON.parse(event.data);
    const call=pending.get(message.id);
    if(call){pending.delete(message.id);if(message.error)call.reject(new Error(message.error.message));else call.resolve(message.result);}
  };
  await command('Page.enable');
  await command('Page.navigate',{url:base});
  await until("document.querySelector('input[type=email]')","login form");
  await clickText('创建账户');
  await setInput('input[name=email]','browser-'+Date.now()+'@example.test');
  await setInput('input[name=password]','browser-test-password-2026');
  await clickText('注册并登录');
  await until("!!document.querySelector('textarea')","research workbench");
  ownsSession=true;
  await setInput('textarea','验证浏览器中的 M1 演示研究、实时事件和报告恢复');
  await until("[...document.querySelectorAll('button')].some(b=>b.textContent==='开始研究'&&!b.disabled)","enabled submit");
  await clickText('开始研究');
  await until("document.querySelector('.status')?.textContent==='已完成'","task completes",45000);
  await until("document.querySelector('pre')?.textContent.includes('M1 演示报告')","report rendered");
  await command('Page.reload');
  await until("!!document.querySelector('button.history')","history after reload");
  await evaluate("document.querySelector('button.history').click()");
  await until("document.querySelector('pre')?.textContent.includes('M1 演示报告')","report restored from snapshot");
  await clickText('模型设置');
  await until("!!document.querySelector('input[name=api_key]')","model form");
  await setInput('input[name=base_url]','https://api.deepseek.com');
  await setInput('input[name=model]','browser-fixture');
  await setInput('input[name=api_key]','browser-fixture-key');
  await clickText('加密保存');
  await until("!document.querySelector('input[name=api_key]')","config saved");
  await clickText('模型设置');
  await until("document.body.textContent.includes('密钥尾号 -key')","redacted config loaded");
  await until("document.querySelector('input[name=model]')?.value==='browser-fixture'","saved model name");
  // Submit without a replacement key and verify that the retained key remains configured.
  await clickText('加密保存');
  await until("!document.querySelector('input[name=api_key]')","config retained");
  await clickText('模型设置');
  await until("document.body.textContent.includes('密钥尾号 -key')","retained key tail");
  await until("document.querySelector('input[name=model]')?.value==='browser-fixture'","retained model name");
  await clickText('撤销全部已保存密钥版本');
  await until("![...document.querySelectorAll('button')].some(b=>b.textContent.includes('撤销全部'))","config revoked");
  const shot=await command('Page.captureScreenshot',{format:'png'});
  await writeFile(path.resolve('.cache/browser-smoke.png'),Buffer.from(shot.data,'base64'));
  console.log('Browser smoke passed: auth, research, report, reload, encrypted config, key retention and revocation.');
}catch(error){
  if(socket?.readyState===WebSocket.OPEN){
    try{const shot=await command('Page.captureScreenshot',{format:'png'});await writeFile(path.resolve('.cache/browser-smoke.png'),Buffer.from(shot.data,'base64'));}catch{}
  }
  throw error;
}finally{
  // Only the fresh profile's own fixture account is affected, including failures.
  if(ownsSession&&socket?.readyState===WebSocket.OPEN){
    try{await evaluate(`fetch('/api/v1/model-config',{method:'DELETE',headers:{'Content-Type':'application/json'},body:'{}'}).then(r=>r.ok)`);}catch{}
  }
  if(socket?.readyState===WebSocket.OPEN){try{await command('Browser.close');}catch{}socket.close();}
  await writeFile(path.resolve(".cache/browser-startup.log"),browserLog);
  child.kill();
}
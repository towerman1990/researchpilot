import React, {useEffect, useRef, useState} from 'react';
import {createRoot} from 'react-dom/client';
import './style.css';
import {applyTaskEvent, mergeEvent, terminal} from './events.js';

const labels = {QUEUED:'等待派发',RUNNING:'执行中',RETRY_WAIT:'等待重试',CANCELLING:'取消中',CANCELLED:'已取消',PARTIAL:'部分完成',SUCCEEDED:'已完成',FAILED:'执行失败'};
const path = '/api/v1/research/tasks';
const errors = {
  invalid_credentials:'请检查邮箱和密码。注册密码至少需要 10 字节。',
  registration_conflict:'这个邮箱已注册，请直接登录。',
  invalid_task_request:'请输入 1 到 2000 字的研究目标。',
  idempotency_conflict:'这次提交的内容已变化，请重新提交。',
  insufficient_evidence:'已取得的资料不足以支撑完整回答，已保留部分成果。',
  model_output_truncated:'模型输出达到上限，未生成完整报告；请缩小研究范围或检查模型配置。',
  model_dns_policy_denied:'模型域名解析到保留或私网地址（可能是代理 Fake-IP），请检查 DNS 设置。',
  model_dns_resolution_failed:'模型域名解析失败，请检查服务端 DNS。',
  model_connection_failed:'模型服务连接失败，请检查网络与代理。',
  model_tls_failed:'模型服务 TLS 证书验证失败。',
  model_http_401:'模型服务拒绝认证，请检查 API Key。',
  model_http_403:'模型服务拒绝访问，请检查账号权限。',
  model_http_429:'模型服务限流，请稍后重试。',
  rate_limited:'请求过于频繁，请稍后重试。',
  model_configuration_revoked:'模型密钥已撤销，请重新配置。',
  authentication_required:'会话已过期，请重新登录。',
};

async function api(url, body, headers) {
  const response = await fetch(url, body === undefined ? undefined : {
    method:'POST', headers:{'Content-Type':'application/json', ...headers}, body:JSON.stringify(body)
  });
  if (response.status === 204) return null;
  const data = await response.json();
  if (!response.ok) throw new Error(errors[data.code] || data.code || '请求失败');
  return data;
}

function App() {
  const [user,setUser] = useState(null);
  const [register,setRegister] = useState(false);
  const [history,setHistory] = useState([]);
  const [selected,setSelected] = useState('');
  const [task,setTask] = useState(null);
  const [events,setEvents] = useState([]);
  const [report,setReport] = useState('');
  const [goal,setGoal] = useState('');
  const [error,setError] = useState('');
  const [busy,setBusy] = useState(false);
  const [connection,setConnection] = useState('');
  const [settings,setSettings] = useState(false);
  const [modelInfo,setModelInfo] = useState(null);
  const requestKey = useRef(null);

  useEffect(() => {
    let active = true;
    api('/api/v1/auth/session').then(() => { if(active) setUser(true); },
      () => { if(active) setUser(false); });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!user) return;
    let active = true;
    api(path).then(items => { if(active) setHistory(items); },
      e => { if(active) setError(e.message); });
    return () => { active = false; };
  }, [user]);

  useEffect(() => {
    if (!user || !settings) return;
    let active=true;
    api('/api/v1/model-config').then(value=>{if(active)setModelInfo(value);},e=>{if(active)setError(e.message);});
    return ()=>{active=false;};
  },[user,settings]);

  async function saveModel(event) {
    event.preventDefault();
    setBusy(true);setError('');
    const form=new FormData(event.currentTarget);
    try {
      const response=await fetch('/api/v1/model-config',{
        method:'PUT',headers:{'Content-Type':'application/json'},
        body:JSON.stringify({base_url:form.get('base_url'),model:form.get('model'),api_key:form.get('api_key')})
      });
      const result=await response.json();
      if(!response.ok)throw new Error(result.code);
      event.target.reset();
      setSettings(false);
      setModelInfo(null);
    } catch(e){setError(e.message);}
    finally{setBusy(false);}
  }

  useEffect(() => {
    if (!selected || !user) return;
    let live = true;
    const endpoint = path + '/' + selected;
    let stream;
    let generation=0;
    async function refresh() {
      try {
        const current=await api(endpoint);
        if(!live)return null;
        setTask(old=>old&&BigInt(old.event_seq)>BigInt(current.event_seq)?old:current);
        setHistory(old=>[current,...old.filter(item=>item.task_id!==current.task_id)]);
        if(current.status==='SUCCEEDED'||current.status==='PARTIAL'){
          const result=await api(endpoint+'/report');
          if(live)setReport(result.markdown);
        }
        return current;
      }catch(e){if(live)setError(e.message);return null;}
    }
    async function connect(){
      const ticket=++generation;
      stream?.close();
      const current=await refresh();
      if(!live||ticket!==generation||!current)return;
      if(terminal(current.status)){setConnection('');return;}
      stream=new EventSource(endpoint+'/events?after='+encodeURIComponent(current.event_seq));
      stream.onopen=()=>{if(live)setConnection('实时连接已建立');};
      stream.onerror=()=>{
        if(!live)return;
        setConnection('连接中断，正在重连');
        void refresh().then(current=>{
          if(live&&current&&terminal(current.status)){stream?.close();setConnection('');}
        });
      };
      stream.addEventListener('reset',()=>{
        if(!live)return;
        stream.close();
        setEvents([]);
        setTask(null);
        setConnection('正在恢复任务快照');
        void connect();
      });
      for(const type of ['task_status','step_started','tool_started','tool_finished','usage_updated','coverage_updated','plan_updated','replan_started','report_ready']){
        stream.addEventListener(type,event=>{
          if(!live)return;
          const value=JSON.parse(event.data);
          const item={...value,type};
          setEvents(old=>mergeEvent(old,item));
          setTask(old=>applyTaskEvent(old,item));
          if((type==='task_status'||type==='report_ready')&&terminal(value.payload.status)){
            stream.close();setConnection('');
            void refresh();
          }
        });
      }
    }
    void connect();
    return ()=>{live=false;generation++;stream?.close();};

  }, [selected,user]);

  function select(id) {
    if (id === selected) return;
    setReport(''); setTask(null); setEvents([]); setError(''); setConnection('');
    setSelected(id);
  }

  async function authenticate(event) {
    event.preventDefault();
    setBusy(true); setError('');
    const form = new FormData(event.currentTarget);
    const credentials = {email:form.get('email'),password:form.get('password')};
    try {
      if (register) await api('/api/v1/auth/register',credentials);
      await api('/api/v1/auth/login',credentials);
      setUser(true);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }

  async function submit(event) {
    event.preventDefault();
    setBusy(true); setError('');
    const value = goal.trim();
    if (requestKey.current?.goal !== value) requestKey.current = {goal:value,id:crypto.randomUUID()};
    try {
      const created = await api(path,{goal:value},{'Idempotency-Key':requestKey.current.id});
      select(created.task_id);
      setTask(created);
      setHistory(old => [created,...old.filter(item => item.task_id !== created.task_id)]);
      setGoal('');
      requestKey.current = null;
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }

  async function cancelTask() {
    try {
      const current = await api(path+'/'+selected+'/cancel',{});
      setTask(old => old && BigInt(old.event_seq)>BigInt(current.event_seq)?old:current);
    } catch(e) { setError(e.message); }
  }

  async function probeModel(){
    setBusy(true);setError('');
    try{const result=await api('/api/v1/model-config/probe',{});setModelInfo(old=>({...old,...result}));}
    catch(e){setError(e.message);}finally{setBusy(false);}
  }
  async function revokeModel(){
    setBusy(true);setError('');
    try{
      const response=await fetch('/api/v1/model-config',{method:'DELETE',headers:{'Content-Type':'application/json'},body:'{}'});
      const result=await response.json();if(!response.ok)throw new Error(result.code);
      setModelInfo(old=>({...old,...result}));
    }catch(e){setError(e.message);}finally{setBusy(false);}
  }
  async function logout() {
    try {
      await api('/api/v1/auth/logout',{});
      select('');
      setHistory([]);
      setUser(false);
    } catch (e) { setError(e.message); }
  }

  if (user === null) return <main className="login"><p role="status">正在检查登录状态</p></main>;
  if (!user) return <main className="login">
    <form onSubmit={authenticate}>
      <p className="brand">ResearchPilot</p>
      <h1>{register ? '创建账户' : '登录研究工作台'}</h1>
      <label>邮箱<input type="email" name="email" required autoComplete="email"/></label>
      <label>密码<input type="password" name="password" required minLength={register?10:undefined}
        autoComplete={register?'new-password':'current-password'}/></label>
      {error && <p className="error" role="alert">{error}</p>}
      <button disabled={busy}>{register?'注册并登录':'登录'}</button>
      <button type="button" className="plain" onClick={() => {setRegister(!register);setError('');}}>
        {register?'返回登录':'创建账户'}
      </button>
    </form>
  </main>;

  return <div className="layout">
    <aside>
      <p className="brand">ResearchPilot</p>
      <button className="plain" onClick={() => select('')}>＋ 新建研究</button>
      <button className="plain" onClick={()=>{setModelInfo(null);setSettings(value=>!value);}}>模型设置</button>
      <h2>研究历史</h2>
      {!history.length && <p className="muted">暂无任务</p>}
      {history.map(item => <button key={item.task_id} className="history"
        aria-current={selected===item.task_id?'page':undefined} onClick={() => select(item.task_id)}>
        <span>{item.goal}</span><small>{labels[item.status]}</small>
      </button>)}
      <button className="plain logout" onClick={logout}>退出登录</button>
    </aside>
    <main>
      {error && <p className="error" role="alert">{error}</p>}
      {settings ? (!modelInfo ? <p role="status">正在加载模型设置</p> : <form onSubmit={saveModel}>
        <h1>模型设置</h1>
        {modelInfo?.configured && <p>已配置 {modelInfo.model}，密钥尾号 {modelInfo.key_tail}</p>}
        <label>Base URL<input name="base_url" type="url" defaultValue={modelInfo?.base_url || 'https://api.deepseek.com'} required/></label>
        <label>模型名称<input name="model" defaultValue={modelInfo?.model || 'deepseek-flash'} required/></label>
        <label>API Key<input name="api_key" type="password" autoComplete="off" required={!modelInfo?.configured} placeholder={modelInfo?.configured?'留空保留现有密钥，填写新值则替换':''}/></label>
        <p className="muted">保存后，新任务使用该配置版本。</p>
        <button disabled={busy}>加密保存</button>
        {modelInfo?.configured && <>
          <button type="button" disabled={busy} onClick={probeModel}>检查连接与能力</button>
          <button type="button" disabled={busy} onClick={revokeModel}>撤销全部已保存密钥版本</button>
        </>}
        {modelInfo?.capabilities && <pre>{JSON.stringify(modelInfo.capabilities,null,2)}</pre>}
      </form>) : !selected ? <form onSubmit={submit}>
        <h1>新建研究</h1>
        <label>研究目标<textarea value={goal} onChange={event=>setGoal(event.target.value)}
          maxLength={2000} rows={5} placeholder="例如：比较 Gin 和 Hertz 的 SSE 集成方式" required/></label>
        <p className="muted">配置模型后可研究公开网页和 GitHub 仓库；尚未配置时运行演示任务。</p>
        <button disabled={busy || !goal.trim()}>开始研究</button>
      </form> : <>
        <header><h1>{task?.goal || '研究任务'}</h1><span className="status" role="status">{labels[task?.status] || '正在加载'}</span></header>
        {task && !terminal(task.status) && <button disabled={task.status==='CANCELLING'} onClick={cancelTask}>取消研究</button>}
        {connection && <p className="muted" aria-live="polite">{connection}</p>}
        <div className="result">
          <section><h2>研究报告</h2>{report ? <pre>{report}</pre> : <p className="muted">{task?.status==='FAILED' ? '研究未完成：'+(errors[task.error_code] || task.error_code || '请查看运行记录') : '报告生成后会显示在这里。'}</p>}</section>
          <section><h2>运行记录</h2>
            <ul>{task?.calls?.map(call=><li key={call.tool_call_id}>
              {call.tool_call_id} · 第 {call.attempt_no} 次 · {call.status}
            </li>)}</ul>
            <ol>{events.map(item=><li key={item.seq}>
              {item.type==='tool_finished'?item.payload.summary : labels[item.payload.status] || item.type}
            </li>)}</ol>
          </section>
        </div>
      </>}
    </main>
  </div>;
}

createRoot(document.getElementById('root')).render(<App/>);

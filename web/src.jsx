import React, {useEffect, useRef, useState} from 'react';
import {createRoot} from 'react-dom/client';
import {AuthView, Workspace} from './components/Workspace.jsx';
import 'antd/dist/reset.css';
import './style.css';
import {applyTaskEvent, mergeEvent, mergeTaskSnapshot, terminal} from './events.js';

const path = '/api/v1/research/tasks';
function newRequestID() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
}
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
  trial_exhausted:'免费试用次数已用完，请在模型设置中配置自己的 API Key。',
  trial_unavailable:'免费试用服务暂不可用，请稍后重试或配置自己的 API Key。',
  model_configuration_revoked:'模型密钥已撤销，请重新配置。',
  authentication_required:'会话已过期，请重新登录。',
  invalid_history_cursor:'历史记录分页位置无效，请刷新后重试。',
};

class APIError extends Error {
  constructor(status, code) {
    super(errors[code] || (status >= 500 ? '服务暂不可用，请稍后重试。' : code || '请求失败'));
    this.status = status;
    this.code = code;
  }
}

function isSessionExpired(error) {
  return error.status === 401 && error.code === 'authentication_required';
}

async function requestJSON(url, options) {
  const response = await fetch(url, options);
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new APIError(response.status, body?.code);
  }
  const data = response.status === 204 ? null : await response.json();
  return {data, headers: response.headers};
}

async function api(url, body, headers) {
  const {data} = await requestJSON(url, body === undefined ? undefined : {
    method:'POST', headers:{'Content-Type':'application/json', ...headers}, body:JSON.stringify(body)
  });
  return data;
}

async function fetchHistory(before = '') {
  const {data, headers} = await requestJSON(before ? path + '?before=' + encodeURIComponent(before) : path);
  return {items:data, next:headers.get('X-Next-Cursor') || ''};
}

function appendHistory(current, incoming) {
  const seen = new Set(current.map(item => item.task_id));
  return [...current, ...incoming.filter(item => !seen.has(item.task_id))];
}

function App() {
  const [user,setUser] = useState(null);
  const [sessionCheck,setSessionCheck] = useState(0);
  const [sessionCheckError,setSessionCheckError] = useState('');
  const [register,setRegister] = useState(false);
  const [history,setHistory] = useState([]);
  const [historyCursor,setHistoryCursor] = useState('');
  const [historyBusy,setHistoryBusy] = useState(false);
  const historyLoading = useRef(false);
  const sessionScope = useRef({});
  const selection = useRef({id:''});
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
    api('/api/v1/auth/session').then(() => {
      if (active) setUser(true);
    }, error => {
      if (!active) return;
      if (isSessionExpired(error)) {
        setUser(false);
      } else {
        setSessionCheckError('暂时无法检查登录状态，请稍后重试。');
      }
    });
    return () => { active = false; };
  }, [sessionCheck]);

  function endSession(message = '') {
    sessionScope.current = {};
    historyLoading.current = false;
    requestKey.current = null;
    select('');
    setHistory([]);
    setHistoryCursor('');
    setHistoryBusy(false);
    setGoal('');
    setSettings(false);
    setModelInfo(null);
    setBusy(false);
    setError(message);
    setUser(false);
  }

  function handleSessionError(scope, error) {
    if (sessionScope.current !== scope) return;
    if (isSessionExpired(error)) {
      endSession(error.message);
    } else {
      setError(error.message);
    }
  }

  useEffect(() => {
    if (!user) return;
    let active = true;
    const scope = sessionScope.current;
    fetchHistory().then(({items,next}) => {
      if (!active || sessionScope.current !== scope) return;
      setHistory(old => appendHistory(old, items));
      setHistoryCursor(next);
    }, e => { if (active) handleSessionError(scope, e); });
    return () => { active = false; };
  }, [user]);

  useEffect(() => {
    if (!user) return;
    let active = true;
    const scope = sessionScope.current;
    api('/api/v1/model-config').then(value => {
      if (active && sessionScope.current === scope) setModelInfo(value);
    }, e => { if (active) handleSessionError(scope, e); });
    return ()=>{active=false;};
  },[user,settings,selected]);

  async function loadMoreHistory() {
    if (!historyCursor || historyLoading.current) return;
    const scope = sessionScope.current;
    historyLoading.current = true;
    setHistoryBusy(true);
    try {
      const {items,next} = await fetchHistory(historyCursor);
      if (sessionScope.current !== scope) return;
      setHistory(old => appendHistory(old, items));
      setHistoryCursor(next);
    } catch (e) {
      handleSessionError(scope, e);
    } finally {
      if (sessionScope.current === scope) {
        historyLoading.current = false;
        setHistoryBusy(false);
      }
    }
  }

  async function runModelOperation(request, apply) {
    const scope = sessionScope.current;
    setBusy(true);
    setError('');
    try {
      const result = await request();
      if (sessionScope.current === scope) apply(result);
    } catch (e) {
      handleSessionError(scope, e);
    } finally {
      if (sessionScope.current === scope) setBusy(false);
    }
  }

  async function saveModel(event) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    await runModelOperation(
      () => requestJSON('/api/v1/model-config', {
        method:'PUT', headers:{'Content-Type':'application/json'},
        body:JSON.stringify({base_url:form.get('base_url'),model:form.get('model'),api_key:form.get('api_key')})
      }),
      () => {
        formElement.reset();
        setSettings(false);
        setModelInfo(null);
      },
    );
  }

  useEffect(() => {
    if (!selected || !user || selection.current.id !== selected) return;
    const scope = selection.current;
    const session = sessionScope.current;
    const endpoint = path + '/' + selected;
    let stream;
    let generation = 0;
    const isCurrent = ticket => selection.current === scope && ticket === generation;

    async function refresh(ticket) {
      try {
        const current = await api(endpoint);
        if (!isCurrent(ticket) || current.task_id !== selected) return null;
        setTask(old => isCurrent(ticket) ? mergeTaskSnapshot(old, current, selected) : old);
        setHistory(old => isCurrent(ticket) ? old.map(item => mergeTaskSnapshot(item, current, item.task_id)) : old);
        if (current.status === 'SUCCEEDED' || current.status === 'PARTIAL') {
          const result = await api(endpoint + '/report');
          if (isCurrent(ticket) && result.task_id === selected) setReport(result.markdown);
        }
        return current;
      } catch (e) {
        if (isCurrent(ticket)) handleSessionError(session, e);
        return null;
      }
    }

    async function connect() {
      const ticket = ++generation;
      stream?.close();
      stream = null;
      const current = await refresh(ticket);
      if (!isCurrent(ticket) || !current) return;
      if (terminal(current.status)) {
        setConnection('');
        return;
      }
      const source = new EventSource(endpoint + '/events?after=' + encodeURIComponent(current.event_seq));
      stream = source;
      const ownsStream = () => isCurrent(ticket) && stream === source;
      source.onopen = () => { if (ownsStream()) setConnection('实时连接已建立'); };
      source.onerror = () => {
        if (!ownsStream()) return;
        setConnection('连接中断，正在重连');
        void refresh(ticket).then(snapshot => {
          if (ownsStream() && snapshot && terminal(snapshot.status)) {
            source.close();
            stream = null;
            setConnection('');
          }
        });
      };
      source.addEventListener('reset', () => {
        if (!ownsStream()) return;
        setEvents([]);
        setTask(null);
        setConnection('正在恢复任务快照');
        void connect();
      });
      for (const type of ['task_status','step_started','tool_started','tool_finished','usage_updated','coverage_updated','plan_updated','replan_started','report_ready']) {
        source.addEventListener(type, event => {
          if (!ownsStream()) return;
          const value = JSON.parse(event.data);
          if (value.task_id !== selected) return;
          const item = {...value,type};
          setEvents(old => isCurrent(ticket) ? mergeEvent(old,item) : old);
          setTask(old => isCurrent(ticket) && old?.task_id === selected ? applyTaskEvent(old,item) : old);
          if ((type === 'task_status' || type === 'report_ready') && terminal(value.payload.status)) {
            source.close();
            stream = null;
            setConnection('');
            void refresh(ticket);
          }
        });
      }
    }

    void connect();
    return () => { generation++; stream?.close(); };
  }, [selected,user]);

  function select(id) {
    if (id === selection.current.id) return;
    selection.current = {id};
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
      sessionScope.current = {};
      setUser(true);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }

  async function submit(event) {
    event.preventDefault();
    setBusy(true); setError('');
    const value = goal.trim();
    if (requestKey.current?.goal !== value) requestKey.current = {goal:value,id:newRequestID()};
    const scope = sessionScope.current;
    try {
      const created = await api(path,{goal:value},{'Idempotency-Key':requestKey.current.id});
      if (sessionScope.current !== scope) return;
      select(created.task_id);
      setTask(created);
      setHistory(old => [created,...old.filter(item => item.task_id !== created.task_id)]);
      setGoal('');
      requestKey.current = null;
    } catch (e) {
      handleSessionError(scope, e);
    } finally {
      if (sessionScope.current === scope) setBusy(false);
    }
  }

  function downloadReport() {
    if (!report || !selected) return;
    const blob = new Blob([report], {type:'text/markdown;charset=utf-8'});
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `researchpilot-${selected}.md`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  async function cancelTask() {
    const scope = selection.current;
    const session = sessionScope.current;
    if (!scope.id) return;
    try {
      const current = await api(path + '/' + scope.id + '/cancel', {});
      if (selection.current !== scope || current.task_id !== scope.id) return;
      setTask(old => selection.current === scope ? mergeTaskSnapshot(old, current, scope.id) : old);
      setHistory(old => selection.current === scope ? old.map(item => mergeTaskSnapshot(item, current, item.task_id)) : old);
    } catch (e) {
      if (selection.current === scope) handleSessionError(session, e);
    }
  }

  function probeModel() {
    return runModelOperation(
      () => api('/api/v1/model-config/probe', {}),
      result => setModelInfo(old => ({...old,...result})),
    );
  }

  function revokeModel() {
    return runModelOperation(
      () => requestJSON('/api/v1/model-config', {
        method:'DELETE', headers:{'Content-Type':'application/json'}, body:'{}',
      }),
      ({data}) => setModelInfo(old => ({...old,...data,capabilities:undefined,errors:undefined})),
    );
  }

  async function logout() {
    const scope = sessionScope.current;
    try {
      await api('/api/v1/auth/logout', {});
      if (sessionScope.current === scope) endSession();
    } catch (e) {
      handleSessionError(scope, e);
    }
  }

  if (user === null) return <main className="auth-page">
    {sessionCheckError ? <div><p role="alert">{sessionCheckError}</p><button onClick={() => {
      setSessionCheckError('');
      setSessionCheck(attempt => attempt + 1);
    }}>重试</button></div> : <p role="status">正在检查登录状态</p>}
  </main>;
  if (!user) return <AuthView {...{register,setRegister,authenticate,error,setError,busy}}/>;
  return <Workspace {...{history,historyCursor,historyBusy,loadMoreHistory,selected,select,settings,setSettings,setModelInfo,logout,error,modelInfo,goal,setGoal,submit,busy,saveModel,probeModel,revokeModel,task,events,report,connection,errors,cancelTask,downloadReport}}/>;
}

createRoot(document.getElementById('root')).render(<App/>);

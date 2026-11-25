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
  invalid_research_materials:'每项任务最多两份 UTF-8 文本资料，每份最多 24 KiB。请检查文件名和内容。',
  model_profile_not_found:'找不到这套可用的模型配置。',
  model_profile_changed:'当前配置已变化，请刷新模型设置后再编辑。',
  api_key_required:'新增配置或更换 Base URL 时，请填写对应的 API Key。',
  invalid_research_preferences:'请选择有效的研究偏好。',
  invalid_research_schedule:'请输入研究目标并选择有效的未来时间。',
  active_schedule_not_found:'计划已结束或已取消，请刷新列表。',
  invalid_plan:'计划内容不符合要求，请检查每一步的资料来源。',
  invalid_plan_schema:'没能生成可审批的计划，请调整目标后重试。',
  plan_not_awaiting_approval:'计划状态已变化，请查看最新任务进度。',
  model_not_configured:'请配置个人模型或使用平台试用。',
  report_not_ready:'请从已生成报告的任务发起追问。',
  task_not_found:'找不到这项研究任务。',
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
  guest_trial_exhausted:'今日 3 次访客试用已用完，请登录或注册后继续。',
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

async function fetchOverview(signal) {
  const {data} = await requestJSON('/api/v1/research/overview', {signal});
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
  const [guestReady,setGuestReady] = useState(false);
  const [authPrompt,setAuthPrompt] = useState(null);
  const [authError,setAuthError] = useState('');
  const [authBusy,setAuthBusy] = useState(false);
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
  const [preferences,setPreferences] = useState(null);
  const [preferencesError,setPreferencesError] = useState(false);
  const [modelInfo,setModelInfo] = useState(null);
  const requestKey = useRef(null);

  useEffect(() => {
    let active = true;
    const scope = sessionScope.current;
    api('/api/v1/auth/session').then(() => {
      if (active && sessionScope.current === scope) setUser(true);
    }, () => {
      if (active && sessionScope.current === scope) setUser(false);
    });
    return () => { active = false; };
  }, []);

  async function refreshGuest(scope) {
    const info = await api('/api/v1/auth/guest', {});
    if (sessionScope.current === scope) {
      setModelInfo(info);
      setGuestReady(true);
    }
  }

  useEffect(() => {
    if (user !== false) return;
    const scope = sessionScope.current;
    void refreshGuest(scope).catch(e => handleSessionError(scope, e));
  }, [user]);

  function openAuth(message = '') {
    setRegister(false);
    setAuthError('');
    setAuthPrompt(message);
  }

  function endSession({keepDraft = false} = {}) {
    sessionScope.current = {};
    historyLoading.current = false;
    requestKey.current = null;
    select('');
    setHistory([]);
    setHistoryCursor('');
    setHistoryBusy(false);
    if (!keepDraft) setGoal('');
    setGuestReady(false);
    setSettings(false);
    setPreferences(null);
    setPreferencesError(false);
    setModelInfo(null);
    setBusy(false);
    setError('');
    setUser(false);
  }

  function handleSessionError(scope, error) {
    if (sessionScope.current !== scope) return;
    if (isSessionExpired(error)) {
      if (user) endSession();
      openAuth(error.message);
    } else {
      setError(error.message);
    }
  }

  useEffect(() => {
    if (!user && !guestReady) return;
    let active = true;
    const scope = sessionScope.current;
    fetchHistory().then(({items,next}) => {
      if (!active || sessionScope.current !== scope) return;
      setHistory(old => appendHistory(old, items));
      setHistoryCursor(next);
    }, e => { if (active) handleSessionError(scope, e); });
    return () => { active = false; };
  }, [user,guestReady]);

  useEffect(() => {
    if (!user) return;
    let active = true;
    const scope = sessionScope.current;
    api('/api/v1/model-config').then(value => {
      if (active && sessionScope.current === scope) setModelInfo(value);
    }, e => { if (active) handleSessionError(scope, e); });
    return ()=>{active=false;};
  },[user,settings,selected]);

  async function loadPreferences() {
    const scope = sessionScope.current;
    setPreferencesError(false);
    try {
      const value = await api('/api/v1/research/preferences');
      if (sessionScope.current === scope) setPreferences(value);
    } catch (e) {
      if (sessionScope.current === scope) setPreferencesError(true);
      handleSessionError(scope, e);
    }
  }

  useEffect(() => {
    if (user && settings === 'preferences') void loadPreferences();
  }, [user, settings]);

  async function savePreferences(value) {
    const scope = sessionScope.current;
    let saved = false;
    await runSettingsOperation(
      () => requestJSON('/api/v1/research/preferences', {
        method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(value)
      }),
      ({data}) => { setPreferences(data); saved = true; },
    );
    return saved && sessionScope.current === scope;
  }

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

  async function runSettingsOperation(request, apply) {
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

  async function saveModel(event, newProfile, expectedVersion) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    let stored = false;
    await runSettingsOperation(async () => {
      await requestJSON('/api/v1/model-config', {
        method:'PUT', headers:{'Content-Type':'application/json'},
        body:JSON.stringify({
          base_url:form.get('base_url'),model:form.get('model'),api_key:form.get('api_key'),
          label:form.get('label'),new_profile:newProfile,expected_version:expectedVersion,
        }),
      });
      return api('/api/v1/model-config');
    }, status => {
      setModelInfo(status);
      stored = true;
    });
    return stored;
  }

  function activateModel(profileID) {
    return runSettingsOperation(
      () => api('/api/v1/model-config/activate',{profile_id:profileID}),
      setModelInfo,
    );
  }

  useEffect(() => {
    if (!selected || (!user && !guestReady) || selection.current.id !== selected) return;
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
      for (const type of ['task_status','step_started','tool_started','tool_finished','usage_updated','coverage_updated','plan_updated','replan_started','citation_review_started','citation_review_completed','report_ready']) {
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
          } else if (type === 'task_status' && value.payload.status === 'AWAITING_APPROVAL') {
            void refresh(ticket);
          }
        });
      }
    }

    void connect();
    return () => { generation++; stream?.close(); };
  }, [selected,user,guestReady]);

  function select(id) {
    if (id === selection.current.id) return;
    selection.current = {id};
    setReport(''); setTask(null); setEvents([]); setError(''); setConnection('');
    setSelected(id);
  }

  async function authenticate(event) {
    event.preventDefault();
    setAuthBusy(true); setAuthError('');
    const form = new FormData(event.currentTarget);
    const credentials = {email:form.get('email'),password:form.get('password')};
    try {
      if (register) await api('/api/v1/auth/register',credentials);
      await api('/api/v1/auth/login',credentials);
      endSession({keepDraft: true});
      setAuthPrompt(null);
      setUser(true);
    } catch (e) { setAuthError(e.message); }
    finally { setAuthBusy(false); }
  }

  async function submit(event, reviewPlan, attachments, reviewCitations) {
    event.preventDefault();
    await submitTask(goal, '', reviewPlan, attachments, reviewCitations);
  }

  async function submitTask(question, parentTaskID = '', reviewPlan = false, attachments = [], reviewCitations = false) {
    setBusy(true); setError('');
    const value = question.trim();
    const attachmentKey = JSON.stringify(attachments);
    if (requestKey.current?.goal !== value || requestKey.current?.parentTaskID !== parentTaskID || requestKey.current?.reviewPlan !== reviewPlan || requestKey.current?.reviewCitations !== reviewCitations || requestKey.current?.attachmentKey !== attachmentKey) {
      requestKey.current = {goal:value,parentTaskID,reviewPlan,reviewCitations,attachmentKey,id:newRequestID()};
    }
    const request = requestKey.current;
    const scope = sessionScope.current;
    const source = selection.current;
    try {
      if (!user && !guestReady) await refreshGuest(scope);
      if (sessionScope.current !== scope) return;
      const payload = {goal:value};
      if (parentTaskID) payload.parent_task_id = parentTaskID;
      if (reviewPlan) payload.review_plan = true;
      if (reviewCitations) payload.review_citations = true;
      if (attachments.length) payload.attachments = attachments;
      const created = await api(path,payload,{'Idempotency-Key':request.id});
      if (sessionScope.current !== scope) return;
      if (selection.current === source) {
        select(created.task_id);
        setTask(created);
      }
      setHistory(old => [created,...old.filter(item => item.task_id !== created.task_id)]);
      if (!parentTaskID) setGoal('');
      if (requestKey.current === request) requestKey.current = null;
      if (!user) void refreshGuest(scope).catch(() => {});
    } catch (e) {
      if (sessionScope.current !== scope) return;
      if (e.code === 'guest_trial_exhausted') {
        setModelInfo(info => ({...info, trial_remaining:0}));
        openAuth(e.message);
      } else {
        handleSessionError(scope, e);
      }
    } finally {
      if (sessionScope.current === scope) setBusy(false);
    }
  }

  async function approvePlan(plan, sequence) {
    const current = selection.current;
    const scope = sessionScope.current;
    setBusy(true); setError('');
    try {
      const updated = await api(path+'/'+current.id+'/plan/approve',{event_seq:sequence,plan});
      if (selection.current === current) {
        setTask(old => mergeTaskSnapshot(old, updated, current.id));
      }
      if (sessionScope.current === scope) {
        setHistory(old => old.map(item => mergeTaskSnapshot(item, updated, item.task_id)));
      }
    } catch (error) {
      handleSessionError(scope, error);
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
    return runSettingsOperation(
      () => api('/api/v1/model-config/probe', {}),
      result => setModelInfo(old => ({...old,...result})),
    );
  }

  function revokeModel() {
    return runSettingsOperation(
      () => requestJSON('/api/v1/model-config', {
        method:'DELETE', headers:{'Content-Type':'application/json'}, body:'{}',
      }),
      ({data}) => setModelInfo(old => ({...old,...data,profiles:[],capabilities:undefined,errors:undefined})),
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

  const authModal = <AuthView open={authPrompt !== null} message={authPrompt}
    onClose={() => setAuthPrompt(null)} error={authError} setError={setAuthError} busy={authBusy}
    {...{register,setRegister,authenticate}}/>;
  return <Workspace user={user} openAuth={openAuth} authModal={authModal} sessionLoading={user === null} readSource={api} readOverview={fetchOverview} {...{history,historyCursor,historyBusy,loadMoreHistory,selected,select,settings,setSettings,setModelInfo,logout,error,modelInfo,goal,setGoal,submit,submitTask,approvePlan,busy,saveModel,activateModel,probeModel,revokeModel,preferences,preferencesError,loadPreferences,savePreferences,task,events,report,connection,errors,cancelTask,downloadReport}}/>;
}

createRoot(document.getElementById('root')).render(<App/>);

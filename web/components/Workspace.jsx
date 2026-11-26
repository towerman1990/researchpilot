import React from 'react';
const ReactMarkdown = React.lazy(() => import('react-markdown'));
import {Alert, Button, Card, Checkbox, ConfigProvider, Empty, Input, Modal, Progress, Space, Tag, Typography} from 'antd';
import {ArrowRightOutlined, BarChartOutlined, BookOutlined, CalendarOutlined, CheckCircleOutlined, CompassOutlined, DownloadOutlined, AppstoreOutlined, ExperimentOutlined, FileTextOutlined, HistoryOutlined, LogoutOutlined, PlusOutlined, PrinterOutlined, SearchOutlined, SettingOutlined, AuditOutlined, SlidersOutlined, ThunderboltOutlined} from '@ant-design/icons';
import {ResearchEvaluation} from './ResearchEvaluation.jsx';
import {ResearchSchedules} from './ResearchSchedules.jsx';
import {ReportEvidence} from './ReportEvidence.jsx';
import {ReportFeedback} from './ReportFeedback.jsx';
import {TaskOverview} from './TaskOverview.jsx';
import {UsageView} from './UsageView.jsx';
import {terminal} from '../events.js';
import brandIcon from '../researchpilot-icon.png';

const {Text} = Typography;
const {TextArea} = Input;
const labels = {QUEUED:'等待派发',RUNNING:'执行中',AWAITING_APPROVAL:'待确认计划',RETRY_WAIT:'等待重试',CANCELLING:'取消中',CANCELLED:'已取消',PARTIAL:'部分完成',SUCCEEDED:'已完成',FAILED:'执行失败'};
const statusColors = {SUCCEEDED:'success',RUNNING:'processing',AWAITING_APPROVAL:'gold',QUEUED:'processing',PARTIAL:'warning',FAILED:'error',CANCELLED:'default',RETRY_WAIT:'warning'};
const eventLabels = {task_status:'任务状态更新',step_started:'开始执行步骤',tool_started:'开始采集资料',tool_finished:'资料采集完成',usage_updated:'用量已更新',coverage_updated:'证据覆盖已更新',plan_updated:'研究计划已更新',replan_started:'调整研究计划',citation_review_started:'复核报告引用',citation_review_completed:'引用复核完成',report_ready:'研究报告已生成'};

export function Theme({children}) {
  return <ConfigProvider button={{autoInsertSpace:false}} theme={{token:{colorPrimary:'#2354d8',borderRadius:12,fontFamily:'Inter, "Microsoft YaHei", sans-serif',colorText:'#182540',colorBgLayout:'#f5f7fc'}}}>{children}</ConfigProvider>;
}
function Brand() { return <div className="brand"><img className="brand-mark" src={brandIcon} width={38} height={38} alt="" /><span><strong>ResearchPilot</strong><small>你的技术研究助手</small></span></div>; }
function ErrorNotice({error}) {return error ? <Alert className="error-notice" type="error" showIcon message={error} role="alert"/> : null;}

export function AuthView({open, onClose, message, register, setRegister, authenticate, error, setError, busy}) {
  return <Modal open={open} title={register?'创建账户':'登录研究工作台'} footer={null}
    onCancel={onClose} closable={!busy} keyboard={!busy} maskClosable={!busy} destroyOnHidden>
    {message && <p className="muted">{message}</p>}
    <ErrorNotice error={error}/>
    <form onSubmit={authenticate} className="auth-form">
      <label>邮箱<Input size="large" type="email" name="email" required autoComplete="email" placeholder="name@example.com"/></label>
      <label>密码<Input.Password size="large" name="password" required minLength={register?10:undefined} autoComplete={register?'new-password':'current-password'} placeholder={register?'至少 10 字节':'输入密码'}/></label>
      <Button block size="large" type="primary" htmlType="submit" loading={busy}>{register?'注册并登录':'登录'}</Button>
      <Button block type="link" disabled={busy} onClick={()=>{setRegister(!register);setError('');}}>{register?'返回登录':'创建账户'}</Button>
    </form>
  </Modal>;
}

function Sidebar({history, historyCursor, historyBusy, loadMoreHistory, selected, select, settings, setSettings, setModelInfo, logout, user, openAuth}) {
  function newResearch(){setSettings(false);select('');}
  return <aside className="sidebar"><Brand/><Button type="primary" size="large" block icon={<PlusOutlined/>} onClick={newResearch}>新建研究</Button>
    <nav className="side-nav"><button className={!settings?'nav-item active':'nav-item'} onClick={newResearch}><CompassOutlined/> 研究工作台</button>{user && <><button className={settings==='taskOverview'?'nav-item active':'nav-item'} onClick={()=>{select('');setSettings('taskOverview');}}><AppstoreOutlined/> 多任务工作区</button><button className={settings==='schedules'?'nav-item active':'nav-item'} onClick={()=>{select('');setSettings('schedules');}}><CalendarOutlined/> 定时研究</button><button className={settings==='model'?'nav-item active':'nav-item'} onClick={()=>{if(settings!=='model'){setModelInfo(null);setSettings('model');}}}><SettingOutlined/> 模型设置</button><button className={settings==='preferences'?'nav-item active':'nav-item'} onClick={()=>setSettings('preferences')}><SlidersOutlined/> 研究偏好</button><button className={settings==='usage'?'nav-item active':'nav-item'} onClick={()=>setSettings('usage')}><BarChartOutlined/> 研究用量</button>
      <button className={settings==='evaluation'?'nav-item active':'nav-item'} onClick={()=>{select('');setSettings('evaluation');}}><AuditOutlined/> 研究评估</button></>}</nav>
    <div className="side-heading"><span>研究历史</span><HistoryOutlined/></div><div className="history-list">{!history.length && <p className="side-empty">暂无研究记录</p>}{history.map(item=><button key={item.task_id} className="history" aria-current={!settings&&selected===item.task_id?'page':undefined} onClick={()=>{setSettings(false);select(item.task_id);}}><FileTextOutlined/><span><b>{item.goal}</b><small>{labels[item.status]}</small></span></button>)}{historyCursor && <Button type="link" block loading={historyBusy} onClick={loadMoreHistory}>加载更早的记录</Button>}</div>
    <div className="sidebar-footer"><span className="online-dot"/> {user ? '研究空间' : '访客模式'}{user
      ? <Button type="text" icon={<LogoutOutlined/>} onClick={logout}>退出登录</Button>
      : <Button type="text" onClick={() => openAuth()}>登录 / 注册</Button>}</div>
  </aside>;
}
function TrialInfo({modelInfo, user}) {
  if (!user && !modelInfo) return <><ThunderboltOutlined/> 访客每天可试用 3 次</>;
  if (modelInfo?.guest) return <><ThunderboltOutlined/> 今日访客试用剩余 {modelInfo.trial_remaining} / {modelInfo.trial_limit} 次{!modelInfo.trial_enabled && '（演示模式）'}</>;
  if (modelInfo?.configured) return <><CheckCircleOutlined/> 当前使用自己的模型配置</>;
  if (modelInfo?.trial_enabled) return <><ThunderboltOutlined/> 免费试用剩余 {modelInfo.trial_remaining} / {modelInfo.trial_limit} 次</>;
  return <><ExperimentOutlined/> 未配置模型时将运行演示任务</>;
}
function NewResearch({goal,setGoal,submit,busy,modelInfo,user,sessionLoading}) {
  const [demoKnowledge,setDemoKnowledge] = React.useState(false);
  const [reviewPlan,setReviewPlan] = React.useState(false);
  const [reviewCitations,setReviewCitations] = React.useState(false);
  const [attachments,setAttachments] = React.useState([]);
  const [reading,setReading] = React.useState(false);
  const [attachmentError,setAttachmentError] = React.useState('');

  async function attach(event) {
    const files = Array.from(event.target.files);
    event.target.value = '';
    if (!files.length) return;
    setReading(true);
    setAttachmentError('');
    try {
      if (files.length > 2) throw Error('每项任务最多上传两份资料。');
      const result = await Promise.all(files.map(async file => {
        if (!/\.(txt|md)$/i.test(file.name) || file.size > 24 * 1024) {
          throw Error('请选择不超过 24 KiB 的 TXT 或 Markdown 文件。');
        }
        let content;
        try {
          content = new TextDecoder('utf-8',{fatal:true}).decode(await file.arrayBuffer());
        } catch {
          throw Error('文件需使用 UTF-8 文本编码。');
        }
        if (!content.trim() || content.includes('\0')) throw Error('请选择包含正文的文本文件。');
        return {name:file.name,content};
      }));
      setAttachments(result);
    } catch (error) {
      setAttachmentError(error.message);
    } finally {
      setReading(false);
    }
  }
  return <div className="new-research"><div className="page-kicker"><span className="kicker-dot"/> RESEARCH WORKSPACE</div><h1>今天想研究什么？</h1><p className="hero-description">告诉 ResearchPilot 你的问题，我们会帮你整理资料、追踪过程并生成有来源的报告。</p>
    <Card className="composer-card" bordered={false}><form onSubmit={event => submit(event,reviewPlan,attachments,reviewCitations,demoKnowledge)}><div className="composer-title"><span className="feature-icon"><SearchOutlined/></span><div><strong>发起一项研究</strong><p>描述背景、目标和你希望了解的问题</p></div></div><label className="field-label" htmlFor="research-goal">研究目标</label><TextArea id="research-goal" value={goal} onChange={e=>setGoal(e.target.value)} maxLength={2000} rows={5} placeholder="例如，我想为小团队选一款项目管理工具，请比较 Notion、Linear 和飞书的协作方式、成本与适用场景。" required showCount/>
      <div className="research-attachments">
        <label htmlFor="research-materials">补充研究资料（可选）</label>
        <input id="research-materials" type="file" accept=".txt,.md" multiple disabled={busy || reading} onChange={attach}/>
        <small className="muted">最多两份 UTF-8 文本，每份 24 KiB。</small>
        {attachments.length > 0 && <ul>{attachments.map((file,index) => <li key={index}>{file.name}</li>)}</ul>}
        {attachmentError && <Alert type="error" showIcon message={attachmentError}/>}
        {(attachments.length > 0 || attachmentError) && <Button type="link" disabled={busy || reading}
          onClick={() => {setAttachments([]);setAttachmentError('');}}>清除资料</Button>}
      </div>
      <div className="plan-review-option"><Checkbox checked={demoKnowledge} onChange={event => setDemoKnowledge(event.target.checked)}>先查公开知识库（.NET、ASP.NET Core、Go）</Checkbox></div>
      <div className="plan-review-option"><Checkbox checked={reviewPlan} onChange={event => setReviewPlan(event.target.checked)}>先生成计划，由我确认后开始查资料</Checkbox></div>
      <div className="plan-review-option"><Checkbox checked={reviewCitations} onChange={event => setReviewCitations(event.target.checked)}>发布前复核引用（额外消耗 Token）</Checkbox></div>
      <div className="composer-footer"><span className="model-hint"><TrialInfo modelInfo={modelInfo} user={user}/></span><Button type="primary" size="large" htmlType="submit" disabled={sessionLoading||busy||reading||Boolean(attachmentError)||!goal.trim()} loading={busy} icon={<ArrowRightOutlined/>} iconPosition="end">开始研究</Button></div></form></Card>
    <div className="tips-header"><strong>从这里开始</strong><span>让研究目标更清晰</span></div><div className="tip-grid"><div><span><CompassOutlined/></span><strong>明确研究方向</strong><p>描述背景和需要解决的问题。</p></div><div><span><BookOutlined/></span><strong>提供参考资料</strong><p>可以附上公开网页或 GitHub 链接。</p></div><div><span><FileTextOutlined/></span><strong>查看引用报告</strong><p>跟踪过程并核对报告中的来源。</p></div></div>
  </div>;
}
function SettingsView({modelInfo,saveModel,activateModel,probeModel,revokeModel,busy}) {
  const [adding,setAdding] = React.useState(false);
  if (!modelInfo) return <p role="status">正在加载模型设置</p>;
  const profiles = modelInfo.profiles || [];
  const active = profiles.find(profile => profile.active);
  const editing = modelInfo.configured && !adding;
  function submit(event) {
    void saveModel(event, !editing, editing ? modelInfo.version : 0).then(saved => {
      if (saved) setAdding(false);
    });
  }
  return <div className="settings-page">
    <h1>模型设置</h1>
    <p className="page-description">选择新任务默认使用的模型配置。</p>
    {modelInfo.trial_enabled && <Alert className="settings-alert" type="info" showIcon
      message={`免费试用剩余 ${modelInfo.trial_remaining} / ${modelInfo.trial_limit} 次。使用自己的 API Key 不消耗试用次数。`}/>}
    {profiles.length > 0 && <Card className="settings-card" title="已保存配置"
      extra={<Button icon={<PlusOutlined/>} disabled={busy || adding} onClick={()=>setAdding(true)}>新增配置</Button>}>
      <ul className="model-profiles">
        {profiles.map(profile => <li key={profile.profile_id}>
          <div>
            <strong>{profile.label || profile.model}</strong>
            <p className="muted">{profile.label ? `${profile.model} · ` : ''}{profile.base_url} · 密钥尾号 {profile.key_tail}</p>
          </div>
          {profile.active ? <Tag color="blue">当前使用</Tag>
            : <Button size="small" disabled={busy} onClick={()=>void activateModel(profile.profile_id)}>设为当前</Button>}
        </li>)}
      </ul>
    </Card>}
    <Card className="settings-card" title={editing ? '编辑当前配置' : '新增模型配置'}>
      <form key={editing ? modelInfo.version : 'new'} onSubmit={submit}>
        <label className="field-label">配置名称<Input name="label" maxLength={80}
          defaultValue={editing ? active?.label : ''} placeholder="可选，例如日常研究"/></label>
        <label className="field-label">Base URL<Input name="base_url" type="url"
          defaultValue={editing ? modelInfo.base_url : 'https://api.deepseek.com'} required/></label>
        <label className="field-label">模型名称<Input name="model"
          defaultValue={editing ? modelInfo.model : 'deepseek-flash'} required/></label>
        <label className="field-label">API Key<Input.Password name="api_key" autoComplete="off"
          required={!editing} placeholder={editing ? '连接地址未变时可留空保留密钥' : '输入此配置的 API Key'}/></label>
        <Space wrap>
          <Button type="primary" htmlType="submit" loading={busy}>保存并使用</Button>
          {adding && <Button disabled={busy} onClick={()=>setAdding(false)}>取消新增</Button>}
          {editing && <Button disabled={busy} onClick={probeModel}>检查连接与能力</Button>}
        </Space>
        {editing && modelInfo.capabilities && <pre>{JSON.stringify(modelInfo.capabilities,null,2)}</pre>}
      </form>
    </Card>
    {modelInfo.configured && <div className="revoke-models">
      <Button danger type="text" disabled={busy} onClick={revokeModel}>撤销全部已保存密钥版本</Button>
    </div>}
  </div>;
}
const preferenceFields = [
  {key:'sources',label:'优先查阅的来源',options:[['official','官方文档与发布说明'],['github','GitHub 上游仓库与实现']]},
  {key:'detail',label:'报告详略',options:[['brief','简明'],['detailed','详细']]},
  {key:'structure',label:'报告结构',options:[['conclusion_first','先给结论'],['bullets','分点列出']]},
];

function PreferencesView({preferences, preferencesError, loadPreferences, savePreferences, busy}) {
  const [draft,setDraft] = React.useState(preferences);
  const [saved,setSaved] = React.useState(false);
  React.useEffect(() => { setDraft(preferences); }, [preferences]);
  if (!draft) return preferencesError
    ? <Button onClick={loadPreferences}>重新加载研究偏好</Button>
    : <p role="status">正在加载研究偏好</p>;
  const changed = preferenceFields.some(({key}) => draft[key] !== preferences[key]);
  const hasPreferences = preferenceFields.some(({key}) => preferences[key]);
  async function save(value) {
    setSaved(await savePreferences(value));
  }
  return <div className="settings-page">
    <h1>研究偏好</h1>
    <p className="page-description">应用到今后创建的研究和追问。单次任务中的明确要求优先。</p>
    <Card className="settings-card" bordered={false}>
      <form onSubmit={event => { event.preventDefault(); if (changed && !busy) void save(draft); }}>
        {preferenceFields.map(({key,label,options}) => <label className="field-label" key={key}>
          {label}
          <select name={key} value={draft[key] || ''} disabled={busy} onChange={event => {
            setSaved(false);
            setDraft(current => ({...current,[key]:event.target.value}));
          }}>
            <option value="">系统默认</option>
            {options.map(([value,text]) => <option key={value} value={value}>{text}</option>)}
          </select>
        </label>)}
        <Space wrap>
          <Button type="primary" htmlType="submit" loading={busy} disabled={!changed}>保存偏好</Button>
          <Button disabled={busy || !hasPreferences} onClick={() => void save({})}>清除已保存偏好</Button>
          {saved && <span role="status">偏好已保存</span>}
        </Space>
      </form>
    </Card>
  </div>;
}

function FollowUpForm({taskID, busy, submitTask}) {
  const [question, setQuestion] = React.useState('');
  const formID = React.useId();
  return <form className="report-follow-up" onSubmit={event => {
    event.preventDefault();
    if (!busy && question.trim()) void submitTask(question, taskID);
  }}>
    <label htmlFor={formID}>基于这份报告追问</label>
    <Input.TextArea id={formID} value={question} onChange={event => setQuestion(event.target.value)}
      autoSize={{minRows:2,maxRows:5}} maxLength={2000} placeholder="还想核实哪些问题？"/>
    <Button type="primary" htmlType="submit" loading={busy} disabled={!question.trim()}>提交追问</Button>
  </form>;
}

function ReportCard({task, report, errors, downloadReport, busy, submitTask, readSource}) {
  const [source,setSource] = React.useState(null);
  const [showEvidence,setShowEvidence] = React.useState(false);
  const request = React.useRef(null);
  async function openSource(href) {
    const ticket = {};
    request.current = ticket;
    setSource({loading:true});
    try {
      const data = await readSource(href);
      if (request.current === ticket) setSource(data);
    } catch (error) {
      if (request.current === ticket) setSource({error:error.message});
    }
  }

  function citationLink({node,href,...props}) {
    const prefix = '/api/v1/research/tasks/' + task?.task_id + '/sources/';
    if (href?.startsWith(prefix) && /^[a-f0-9]{32}$/.test(href.slice(prefix.length))) {
      return <a {...props} href={href} onClick={event => {event.preventDefault(); void openSource(href);}}/>;
    }
    return <a {...props} href={href} target="_blank" rel="noopener noreferrer"/>;
  }
  const actions = report && <Space wrap>
    {task?.task_id && <Button icon={<BookOutlined/>} aria-expanded={showEvidence} aria-controls={showEvidence ? 'report-evidence' : undefined}
      onClick={() => setShowEvidence(current => !current)}>{showEvidence ? '收起证据' : '核查证据'}</Button>}
    <Button icon={<DownloadOutlined/>} onClick={downloadReport}>下载 Markdown</Button>
    <Button icon={<PrinterOutlined/>} onClick={() => window.print()}>导出 PDF</Button>
  </Space>;
  return <Card className="report-card" bordered={false} title={<span><FileTextOutlined/> 研究报告</span>} extra={actions}>
    {report
      ? <article className="report-content">
          <ReactMarkdown components={{a:citationLink}}>{report}</ReactMarkdown>
        </article>
      : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={task?.status==='FAILED'?'研究未完成：'+(errors[task.error_code]||task.error_code||'请查看运行记录'):'报告生成后会显示在这里。'}/>}
    {report && task?.task_id && showEvidence && <ReportEvidence taskID={task.task_id} read={readSource} openSource={openSource}/>}
    <Modal title={source?.title || '研究资料'} open={source !== null} footer={null}
      onCancel={() => {request.current = null;setSource(null);}}>
      {source?.loading ? <p role="status">正在读取资料</p>
        : source?.error ? <Alert type="error" showIcon message={source.error}/>
        : <pre className="source-text">{source?.excerpt}</pre>}
    </Modal>
    {report && task?.task_id && <>
      <ReportFeedback taskID={task.task_id} read={readSource}/>
      <FollowUpForm key={task.task_id} taskID={task.task_id} {...{busy, submitTask}}/>
    </>}
  </Card>;
}
const planTools = {
  fetch_page: ['读取网页', [['url','公开网页 URL']]],
  web_search: ['搜索资料', [['query','搜索词']]],
  github_repo: ['分析 GitHub 仓库', [['owner','仓库所有者'],['repo','仓库名称'],['path','仓库内目录（可选）',false]]],
  microsoft_docs_search: ['搜索 Microsoft Learn', [['query','公开技术主题',true,[
    ['aspnet-routing','ASP.NET Core Minimal API 路由'],
    ['dotnet-httpclient','.NET HttpClient 与连接池'],
    ['azure-app-service','Azure App Service 自定义容器'],
    ['wsl-containers','WSL 容器'],
  ]]]],
};

function PlanReview({rawPlan, sequence, busy, approvePlan}) {
  const [plan,setPlan] = React.useState(() => JSON.parse(rawPlan));
  function updateStep(index, key, value) {
    setPlan(current => ({...current,steps:current.steps.map((step, i) => i === index ? {...step,[key]:value} : step)}));
  }
  return <Card title="确认研究计划" className="plan-review">
    <form onSubmit={event => {
      event.preventDefault();
      if (!busy) void approvePlan(plan,sequence);
    }}>
      <p>可以调整研究问题和资料来源。确认后开始采集，后续补充取证由系统自动处理。</p>
      {plan.steps.map((step,index) => {
        const [name,fields] = planTools[step.tool];
        return <fieldset className="plan-step" key={step.step_id}>
          <legend>{index+1}. {name}</legend>
          <label>研究问题<Input value={step.question || ''} maxLength={2000}
            onChange={event => updateStep(index,'question',event.target.value)}/></label>
          {fields.map(([key,label,required=true,choices]) => <label key={key}>{label}
            {choices ? <select value={step[key]} onChange={event => updateStep(index,key,event.target.value)}>
              {choices.map(([value,text]) => <option key={value} value={value}>{text}</option>)}
            </select> : <Input value={step[key] || ''} required={required}
              onChange={event => updateStep(index,key,event.target.value)}/>}
          </label>)}
          {step.depends_on?.length > 0 && <p className="muted">依赖步骤 {step.depends_on.map(id => plan.steps.findIndex(item => item.step_id === id)+1).join('、')}</p>}
        </fieldset>;
      })}
      <Button type="primary" htmlType="submit" loading={busy}>确认计划并开始研究</Button>
    </form>
  </Card>;
}

function ResearchDetail({task,events,report,connection,errors,cancelTask,downloadReport,busy,submitTask,select,approvePlan,readSource}) {
  const percentage = task?.token_limit ? Math.min(100,Math.round(task.tokens_used/task.token_limit*100)) : 0;
  return <div className="detail-page"><div className="detail-top"><div><div className="page-kicker">RESEARCH DETAIL</div><h1>{task?.goal || '研究任务'}</h1></div><Tag className="status" color={statusColors[task?.status]||'default'} role="status">{labels[task?.status]||'正在加载'}</Tag></div><div className="detail-meta"><span><span className="online-dot"/> {connection||'研究过程与报告'}</span>{task && !terminal(task.status) && <Button disabled={task.status==='CANCELLING'} onClick={cancelTask}>取消研究</Button>}</div>
    {task?.creation_logid && <p className="task-log-id"><Text type="secondary">创建请求 Log ID </Text><Text code
      copyable={{text:task.creation_logid, tooltips:['复制 Log ID', '已复制']}}>{task.creation_logid}</Text></p>}
    {task?.parent_task_id && <div className="parent-report-link"><Button type="link" onClick={() => select(task.parent_task_id)}>查看上一份报告</Button></div>}
    <div className="result">
      {task?.status === 'AWAITING_APPROVAL' && task.review_plan
        ? <PlanReview key={task.task_id} rawPlan={task.review_plan} sequence={task.event_seq} {...{busy,approvePlan}}/>
        : <React.Suspense fallback={<Card className="report-card" bordered={false}><p role="status">正在加载报告…</p></Card>}>
        <ReportCard key={task?.task_id} {...{task, report, errors, downloadReport, busy, submitTask, readSource}}/>
      </React.Suspense>}
      <div className="detail-side"><Card className="progress-card" bordered={false} title="研究进度"><div className="usage-label"><span>Token 用量</span><span>{task?.tokens_used || 0} / {task?.token_limit || '—'}</span></div><Progress percent={percentage} showInfo={false} size="small"/><p className="muted">{connection||'任务状态会自动更新'}</p></Card><Card className="timeline-card" bordered={false} title="运行记录">{!events.length&&!task?.calls?.length ? <p className="muted">执行记录将在这里显示。</p> : <><ul className="call-list">{task?.calls?.map(call=><li key={call.tool_call_id}>{call.tool_call_id} · 第 {call.attempt_no} 次 · {call.status}</li>)}</ul><ol className="event-list">{events.map(item=><li key={item.seq}><span className="event-dot"/><span>{item.type==='tool_finished'?(item.payload.summary||eventLabels[item.type]):labels[item.payload.status]||eventLabels[item.type]||item.type}</span></li>)}</ol></>}</Card></div>
    </div></div>;
}
export function Workspace(props) {
  const {
    user, openAuth, authModal, sessionLoading,
    history, historyCursor, historyBusy, loadMoreHistory, selected, select, settings, setSettings, setModelInfo,
    logout, error, modelInfo, goal, setGoal, submit, submitTask, approvePlan, busy, saveModel, activateModel,
    probeModel, revokeModel, preferences, preferencesError, loadPreferences, savePreferences,
    task, events, report, connection, errors, cancelTask, downloadReport, readSource, readOverview,
  } = props;
  function openTask(id) {
    setSettings(false);
    select(id);
  }

  let content;
  let title = '工作台';
  switch (settings) {
    case 'schedules':
      content = <ResearchSchedules request={readSource} modelInfo={modelInfo} openTask={openTask}/>;
      break;
    case 'taskOverview':
      content = <TaskOverview read={readOverview} openTask={openTask} {...{labels,statusColors}}/>;
      break;
    case 'evaluation':
      title = '运行概览';
      content = <ResearchEvaluation read={readSource} openTask={openTask}/>;
      break;
    case 'usage':
      title = '运行概览';
      content = <UsageView read={readSource}/>;
      break;
    case 'preferences':
      title = '设置';
      content = <PreferencesView {...{preferences,preferencesError,loadPreferences,savePreferences,busy}}/>;
      break;
    case 'model':
      title = '设置';
      content = <SettingsView {...{modelInfo,saveModel,activateModel,probeModel,revokeModel,busy}}/>;
      break;
    default:
      if (selected) {
        title = '研究详情';
        content = <ResearchDetail {...{task,events,report,connection,errors,cancelTask,downloadReport,busy,submitTask,select,approvePlan,readSource}}/>;
      } else {
        content = <NewResearch {...{goal,setGoal,submit,busy,modelInfo,user,sessionLoading}}/>;
      }
  }

  return <Theme><div className="workspace">
    <Sidebar {...{history,historyCursor,historyBusy,loadMoreHistory,selected,select,settings,setSettings,setModelInfo,logout,user,openAuth}}/>
    <main className="main-area">
      <div className="topbar"><span>{title}</span><span className="topbar-right"><span className="online-dot"/> ResearchPilot</span></div>
      <div className="main-content"><ErrorNotice error={error}/>{content}</div>
    </main>
    {authModal}
  </div></Theme>;
}

import React from 'react';
const ReactMarkdown = React.lazy(() => import('react-markdown'));
import {Alert, Button, Card, ConfigProvider, Empty, Input, Progress, Space, Tag, Typography} from 'antd';
import {ArrowRightOutlined, BookOutlined, CheckCircleOutlined, CompassOutlined, DownloadOutlined, ExperimentOutlined, FileTextOutlined, HistoryOutlined, LockOutlined, LogoutOutlined, PlusOutlined, SearchOutlined, SettingOutlined, ThunderboltOutlined} from '@ant-design/icons';
import {terminal} from '../events.js';
import brandIcon from '../researchpilot-icon.png';

const {Text} = Typography;
const {TextArea} = Input;
const labels = {QUEUED:'等待派发',RUNNING:'执行中',RETRY_WAIT:'等待重试',CANCELLING:'取消中',CANCELLED:'已取消',PARTIAL:'部分完成',SUCCEEDED:'已完成',FAILED:'执行失败'};
const statusColors = {SUCCEEDED:'success',RUNNING:'processing',QUEUED:'processing',PARTIAL:'warning',FAILED:'error',CANCELLED:'default',RETRY_WAIT:'warning'};
const eventLabels = {task_status:'任务状态更新',step_started:'开始执行步骤',tool_started:'开始采集资料',tool_finished:'资料采集完成',usage_updated:'用量已更新',coverage_updated:'证据覆盖已更新',plan_updated:'研究计划已更新',replan_started:'调整研究计划',report_ready:'研究报告已生成'};

export function Theme({children}) {
  return <ConfigProvider button={{autoInsertSpace:false}} theme={{token:{colorPrimary:'#2354d8',borderRadius:12,fontFamily:'Inter, "Microsoft YaHei", sans-serif',colorText:'#182540',colorBgLayout:'#f5f7fc'}}}>{children}</ConfigProvider>;
}
function Brand() { return <div className="brand"><img className="brand-mark" src={brandIcon} width={38} height={38} alt="" /><span><strong>ResearchPilot</strong><small>你的技术研究助手</small></span></div>; }
function ErrorNotice({error}) {return error ? <Alert className="error-notice" type="error" showIcon message={error} role="alert"/> : null;}

export function AuthView({register, setRegister, authenticate, error, setError, busy}) {
  return <Theme><main className="auth-page"><div className="auth-shell">
    <div className="auth-intro"><Brand/><div><Tag color="blue">RESEARCH WORKSPACE</Tag><h1>把复杂问题，<br/>变成清晰答案。</h1><p>从一个问题开始，收集可靠资料，整理有据可查的研究报告。</p></div><div className="auth-points"><span><SearchOutlined /> 探索公开资料</span><span><BookOutlined /> 保留引用来源</span><span><ThunderboltOutlined /> 实时追踪进度</span></div></div>
    <div className="auth-card"><div className="mobile-brand"><Brand/></div><span className="eyebrow">WELCOME TO RESEARCHPILOT</span><h2>{register?'创建账户':'登录研究工作台'}</h2><p className="muted">{register?'创建账户后，即可开始你的第一项研究。':'欢迎回来，继续探索你的研究问题。'}</p>
      <ErrorNotice error={error}/><form onSubmit={authenticate} className="auth-form">
        <label>邮箱<Input size="large" type="email" name="email" required autoComplete="email" placeholder="name@example.com"/></label>
        <label>密码<Input.Password size="large" name="password" required minLength={register?10:undefined} autoComplete={register?'new-password':'current-password'} placeholder={register?'至少 10 字节':'输入密码'}/></label>
        <Button block size="large" type="primary" htmlType="submit" loading={busy}>{register?'注册并登录':'登录'}</Button>
        <Button block type="link" onClick={()=>{setRegister(!register);setError('');}}>{register?'返回登录':'创建账户'}</Button>
      </form><p className="auth-foot"><LockOutlined /> 你的研究记录保存在账户中</p>
    </div></div></main></Theme>;
}

function Sidebar({history, historyCursor, historyBusy, loadMoreHistory, selected, select, settings, setSettings, setModelInfo, logout}) {
  function newResearch(){setSettings(false);select('');}
  return <aside className="sidebar"><Brand/><Button type="primary" size="large" block icon={<PlusOutlined/>} onClick={newResearch}>新建研究</Button>
    <nav className="side-nav"><button className={!settings?'nav-item active':'nav-item'} onClick={newResearch}><CompassOutlined/> 研究工作台</button><button className={settings?'nav-item active':'nav-item'} onClick={()=>{if(!settings){setModelInfo(null);setSettings(true);}}}><SettingOutlined/> 模型设置</button></nav>
    <div className="side-heading"><span>研究历史</span><HistoryOutlined/></div><div className="history-list">{!history.length && <p className="side-empty">暂无研究记录</p>}{history.map(item=><button key={item.task_id} className="history" aria-current={!settings&&selected===item.task_id?'page':undefined} onClick={()=>{setSettings(false);select(item.task_id);}}><FileTextOutlined/><span><b>{item.goal}</b><small>{labels[item.status]}</small></span></button>)}{historyCursor && <Button type="link" block loading={historyBusy} onClick={loadMoreHistory}>加载更早的记录</Button>}</div>
    <div className="sidebar-footer"><span className="online-dot"/> 研究空间<Button type="text" icon={<LogoutOutlined/>} onClick={logout}>退出登录</Button></div>
  </aside>;
}
function TrialInfo({modelInfo}) {
  if (modelInfo?.configured) return <><CheckCircleOutlined/> 当前使用自己的模型配置</>;
  if (modelInfo?.trial_enabled) return <><ThunderboltOutlined/> 免费试用剩余 {modelInfo.trial_remaining} / {modelInfo.trial_limit} 次</>;
  return <><ExperimentOutlined/> 未配置模型时将运行演示任务</>;
}
function NewResearch({goal,setGoal,submit,busy,modelInfo}) {
  return <div className="new-research"><div className="page-kicker"><span className="kicker-dot"/> RESEARCH WORKSPACE</div><h1>今天想研究什么？</h1><p className="hero-description">告诉 ResearchPilot 你的问题，我们会帮你整理资料、追踪过程并生成有来源的报告。</p>
    <Card className="composer-card" bordered={false}><form onSubmit={submit}><div className="composer-title"><span className="feature-icon"><SearchOutlined/></span><div><strong>发起一项研究</strong><p>描述背景、目标和你希望了解的问题</p></div></div><label className="field-label" htmlFor="research-goal">研究目标</label><TextArea id="research-goal" value={goal} onChange={e=>setGoal(e.target.value)} maxLength={2000} rows={5} placeholder="例如：比较 Gin 和 Hertz 的 SSE 集成方式，并分析适用场景。" required showCount/><div className="composer-footer"><span className="model-hint"><TrialInfo modelInfo={modelInfo}/></span><Button type="primary" size="large" htmlType="submit" disabled={busy||!goal.trim()} loading={busy} icon={<ArrowRightOutlined/>} iconPosition="end">开始研究</Button></div></form></Card>
    <div className="tips-header"><strong>从这里开始</strong><span>让研究目标更清晰</span></div><div className="tip-grid"><div><span><CompassOutlined/></span><strong>明确研究方向</strong><p>描述背景和需要解决的问题。</p></div><div><span><BookOutlined/></span><strong>提供参考资料</strong><p>可以附上公开网页或 GitHub 链接。</p></div><div><span><FileTextOutlined/></span><strong>查看引用报告</strong><p>跟踪过程并核对报告中的来源。</p></div></div>
  </div>;
}
function SettingsView({modelInfo,saveModel,probeModel,revokeModel,busy}) {
  if (!modelInfo) return <p role="status">正在加载模型设置</p>;
  return <div className="settings-page"><div className="page-kicker">CONFIGURATION</div><h1>模型设置</h1><p className="page-description">配置你的模型服务，用于生成研究计划和报告。</p><Card className="settings-card" bordered={false}><form onSubmit={saveModel}>
    <div className="card-heading"><span className="feature-icon"><SettingOutlined/></span><div><strong>模型连接</strong><p>兼容 OpenAI 接口的模型供应商</p></div></div>
    {modelInfo.trial_enabled && <Alert className="settings-alert" type="info" showIcon message={`免费试用剩余 ${modelInfo.trial_remaining} / ${modelInfo.trial_limit} 次。使用自己的 API Key 不消耗试用次数。`}/>}
    {modelInfo.configured && <p>已配置 {modelInfo.model}，密钥尾号 {modelInfo.key_tail}</p>}
    <label className="field-label">Base URL<Input size="large" name="base_url" type="url" defaultValue={modelInfo.base_url || 'https://api.deepseek.com'} required/></label>
    <label className="field-label">模型名称<Input size="large" name="model" defaultValue={modelInfo.model || 'deepseek-flash'} required/></label>
    <label className="field-label">API Key<Input.Password size="large" name="api_key" autoComplete="off" required={!modelInfo.configured} placeholder={modelInfo.configured?'留空保留现有密钥，填写新值则替换':'输入你的 API Key'}/></label>
    <p className="muted"><LockOutlined/> 保存后加密存储，新任务使用该配置版本。</p><Space wrap><Button type="primary" htmlType="submit" loading={busy}>加密保存</Button>{modelInfo.configured && <><Button onClick={probeModel} disabled={busy}>检查连接与能力</Button><Button danger onClick={revokeModel} disabled={busy}>撤销全部已保存密钥版本</Button></>}</Space>{modelInfo.capabilities && <pre>{JSON.stringify(modelInfo.capabilities,null,2)}</pre>}
  </form></Card></div>;
}
function ResearchDetail({task,events,report,connection,errors,cancelTask,downloadReport}) {
  const percentage = task?.token_limit ? Math.min(100,Math.round(task.tokens_used/task.token_limit*100)) : 0;
  return <div className="detail-page"><div className="detail-top"><div><div className="page-kicker">RESEARCH DETAIL</div><h1>{task?.goal || '研究任务'}</h1></div><Tag className="status" color={statusColors[task?.status]||'default'} role="status">{labels[task?.status]||'正在加载'}</Tag></div><div className="detail-meta"><span><span className="online-dot"/> {connection||'研究过程与报告'}</span>{task && !terminal(task.status) && <Button disabled={task.status==='CANCELLING'} onClick={cancelTask}>取消研究</Button>}</div>
    <div className="result"><Card className="report-card" bordered={false} title={<span><FileTextOutlined/> 研究报告</span>} extra={report && <Button icon={<DownloadOutlined/>} onClick={downloadReport}>下载 Markdown</Button>}>
      {report ? <article className="report-content"><React.Suspense fallback={<p>正在加载报告…</p>}><ReactMarkdown components={{a:({node,...props})=><a {...props} target="_blank" rel="noopener noreferrer"/>}}>{report}</ReactMarkdown></React.Suspense></article> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={task?.status==='FAILED'?'研究未完成：'+(errors[task.error_code]||task.error_code||'请查看运行记录'):'报告生成后会显示在这里。'}/>}</Card>
      <div className="detail-side"><Card className="progress-card" bordered={false} title="研究进度"><div className="usage-label"><span>Token 用量</span><span>{task?.tokens_used || 0} / {task?.token_limit || '—'}</span></div><Progress percent={percentage} showInfo={false} size="small"/><p className="muted">{connection||'任务状态会自动更新'}</p></Card><Card className="timeline-card" bordered={false} title="运行记录">{!events.length&&!task?.calls?.length ? <p className="muted">执行记录将在这里显示。</p> : <><ul className="call-list">{task?.calls?.map(call=><li key={call.tool_call_id}>{call.tool_call_id} · 第 {call.attempt_no} 次 · {call.status}</li>)}</ul><ol className="event-list">{events.map(item=><li key={item.seq}><span className="event-dot"/><span>{item.type==='tool_finished'?(item.payload.summary||eventLabels[item.type]):labels[item.payload.status]||eventLabels[item.type]||item.type}</span></li>)}</ol></>}</Card></div>
    </div></div>;
}
export function Workspace(props) {
  const {history,historyCursor,historyBusy,loadMoreHistory,selected,select,settings,setSettings,setModelInfo,logout,error,modelInfo,goal,setGoal,submit,busy,saveModel,probeModel,revokeModel,task,events,report,connection,errors,cancelTask,downloadReport}=props;
  return <Theme><div className="workspace"><Sidebar {...{history,historyCursor,historyBusy,loadMoreHistory,selected,select,settings,setSettings,setModelInfo,logout}}/><main className="main-area"><div className="topbar"><span>{settings?'设置':selected?'研究详情':'工作台'}</span><span className="topbar-right"><span className="online-dot"/> ResearchPilot</span></div><div className="main-content"><ErrorNotice error={error}/>{settings ? <SettingsView {...{modelInfo,saveModel,probeModel,revokeModel,busy}}/> : !selected ? <NewResearch {...{goal,setGoal,submit,busy,modelInfo}}/> : <ResearchDetail {...{task,events,report,connection,errors,cancelTask,downloadReport}}/>}</div></main></div></Theme>;
}

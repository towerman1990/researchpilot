import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Checkbox, Empty, Input, Segmented, Select, Space, Tag} from 'antd';
import {ScheduleResults} from './ScheduleResults.jsx';
import './research-schedules.css';

const frequencies = [
  {value:0,label:'只运行一次'},
  {value:1,label:'每天'},
  {value:7,label:'每周'},
];
const statuses = {active:'已安排', completed:'已触发', cancelled:'已取消', blocked:'缺少个人模型'};

export function ResearchSchedules({request, modelInfo, openTask}) {
  const endpoint = '/api/v1/research/schedules';
  const [view, setView] = useState('plans');
  const [rows, setRows] = useState(null);
  const [revision, setRevision] = useState(0);
  const [goal, setGoal] = useState('');
  const [start, setStart] = useState('');
  const [repeat, setRepeat] = useState(0);
  const [notify, setNotify] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (view !== 'plans') return;
    let active = true;
    request(endpoint).then(
      result => { if (active) { setRows(result); setError(''); } },
      err => { if (active) setError(err.message); },
    );
    return () => { active = false; };
  }, [request, endpoint, revision, view]);

  async function update(path, body) {
    setError('');
    setBusy(true);
    try {
      await request(path, body);
      setRevision(value => value + 1);
      return true;
    } catch (err) {
      setError(err.message);
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function create(event) {
    event.preventDefault();
    const date = new Date(start);
    if (Number.isNaN(date.getTime()) || date <= new Date()) {
      setError('请选择未来的运行时间。');
      return;
    }
    if (await update(endpoint, {goal:goal.trim(), first_run:date.toISOString(), interval_days:repeat, notify_results:notify})) {
      setGoal('');
      setStart('');
    }
  }

  return <section className="settings-page research-schedules">
    <h1>定时研究</h1>
    <div><Segmented aria-label="定时研究页面" value={view}
      options={[{label:'研究计划', value:'plans'}, {label:'订阅结果', value:'results'}]} onChange={setView}/></div>
    {view === 'results' ? <ScheduleResults request={request} openTask={openTask}/> : <>
    {error && <Alert type="error" showIcon message={error}/>}
    {modelInfo && !modelInfo.configured && <Alert type="info" showIcon message="配置个人模型后即可安排定时研究。"/>}
    <Card title="安排研究" className="settings-card">
      <form onSubmit={create} className="schedule-form">
        <label>研究目标<Input.TextArea required rows={2} maxLength={2000} value={goal}
          onChange={event => setGoal(event.target.value)}/></label>
        <div className="schedule-fields">
          <label>首次运行（本地时间）<Input type="datetime-local" required value={start}
            onChange={event => setStart(event.target.value)}/></label>
          <label>重复频率<Select aria-label="重复频率" value={repeat} options={frequencies} onChange={setRepeat}/></label>
        </div>
        <Checkbox checked={notify} onChange={event => setNotify(event.target.checked)}>订阅站内运行结果</Checkbox>
        <Button type="primary" htmlType="submit" loading={busy} disabled={!modelInfo?.configured || !goal.trim() || !start}>保存计划</Button>
      </form>
    </Card>
    <Card className="settings-card" title="研究计划" extra={<Button disabled={busy} onClick={() => setRevision(value => value + 1)}>刷新</Button>}>
      {!rows ? !error && <p role="status">正在加载研究计划</p>
        : !rows.length ? <Empty description="暂无定时研究"/>
        : <ul className="schedule-list">{rows.map(row => <li key={row.schedule_id}>
          <Space wrap><Tag color={row.status === 'active' ? 'blue' : row.status === 'blocked' ? 'orange' : 'default'}>{statuses[row.status]}</Tag>
            <strong>{row.goal}</strong></Space>
          <p className="muted">{frequencies.find(item => item.value === row.interval_days)?.label}
            {row.next_run && <> · 下次运行 <time dateTime={row.next_run}>{new Date(row.next_run).toLocaleString()}</time></>}</p>
          <Space wrap>
            {row.status === 'active' && <Checkbox checked={row.notify_results} disabled={busy}
              onChange={event => void update(endpoint + '/' + row.schedule_id + '/subscription', {notify_results:event.target.checked})}>
              订阅结果
            </Checkbox>}
            {row.last_task_id && <Button type="link" onClick={() => openTask(row.last_task_id)}>查看上次研究</Button>}
            {row.status === 'active' && <Button type="text" danger disabled={busy}
              onClick={() => void update(endpoint + '/' + row.schedule_id + '/cancel', {})}>取消计划</Button>}
          </Space>
        </li>)}</ul>}
    </Card>
    </>}
  </section>;
}

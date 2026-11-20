import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Segmented, Skeleton, Space, Statistic} from 'antd';
import {ModelCostView} from './ModelCostView.jsx';
import './usage.css';

const format = new Intl.NumberFormat('zh-CN');

export function UsageView({read}) {
  const [days, setDays] = useState(7);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState(null);

  useEffect(() => {
    let active = true;
    read('/api/v1/research/usage?days=' + days).then(
      rows => { if (active) setResult({days, revision, rows}); },
      error => { if (active) setResult({days, revision, error}); },
    );
    return () => { active = false; };
  }, [days, revision, read]);

  const current = result?.days === days && result.revision === revision ? result : null;
  return <section className="settings-page usage-page">
    <h1>研究用量</h1>
    <p className="page-description">查看本账号每日已结算的模型调用与 Token 用量，日期按 UTC 统计。</p>
    <Space wrap className="usage-controls">
      <Segmented aria-label="统计范围" value={days} options={[{label:'最近 7 天', value:7}, {label:'最近 30 天', value:30}]}
        onChange={value => { setResult(null); setDays(value); }}/>
      <Button disabled={!current} onClick={() => setRevision(value => value + 1)}>刷新</Button>
    </Space>
    {!current ? <Card><Skeleton active paragraph={{rows:4}}/></Card>
      : current.error ? <Alert type="error" showIcon message="无法加载研究用量"
        description={current.error.status === 401 ? '登录已过期，请重新登录。' : '请刷新后重试。'}/>
      : <UsageResults rows={current.rows}/>}
    <ModelCostView read={read} days={days} revision={revision}/>
  </section>;
}

function UsageResults({rows}) {
  const totals = rows.reduce((sum, day) => ({
    actual: sum.actual + day.actual_tokens,
    estimated: sum.estimated + day.estimated_tokens,
    attempts: sum.attempts + day.attempts,
    failures: sum.failures + day.failed_attempts,
  }), {actual:0, estimated:0, attempts:0, failures:0});
  if (!totals.attempts) return <Card><Empty description="此期间暂无已结算的模型调用"/></Card>;
  const peak = Math.max(1, ...rows.map(day => day.actual_tokens + day.estimated_tokens));
  return <>
    <div className="usage-summary">
      <Card><Statistic title="实际 Token" value={totals.actual}/></Card>
      <Card><Statistic title="估算 Token" value={totals.estimated}/></Card>
      <Card><Statistic title="已结算调用" value={totals.attempts}/></Card>
      <Card><Statistic title="记录为失败的调用" value={totals.failures}/></Card>
    </div>
    <Card title="每日用量">
      <div className="usage-table">
        <table aria-label="每日模型用量">
          <thead><tr><th scope="col">日期（UTC）</th><th scope="col">实际 Token</th><th scope="col">估算 Token</th>
            <th scope="col">调用</th><th scope="col">失败</th></tr></thead>
          <tbody>{[...rows].reverse().map(day => <tr key={day.day}>
            <th scope="row"><time dateTime={day.day}>{day.day}</time>
              <div className="usage-bar" aria-hidden="true">
                <span style={{width:day.actual_tokens / peak * 100 + '%'}}/>
                <span style={{width:day.estimated_tokens / peak * 100 + '%'}}/>
              </div>
            </th>
            <td>{format.format(day.actual_tokens)}</td><td>{format.format(day.estimated_tokens)}</td>
            <td>{format.format(day.attempts)}</td><td>{format.format(day.failed_attempts)}</td>
          </tr>)}</tbody>
        </table>
      </div>
      <p className="usage-note">蓝色为供应商返回的 Token，紫色为系统估算。估算值用于预算核算，实际金额以供应商账单为准。</p>
    </Card>
  </>;
}

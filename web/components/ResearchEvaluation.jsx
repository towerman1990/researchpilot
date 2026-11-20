import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Segmented, Skeleton, Space, Tag} from 'antd';
import './research-evaluation.css';

const format = new Intl.NumberFormat('zh-CN');
const dateTime = new Intl.DateTimeFormat('zh-CN', {dateStyle:'short', timeStyle:'short'});
const outcomes = [
  {key:'succeeded', label:'完成', color:'#237b52'},
  {key:'partial', label:'部分完成', color:'#d0841e'},
  {key:'failed', label:'失败', color:'#c54753'},
  {key:'cancelled', label:'取消', color:'#8290a4'},
];

export function ResearchEvaluation({read, openTask}) {
  const [days, setDays] = useState(7);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState(null);

  useEffect(() => {
    let active = true;
    read('/api/v1/research/evaluation?days=' + days).then(
      data => { if (active) setResult({days, revision, data}); },
      error => { if (active) setResult({days, revision, error}); },
    );
    return () => { active = false; };
  }, [read, days, revision]);

  const current = result?.days === days && result.revision === revision ? result : null;
  return <section className="settings-page research-evaluation">
    <h1>研究评估</h1>
    <p className="page-description">按任务结束时间统计真实研究，日期以 UTC 为准。</p>
    <Space wrap className="evaluation-controls">
      <Segmented aria-label="统计范围" value={days} options={[{label:'最近 7 天', value:7}, {label:'最近 30 天', value:30}]}
        onChange={value => { setResult(null); setDays(value); }}/>
      <Button disabled={!current} onClick={() => setRevision(value => value + 1)}>刷新</Button>
    </Space>
    {!current ? <Card><Skeleton active paragraph={{rows:3}}/></Card>
      : current.error ? <Alert type="error" showIcon message="暂时无法读取研究评估" description="请刷新后重试。"/>
      : current.data.summary.finished_tasks === 0 ? <Card><Empty description="此期间暂无已结束的真实研究"/></Card>
      : <EvaluationResults {...current.data} openTask={openTask}/>}
  </section>;
}

function EvaluationResults({summary, review_queue:queue, openTask}) {
  return <>
    <div className="evaluation-metrics">
      <Card title="运行结果" extra={format.format(summary.finished_tasks) + ' 项'}>
        <div className="evaluation-outcome-bar" aria-hidden="true">
          {outcomes.map(item => <span key={item.key} style={{
            width: summary[item.key] / summary.finished_tasks * 100 + '%', backgroundColor:item.color,
          }}/>)}
        </div>
        <dl>{outcomes.map(item => <div key={item.key}>
          <dt><i aria-hidden="true" style={{backgroundColor:item.color}}/>{item.label}</dt>
          <dd>{format.format(summary[item.key])}</dd>
        </div>)}</dl>
      </Card>
      <Card title="报告与反馈">
        <dl>
          <div><dt>生成报告</dt><dd>{format.format(summary.reports)}</dd></div>
          <div><dt>含归档引用</dt><dd>{format.format(summary.cited_reports)}</dd></div>
          <div><dt>有帮助</dt><dd>{format.format(summary.helpful)}</dd></div>
          <div><dt>帮助不大</dt><dd>{format.format(summary.not_helpful)}</dd></div>
        </dl>
      </Card>
    </div>
    <Card title="最近可复查的研究">
      {queue.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无部分完成、失败或收到负面反馈的任务"/>
        : <ul className="evaluation-review-list">{queue.map(item => <li key={item.task_id}>
          <div>
            <strong>{item.goal}</strong>
            <Space wrap>
              {item.status === 'FAILED' && <Tag color="error">失败</Tag>}
              {item.status === 'PARTIAL' && <Tag color="warning">部分完成</Tag>}
              {item.rating === 'not_helpful' && <Tag>帮助不大</Tag>}
              <time dateTime={item.finished_at}>{dateTime.format(new Date(item.finished_at))}</time>
            </Space>
          </div>
          <Button type="link" aria-label={'查看任务 · ' + item.goal} onClick={() => openTask(item.task_id)}>查看任务</Button>
        </li>)}</ul>}
    </Card>
  </>;
}

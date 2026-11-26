import React, {useEffect, useMemo, useState} from 'react';
import {Alert, Button, Empty, Modal, Select, Space, Tag} from 'antd';
import {compareReports, readComparableReport} from '../reportComparison.js';

const unavailable = '该报告缺少可对比的结论和引用。';
const terminalReport = new Set(['SUCCEEDED','PARTIAL']);

function References({references}) {
  if (references.length === 0) return null;
  return <ul>
    {references.map(({key,number,title}) => <li key={key}>[{number}] {title}</li>)}
  </ul>;
}

function FindingList({title,findings}) {
  return <section>
    <h3>{title} <Tag>{findings.length}</Tag></h3>
    {findings.length > 0 && <ul>{findings.map(({text,references}) =>
      <li key={text}><p>{text}</p><References references={references}/></li>)}</ul>}
  </section>;
}

export function ReportComparison({taskID, report, history, historyCursor, historyBusy, loadMoreHistory, read, openTask}) {
  const [open,setOpen] = useState(false);
  const [baselineID,setBaselineID] = useState('');
  const [loaded,setLoaded] = useState(null);
  const baseline = loaded?.id === baselineID ? loaded.report : null;
  const error = loaded?.id === baselineID ? loaded.error : '';
  const current = useMemo(() => readComparableReport(report), [report]);
  const options = history.filter(item => item.task_id !== taskID && terminalReport.has(item.status))
    .map(item => ({
      value:item.task_id,
      label:item.created_at ? `${item.goal}（${new Date(item.created_at).toLocaleString()}）` : item.goal,
    }));

  useEffect(() => {
    if (!open || !baselineID) return;
    let active = true;
    read('/api/v1/research/tasks/' + baselineID + '/report').then(value => {
      if (!active) return;
      const parsed = value.task_id === baselineID && readComparableReport(value.markdown);
      setLoaded({id:baselineID,report:parsed,error:parsed ? '' : unavailable});
    }, e => {
      if (active) setLoaded({id:baselineID,report:null,error:e.message});
    });
    return () => { active = false; };
  }, [open,baselineID,read]);

  function choose(id) {
    setBaselineID(id || '');
    setLoaded(null);
  }

  if (!current) return null;
  const differences = baseline ? compareReports(baseline, current) : null;
  return <>
    <Button onClick={() => setOpen(true)}>对比报告</Button>
    <Modal open={open} onCancel={() => {setOpen(false); choose('');}} title="对比历史报告" footer={null} width={760}>
      <Space direction="vertical" size="middle" style={{width:'100%'}}>
        <label htmlFor="comparison-baseline">选择基准报告</label>
        <Select id="comparison-baseline" showSearch optionFilterProp="label" options={options}
          value={baselineID || undefined} onChange={choose} placeholder="选择已完成的历史研究" style={{width:'100%'}}/>
        {historyCursor && <Button type="link" loading={historyBusy} onClick={loadMoreHistory}>加载更早的报告</Button>}
        {options.length === 0 && !historyCursor && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无其他已完成的报告"/>}
        {baselineID && !baseline && !error && <p role="status">正在读取基准报告</p>}
        {error && <Alert type="error" message={error} showIcon/>}
        {differences && <>
          <p className="muted">按报告文字对比。仅改写措辞也可能显示为新增或移除；引用列表相同的网页仍可能更新。</p>
          {(baseline.partial || current.partial) && <Alert type="info" showIcon message="包含部分研究成果，未覆盖的内容可能造成差异。"/>}
          <Button onClick={() => openTask(baselineID)}>打开基准报告核对原文</Button>
          <FindingList title="当前报告新增" findings={differences.added}/>
          <FindingList title="仅在基准报告中出现" findings={differences.removed}/>
          <section>
            <h3>正文相同、引用列表有变化 <Tag>{differences.referencesChanged.length}</Tag></h3>
            {differences.referencesChanged.map(({previous,current: finding}) => <div key={finding.text}>
              <p>{finding.text}</p>
              <b>基准报告引用</b><References references={previous.references}/>
              <b>当前报告引用</b><References references={finding.references}/>
            </div>)}
          </section>
          <p>共有 {differences.unchanged} 条结论的正文和引用列表相同。</p>
        </>}
      </Space>
    </Modal>
  </>;
}

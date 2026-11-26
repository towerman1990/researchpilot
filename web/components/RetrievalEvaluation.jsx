import React, {useState} from 'react';
import {Alert, Card, Empty, Segmented, Space, Table, Tag} from 'antd';
import sample from '../retrievalEvaluation.sample.json';
import {parseRetrievalEvaluation, retrievalOutcome} from '../retrievalEvaluation.js';

const modes = new Map([['keyword','关键词'],['vector','纯向量'],['hybrid','混合']]);
const outcomes = {
  covered:['命中全部标注','success'],
  missed:['缺少标注片段','warning'],
  false_positive:['无答案误命中','error'],
  no_answer:['无答案且无候选','default'],
};
const percent = new Intl.NumberFormat('zh-CN',{style:'percent',maximumFractionDigits:1});
const ms = new Intl.NumberFormat('zh-CN',{maximumFractionDigits:2});

const summaryColumns = [
  {title:'策略',dataIndex:'mode',render:value => modes.get(value) || value},
  ...[1,3,5].map(k => ({title:'Recall@'+k,dataIndex:'recall_at_'+k,render:value => percent.format(value)})),
  {title:'MRR@5', dataIndex:'mrr_at_5',render:value => percent.format(value)},
  {title:'P95（ms）',dataIndex:'p95_ms',render:value => ms.format(value)},
  {title:'无答案误命中',dataIndex:'uncovered_with_results'},
];

export function RetrievalEvaluation() {
  const [report,setReport] = useState(sample);
  const [mode,setMode] = useState(sample.strategies[0].mode);
  const [filter,setFilter] = useState('all');
  const [error,setError] = useState('');
  const [fileName,setFileName] = useState('');
  const strategy = report.strategies.find(item => item.mode === mode);

  async function load(event) {
    const file = event.target.files[0];
    event.target.value = '';
    if (!file) return;
    setError('');
    try {
      if (file.size > 1024 * 1024) throw Error('请选择不超过 1 MiB 的评估 JSON。');
      const loaded = parseRetrievalEvaluation(JSON.parse(await file.text()));
      setMode(loaded.strategies[0].mode);
      setReport(loaded);
      setFileName(file.name);
    } catch (e) {
      setError(e instanceof SyntaxError ? '文件不是有效的 JSON。' : e.message);
    }
  }

  const rows = strategy.cases.filter(row => filter === 'all' || retrievalOutcome(row) === filter);
  return <Card title="检索评估">
    <Space direction="vertical" size="middle" style={{width:'100%'}}>
      <p>{fileName || '内置样例 · 真实多语言嵌入本地实验'} · 语料 {report.corpus} · 题集 {report.query_set}</p>
      {typeof report.embedding_model === 'string' && <p className="muted">嵌入模型 {report.embedding_model}</p>}
      <label htmlFor="retrieval-evaluation-file">导入评估 JSON</label>
      <input id="retrieval-evaluation-file" type="file" accept=".json,application/json" onChange={load}/>
      {error && <Alert type="error" showIcon message={error}/>}
      <Table size="small" rowKey="mode" dataSource={report.strategies} columns={summaryColumns} pagination={false} scroll={{x:true}}/>
      <Space wrap>
        <Segmented aria-label="检索策略" value={mode} onChange={setMode}
          options={report.strategies.map(item => ({value:item.mode,label:modes.get(item.mode) || item.mode}))}/>
        <Segmented aria-label="题目筛选" value={filter} onChange={setFilter}
          options={[{label:'全部',value:'all'},{label:'漏召回',value:'missed'},{label:'无答案误命中',value:'false_positive'}]}/>
      </Space>
      {!rows.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="此筛选条件下没有题目"/> :
        <ul className="evaluation-review-list">
          {rows.map(row => {
            const [label,color] = outcomes[retrievalOutcome(row)];
            return <li key={row.id}><div>
              <strong>{row.query}</strong>
              <p>标注片段 {row.relevant_ids.join('、') || '无'}</p>
              <p>实际排序 {row.result_ids.join('、') || '无'}</p>
            </div><Tag color={color}>{label}</Tag></li>;
          })}
        </ul>}
    </Space>
  </Card>;
}

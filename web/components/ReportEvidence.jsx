import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Skeleton, Tag} from 'antd';
import './report-evidence.css';

const reportKinds = {research:'完整研究', partial:'部分报告', demo:'演示报告'};

export function ReportEvidence({taskID, read, openSource}) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(false);
  const [retry, setRetry] = useState(0);

  useEffect(() => {
    let active = true;
    read('/api/v1/research/tasks/' + taskID + '/report/evidence').then(
      evidence => { if (active) setResult(evidence); },
      () => { if (active) setError(true); },
    );
    return () => { active = false; };
  }, [taskID, read, retry]);

  return <Card className="report-evidence" id="report-evidence" size="small" title="证据核查">
    {error ? <Alert type="error" showIcon message="暂时无法读取引用"
      action={<Button size="small" onClick={() => { setError(false); setRetry(value => value + 1); }}>重试</Button>}/>
      : !result ? <Skeleton active paragraph={{rows:2}}/>
      : <>
        <dl className="report-evidence-stats">
          <div><dt>报告类型</dt><dd>{reportKinds[result.kind] || result.kind}</dd></div>
          <div><dt>归档来源</dt><dd>{result.archived_sources}</dd></div>
          <div><dt>报告引用</dt><dd>{result.citations.length}</dd></div>
        </dl>
        {result.citations.length ? <ol className="report-evidence-list">
          {result.citations.map(({source_id, title, source_type, ordinal}) => (
            <li key={ordinal} value={ordinal}>
              <Button type="link" className="report-evidence-source" onClick={() => {
                void openSource('/api/v1/research/tasks/' + taskID + '/sources/' + source_id);
              }}>{title}</Button>
              <Tag>{({upload:'上传资料', knowledge:'私有知识库'})[source_type] || '公开来源'}</Tag>
            </li>
          ))}
        </ol> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这份报告没有已归档的引用"/>}
      </>}
  </Card>;
}

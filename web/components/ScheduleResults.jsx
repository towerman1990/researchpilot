import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Space, Tag} from 'antd';

const endpoint = '/api/v1/research/schedule-results';
const statuses = {SUCCEEDED:'已完成', PARTIAL:'部分完成', FAILED:'执行失败', CANCELLED:'已取消'};

export function ScheduleResults({request, openTask}) {
  const [rows, setRows] = useState(null);
  const [revision, setRevision] = useState(0);
  const [loadError, setLoadError] = useState('');
  const [writeError, setWriteError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let active = true;
    let timer;
    async function refresh() {
      try {
        if (!document.hidden) {
          const data = await request(endpoint);
          if (active) {
            setRows(data);
            setLoadError('');
          }
        }
      } catch (err) {
        if (active) setLoadError(err.message);
      } finally {
        if (active) timer = setTimeout(refresh, 10000);
      }
    }
    void refresh();
    return () => { active = false; clearTimeout(timer); };
  }, [request, revision]);

  async function markRead(taskID) {
    setSaving(true);
    setWriteError('');
    try {
      await request(endpoint + '/' + taskID + '/read', {});
      setRows(current => current.map(row => row.task_id === taskID ? {...row, unread:false} : row));
      setRevision(value => value + 1);
    } catch (err) {
      setWriteError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return <Card title="订阅结果" extra={<Button onClick={() => setRevision(value => value + 1)}>刷新</Button>}>
    {(loadError || writeError) && <Alert type="error" showIcon message={writeError || loadError}/>}
    {!rows ? !loadError && <p role="status">正在读取订阅结果</p>
      : !rows.length ? <Empty description="已订阅的研究任务结束后，结果会显示在这里"/>
      : <ul className="schedule-list">{rows.map(row => <li key={row.task_id}>
        <Space wrap>
          {row.unread && <Tag color="blue">未读</Tag>}
          <Tag color={row.status === 'SUCCEEDED' ? 'success' : row.status === 'FAILED' ? 'error' : 'default'}>
            {statuses[row.status]}
          </Tag>
          <strong>{row.goal}</strong>
        </Space>
        <p className="muted"><time dateTime={row.finished_at}>{new Date(row.finished_at).toLocaleString()}</time></p>
        <Space wrap>
          <Button type="link" onClick={() => openTask(row.task_id)}>查看任务</Button>
          {row.unread && <Button type="text" disabled={saving} onClick={() => void markRead(row.task_id)}>标为已读</Button>}
        </Space>
      </li>)}</ul>}
  </Card>;
}

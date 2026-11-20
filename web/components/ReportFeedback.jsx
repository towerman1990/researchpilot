import React, {useEffect, useState} from 'react';
import {Alert, Button, Input, Radio, Space} from 'antd';
import './report-feedback.css';

export function ReportFeedback({taskID, read}) {
  const [opened, setOpened] = useState(false);
  return <details className="report-feedback" onToggle={event => {
    if (event.currentTarget.open) setOpened(true);
  }}>
    <summary>报告反馈</summary>
    {opened && <FeedbackForm taskID={taskID} read={read}/>}
  </details>;
}

function FeedbackForm({taskID, read}) {
  const endpoint = '/api/v1/research/tasks/' + taskID + '/report/feedback';
  const [draft, setDraft] = useState(null);
  const [saved, setSaved] = useState(null);
  const [retry, setRetry] = useState(0);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let active = true;
    read(endpoint).then(value => {
      if (active) { setDraft(value); setSaved(value); }
    }, () => { if (active) setError('暂时无法读取报告反馈。'); });
    return () => { active = false; };
  }, [endpoint, read, retry]);

  async function submit(event) {
    event.preventDefault();
    setSaving(true);
    setError('');
    try {
      const response = await fetch(endpoint, {
        method:'PUT',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify(draft),
      });
      if (!response.ok) throw new Error('feedback_save_failed');
      const feedback = await response.json();
      setSaved(feedback);
      setDraft(feedback);
    } catch {
      setError('暂时无法保存反馈，请重试。');
    } finally {
      setSaving(false);
    }
  }

  if (!draft) {
    return error ? <Alert showIcon type="error" message={error}
      action={<Button size="small" onClick={() => { setError(''); setRetry(value => value + 1); }}>重试</Button>}/>
      : <p role="status">正在读取报告反馈</p>;
  }

  const length = [...draft.comment.trim()].length;
  const changed = draft.rating !== saved.rating || draft.comment.trim() !== saved.comment;
  return <form onSubmit={submit}>
    <fieldset disabled={saving}>
      <legend>这份报告对你有帮助吗？</legend>
      <Radio.Group name="report-rating" value={draft.rating} onChange={event => {
        setDraft({...draft, rating:event.target.value});
      }}>
        <Radio value="helpful">有帮助</Radio>
        <Radio value="not_helpful">帮助不大</Radio>
      </Radio.Group>
      <label>备注（可选）
        <Input.TextArea rows={2} value={draft.comment} maxLength={1000}
          status={length > 500 ? 'error' : undefined}
          onChange={event => setDraft({...draft, comment:event.target.value})}/>
      </label>
      <p className="report-feedback-count">{length} / 500 字</p>
      {error && <Alert showIcon type="error" message={error}/>}
      <Space wrap>
        <Button type="primary" htmlType="submit" loading={saving}
          disabled={!draft.rating || !changed || length > 500}>保存反馈</Button>
        {!changed && saved.rating && <span role="status">反馈已保存</span>}
      </Space>
    </fieldset>
  </form>;
}

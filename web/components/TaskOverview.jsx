import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Skeleton, Tag} from 'antd';
import {ArrowRightOutlined, ReloadOutlined} from '@ant-design/icons';
import './task-overview.css';

const refreshInterval = 5000;
const dateTime = new Intl.DateTimeFormat('zh-CN', {dateStyle:'short', timeStyle:'short'});
const phases = {planning:'规划', executing:'收集资料', observing:'检查证据', replanning:'调整计划', synthesizing:'整理报告'};

export function TaskOverview({read, openTask, labels, statusColors}) {
  const [overview, setOverview] = useState(null);
  const [error, setError] = useState(null);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    let timer;
    let loading = false;
    let expired = false;

    async function refresh() {
      if (document.hidden || loading || expired) return;
      loading = true;
      try {
        const result = await read(controller.signal);
        if (!controller.signal.aborted) {
          setOverview(result);
          setError(null);
        }
      } catch (err) {
        if (!controller.signal.aborted) {
          expired = err.status === 401;
          if (expired) setOverview(null);
          setError(err);
        }
      } finally {
        loading = false;
        if (!controller.signal.aborted && !document.hidden && !expired) {
          timer = setTimeout(refresh, refreshInterval);
        }
      }
    }

    function visibilityChanged() {
      clearTimeout(timer);
      if (!document.hidden) void refresh();
    }

    document.addEventListener('visibilitychange', visibilityChanged);
    void refresh();
    return () => {
      document.removeEventListener('visibilitychange', visibilityChanged);
      clearTimeout(timer);
      controller.abort();
    };
  }, [read, revision]);

  const approval = overview?.open.filter(task => task.status === 'AWAITING_APPROVAL');
  const running = overview?.open.filter(task => task.status !== 'AWAITING_APPROVAL');
  const groupProps = {openTask, labels, statusColors};
  return <section className="task-overview">
    <header className="task-overview-heading">
      <h1>多任务工作区</h1>
      <Button icon={<ReloadOutlined/>} onClick={() => setRevision(value => value + 1)}>刷新</Button>
    </header>
    {error && <Alert type="error" showIcon message={error.status === 401
      ? '登录已过期，请重新登录。'
      : '暂时无法更新任务，可手动刷新重试。'}/>}
    {!overview && !error && <Card><Skeleton active/></Card>}
    {overview && <>
      {overview.more_open && <Alert type="info" showIcon message="这里显示最新的 50 个未结束任务。更早的任务可在研究历史中查看。"/>}
      <div className="task-overview-grid">
        <TaskGroup {...groupProps} title="进行中" empty="暂无运行或排队任务" tasks={running}/>
        <TaskGroup {...groupProps} title="待确认计划" empty="暂无待确认计划" tasks={approval}/>
        <TaskGroup {...groupProps} title="最近结束" empty="暂无已结束的任务" tasks={overview.finished}/>
      </div>
    </>}
  </section>;
}

function TaskGroup({title, tasks, empty, openTask, labels, statusColors}) {
  return <Card title={title} extra={tasks.length || null} className="task-overview-group">
    {tasks.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={empty}/>
      : <ul>{tasks.map(task => {
        const timestamp = task.finished_at || task.created_at;
        const action = task.status === 'AWAITING_APPROVAL' ? '审阅计划' : '查看详情';
        return <li key={task.task_id}>
          <div className="task-overview-status">
            <Tag color={statusColors[task.status]}>{labels[task.status]}</Tag>
            {task.phase && phases[task.phase] && <span>{phases[task.phase]}</span>}
          </div>
          <p>{task.goal}</p>
          <div className="task-overview-meta">
            <time dateTime={timestamp}>{dateTime.format(new Date(timestamp))}</time>
            <Button type="link" icon={<ArrowRightOutlined/>} iconPlacement="end"
              aria-label={action + ' · ' + task.goal} onClick={() => openTask(task.task_id)}>{action}</Button>
          </div>
        </li>;
      })}</ul>}
  </Card>;
}

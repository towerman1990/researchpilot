import React, {useEffect, useRef, useState} from 'react';
import {Alert, Button, Card, Input, Space} from 'antd';
import {terminal} from '../events.js';

function mergeTurns(previous, incoming) {
  const rows=new Map(previous.map(row=>[row.task_id,row]));
  for(const row of incoming) rows.set(row.task_id,row);
  return [...rows.values()].sort((a,b)=>a.conversation_turn-b.conversation_turn);
}

function ArchivedTurn({turn,read,renderReport,select}) {
  const [result,setResult]=useState(null);
  const [retry,setRetry]=useState(0);
  useEffect(()=>{
    let active=true;
    if(!['SUCCEEDED','PARTIAL'].includes(turn.status)) return;
    read('/api/v1/research/tasks/'+turn.task_id+'/report').then(value=>{
      if(active && value.task_id===turn.task_id) setResult({markdown:value.markdown});
    },error=>{if(active)setResult({error:error.message});});
    return ()=>{active=false;};
  },[turn.task_id,turn.status,read,retry]);
  return <section className="conversation-turn" aria-label={'研究问题：'+turn.goal}>
    <h2>{turn.goal}</h2>
    {result?.markdown ? renderReport(turn,result.markdown) : result?.error
      ? <Alert type="error" message={result.error} action={<Button onClick={()=>{setResult(null);setRetry(value=>value+1);}}>重试</Button>}/>
      : ['SUCCEEDED','PARTIAL'].includes(turn.status) ? <p role="status">正在读取报告</p>
      : <Card><p>{turn.status==='FAILED'?'这轮研究执行失败。':turn.status==='CANCELLED'?'这轮研究已取消。':'这轮研究正在执行。'}</p>
          <Button type="link" onClick={()=>select(turn.task_id)}>查看运行记录</Button></Card>}
  </section>;
}

export function ConversationView({conversationID,read,currentTask,active,busy,submitTask,select,renderReport}) {
  const [data,setData]=useState(null);
  const [error,setError]=useState('');
  const [question,setQuestion]=useState('');
  const [loadingOlder,setLoadingOlder]=useState(false);
  const [retry,setRetry]=useState(0);
  const latest=useRef(null);
  useEffect(()=>{
    let live=true;
    let pending=false;
    async function refresh(force = false) {
      if(pending || (!force && document.visibilityState==='hidden')) return;
      pending=true;
      try {
        const result=await read('/api/v1/research/conversations/'+conversationID);
        if(!live) return;
        if(result.conversation?.conversation_id!==conversationID) throw Error('会话数据不匹配。');
        setError('');
        setData(previous=>{
          const next={...result,tasks:mergeTurns(previous?.tasks||[],result.tasks),
            next_before:previous ? previous.next_before : result.next_before};
          latest.current=next;
          return next;
        });
      } catch(e) {if(live)setError(e.message);}
      finally {pending=false;}
    }
    void refresh(true);
    const timer=setInterval(()=>void refresh(),15000);
    const onVisible=()=>{if(document.visibilityState==='visible')void refresh(true);};
    document.addEventListener('visibilitychange',onVisible);
    return ()=>{
      live=false;
      clearInterval(timer);
      document.removeEventListener('visibilitychange',onVisible);
    };
  },[conversationID,read,currentTask.task_id,currentTask.status,retry]);

  async function loadOlder() {
    if(!latest.current?.next_before || loadingOlder) return;
    setLoadingOlder(true);
    try{
      const page=await read('/api/v1/research/conversations/'+conversationID+'?before='+encodeURIComponent(latest.current.next_before));
      if(page.conversation?.conversation_id!==conversationID) throw Error('会话数据不匹配。');
      setData(previous=>{
        const next={...previous,tasks:mergeTurns(page.tasks,previous.tasks),next_before:page.next_before};
        latest.current=next;
        return next;
      });
      setError('');
    }catch(e){setError(e.message);}
    finally{setLoadingOlder(false);}
  }
  const rows=data ? mergeTurns(data.tasks,[currentTask]) : [currentTask];
  const lastID=data?.conversation.last_task_id;
  const last=rows.find(row=>row.task_id===lastID);
  const canContinue=Boolean(last && terminal(last.status) && terminal(currentTask.status));
  async function send(event) {
    event.preventDefault();
    if(!canContinue || busy || !question.trim()) return;
    const submitted=question;
    const created=await submitTask(submitted,'',false,[],false,false,'',false,conversationID);
    if(created) setQuestion(value=>value===submitted?'':value);
  }
  return <div className="conversation-page">
    {error && <Alert type="error" showIcon message={error} action={<Button onClick={()=>setRetry(value=>value+1)}>重新加载</Button>}/>}
    {data?.next_before && <Button loading={loadingOlder} onClick={loadOlder}>加载更早的对话</Button>}
    {rows.map(turn=>turn.task_id===currentTask.task_id
      ? <section className="conversation-turn" key={turn.task_id}>{active}</section>
      : <ArchivedTurn key={turn.task_id} {...{turn,read,renderReport,select}}/>)}
    {lastID && lastID!==currentTask.task_id && <Button onClick={()=>select(lastID)}>查看最新一轮运行记录</Button>}
    <Card className="conversation-composer">
      <form onSubmit={send}>
        <label className="field-label" htmlFor="conversation-question">继续这个会话</label>
        <Input.TextArea id="conversation-question" value={question} maxLength={2000}
          autoSize={{minRows:2,maxRows:6}} onChange={event=>setQuestion(event.target.value)}
          placeholder="继续提问，或补充条件和想核实的内容"/>
        <Space className="conversation-actions">
          {!canContinue && <span className="muted">{data?'当前研究结束后可以继续提问。':'正在加载会话。'}</span>}
          <Button type="primary" htmlType="submit" loading={busy} disabled={!canContinue || busy || !question.trim()}>发送</Button>
        </Space>
      </form>
    </Card>
  </div>;
}

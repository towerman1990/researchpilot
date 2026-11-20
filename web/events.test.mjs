import {test} from 'node:test';
import assert from 'node:assert/strict';
import {applyTaskEvent,mergeEvent,mergeTaskSnapshot,terminal} from './events.js';

test('a successful tool attempt cannot complete the research task',()=>{
  const state={status:'RUNNING',event_seq:'9007199254740993'};
  const event={seq:'9007199254740994',type:'tool_finished',payload:{status:'SUCCEEDED'}};
  assert.equal(applyTaskEvent(state,event).status,'RUNNING');
  assert.equal(applyTaskEvent(state,event).event_seq,event.seq);
  assert.equal(applyTaskEvent(state,{...event,seq:'9007199254740992'}),state);
});
test('replayed and out of order events retain one entry per seq',()=>{
  const first={seq:'10',payload:{}},second={seq:'11',payload:{}};
  assert.deepEqual(mergeEvent(mergeEvent([second],first),first),[first,second]);
  assert(terminal('CANCELLED'));
});

test('snapshot calls survive replay and new attempts replace old ones', () => {
  const current={status:'RUNNING',phase:'executing',event_seq:'10',calls:[{tool_call_id:'tool-1',attempt_no:1,status:'STARTED'}]};
  assert.equal(applyTaskEvent(current,{seq:'9',type:'tool_finished',payload:{tool_call_id:'tool-1',attempt_no:1,status:'SUCCEEDED'}}),current);
  const next=applyTaskEvent(current,{seq:'11',type:'step_started',payload:{tool_call_id:'tool-1',attempt_no:2}});
  assert.equal(next.calls.length,1);
  assert.equal(next.calls[0].attempt_no,2);
  const late=applyTaskEvent(next,{seq:'12',type:'tool_finished',payload:{tool_call_id:'tool-1',attempt_no:1,status:'SUCCEEDED'}});
  assert.equal(late.calls[0].status,'STARTED');
  assert.equal(late.status,'RUNNING');
});
test('partial report is terminal and clears active phase', () => {
  const next=applyTaskEvent({status:'RUNNING',phase:'synthesizing',event_seq:'1'},{seq:'2',type:'report_ready',payload:{status:'PARTIAL'}});
  assert.equal(next.status,'PARTIAL');
  assert.equal(next.phase,null);
});
test('a delayed snapshot can only update its selected task', () => {
  const current = {task_id:'B', event_seq:'3'};
  const delayed = {task_id:'A', event_seq:'13'};
  assert.equal(mergeTaskSnapshot(current, delayed, 'B'), current);
  assert.equal(mergeTaskSnapshot(null, delayed, 'B'), null);
  assert.equal(mergeTaskSnapshot(current, {...current,event_seq:'2'}, 'B'), current);
  const next = {...current,event_seq:'4'};
  assert.equal(mergeTaskSnapshot(current, next, 'B'), next);
});

test('a task can pause for plan approval and return to the queue', () => {
  const running = {status:'RUNNING',phase:'planning',event_seq:'8'};
  const waiting = applyTaskEvent(running,{seq:'9',type:'task_status',payload:{status:'AWAITING_APPROVAL'}});
  assert.equal(waiting.status,'AWAITING_APPROVAL');
  assert.equal(waiting.phase,null);
  assert.equal(terminal(waiting.status),false);
  const queued = applyTaskEvent(waiting,{seq:'10',type:'task_status',payload:{status:'QUEUED',reason:'plan_approved'}});
  assert.equal(queued.status,'QUEUED');
});

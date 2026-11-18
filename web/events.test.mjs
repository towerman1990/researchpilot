import {test} from 'node:test';
import assert from 'node:assert/strict';
import {applyTaskEvent,mergeEvent,terminal} from './events.js';

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
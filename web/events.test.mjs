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

import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import {parseRetrievalEvaluation, retrievalOutcome} from './retrievalEvaluation.js';

const fixture = JSON.parse(readFileSync(new URL('./retrievalEvaluation.sample.json', import.meta.url), 'utf8'));

test('accepts the bundled keyword, vector and hybrid benchmark', () => {
  const report = parseRetrievalEvaluation(fixture);
  assert.deepEqual(report.strategies.map(item => item.mode).sort(), ['hybrid', 'keyword', 'vector']);
});

test('rejects duplicate strategies and malformed cases', () => {
  const duplicate = structuredClone(fixture);
  duplicate.strategies[1].mode = duplicate.strategies[0].mode;
  assert.throws(() => parseRetrievalEvaluation(duplicate), /指标/);
  const malformed = structuredClone(fixture);
  malformed.strategies[0].cases[0].result_ids = [null];
  assert.throws(() => parseRetrievalEvaluation(malformed), /明细/);
});

test('classifies incomplete retrieval and unanswered queries', () => {
  assert.equal(retrievalOutcome({relevant_ids:['a','b'], result_ids:['a','b']}), 'covered');
  assert.equal(retrievalOutcome({relevant_ids:['a','b'], result_ids:['a']}), 'missed');
  assert.equal(retrievalOutcome({relevant_ids:[], result_ids:['a']}), 'false_positive');
  assert.equal(retrievalOutcome({relevant_ids:[], result_ids:[]}), 'no_answer');
});

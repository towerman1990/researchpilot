import assert from 'node:assert/strict';
import test from 'node:test';
import {compareReports, readComparableReport} from './reportComparison.js';

const report = (findings, refs, title = '研究报告') =>
  '# ' + title + '\n\n' + findings.join('\n\n') + '\n\n## 局限\n\n- 资料范围\n\n## 参考资料\n\n' + refs.join('\n');

test('compares findings independently of whitespace, citation numbering, and order', () => {
  const old = readComparableReport(report([
    '- 相同的结论 [1] [2]',
    '仅在基准报告 [2]',
    '引用变动 [1]',
  ], ['1. [规格](<https://example.test/spec>)','2. [手册](<https://example.test/manual>)']));
  const current = readComparableReport(report([
    '当前新增 [1]',
    '引用变动 [1]',
    '相同的\n结论 [1] [2]',
  ], ['1. [手册](<https://example.test/manual>)','2. [规格](<https://example.test/spec>)']));
  const diff = compareReports(old, current);
  assert.deepEqual(diff.added.map(item => item.text), ['当前新增']);
  assert.deepEqual(diff.removed.map(item => item.text), ['仅在基准报告']);
  assert.equal(diff.unchanged, 1);
  assert.equal(diff.referencesChanged.length, 1);
  assert.deepEqual(diff.referencesChanged[0].previous.references.map(item => item.title), ['规格']);
  assert.deepEqual(diff.referencesChanged[0].current.references.map(item => item.title), ['手册']);
});

test('uses archived version titles when comparing task-local source links', () => {
  const archived = (task, title) => '1. [' + title + '](</api/v1/research/tasks/' + task.repeat(32) + '/sources/' + 'c'.repeat(32) + '>)';
  const baseline = readComparableReport(report(['检查 A 组 [1]'], [archived('a','手册 · 第 1 版 · 片段 1')]));
  const same = readComparableReport(report(['检查 A\n组 [1]'], [archived('b','手册 · 第 1 版 · 片段 1')]));
  const changed = readComparableReport(report(['检查 A 组 [1]'], [archived('b','手册 · 第 2 版 · 片段 1')], '部分研究成果'));
  assert.equal(compareReports(baseline, same).unchanged, 1);
  assert.equal(compareReports(baseline, changed).referencesChanged.length, 1);
  assert.equal(changed.partial, true);
});

test('findings may include multiple paragraphs and escaped citation-like text', () => {
  const parsed = readComparableReport(report(['使用状态 \\[42\\]。\n\n继续检查 &lt;timeout&gt;。 [1]'], ['1. [手册](<https://example.test/manual>)']));
  const [finding] = parsed.findings.values();
  assert.equal(finding.text, '使用状态 [42]。\n\n继续检查 <timeout>。');
  assert.equal(finding.references.length, 1);
  assert.equal(readComparableReport('# M1 演示报告\n\n旧演示内容'), null);
});

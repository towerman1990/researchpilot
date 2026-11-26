const metricNames = ['recall_at_1','recall_at_3','recall_at_5','mrr_at_5','p95_ms','uncovered_with_results'];
const text = value => typeof value === 'string' && value.trim().length > 0 && value.length <= 500;
const ids = value => Array.isArray(value) && value.length <= 100 && value.every(text);

export function parseRetrievalEvaluation(value) {
  if (!value || !text(value.corpus) || !text(value.query_set) ||
      !Array.isArray(value.strategies) || value.strategies.length < 1 || value.strategies.length > 8) {
    throw Error('文件不是 ResearchPilot 检索评估结果。');
  }
  const modes = new Set();
  for (const strategy of value.strategies) {
    if (!strategy || !text(strategy.mode) || modes.has(strategy.mode) ||
        !metricNames.every(key => Number.isFinite(strategy[key]) && strategy[key] >= 0) ||
        !metricNames.slice(0,4).every(key => strategy[key] <= 1) ||
        !Number.isInteger(strategy.uncovered_with_results) ||
        !Array.isArray(strategy.cases) || strategy.cases.length < 1 || strategy.cases.length > 500) {
      throw Error('检索策略或指标格式不正确。');
    }
    modes.add(strategy.mode);
    const cases = new Set();
    for (const row of strategy.cases) {
      if (!row || !text(row.id) || cases.has(row.id) || !text(row.query) || !ids(row.relevant_ids) ||
          !ids(row.result_ids) || !Number.isFinite(row.ms) || row.ms < 0) {
        throw Error('检索题目明细格式不正确。');
      }
      cases.add(row.id);
    }
  }
  return value;
}

export function retrievalOutcome(row) {
  if (row.relevant_ids.length === 0) return row.result_ids.length ? 'false_positive' : 'no_answer';
  const retrieved = new Set(row.result_ids.slice(0,5));
  return row.relevant_ids.every(id => retrieved.has(id)) ? 'covered' : 'missed';
}

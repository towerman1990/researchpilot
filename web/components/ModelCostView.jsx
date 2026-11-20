import React, {useEffect, useState} from 'react';
import {Alert, Card, Empty, InputNumber, Select, Skeleton} from 'antd';

const format = new Intl.NumberFormat('zh-CN');
const keyFor = row => JSON.stringify([row.base_url, row.model]);
const blankRates = {input:null, output:null, currency:'CNY'};
const currencies = [{value:'CNY',label:'人民币 CNY'}, {value:'USD',label:'美元 USD'}];

export function ModelCostView({read, days, revision}) {
  const [result, setResult] = useState(null);
  const [selected, setSelected] = useState('');
  const [prices, setPrices] = useState({});

  useEffect(() => {
    let active = true;
    read('/api/v1/research/usage/models?days=' + days).then(
      rows => { if (active) setResult({days, revision, rows}); },
      error => { if (active) setResult({days, revision, error}); },
    );
    return () => { active = false; };
  }, [days, revision, read]);

  const current = result?.days === days && result.revision === revision ? result : null;
  const row = current?.rows?.find(item => keyFor(item) === selected) || current?.rows?.[0];
  const rowKey = row && keyFor(row);
  const rates = prices[rowKey] || blankRates;

  function updatePrice(field, value) {
    setPrices(current => ({...current, [rowKey]:{...(current[rowKey] || blankRates), [field]:value}}));
  }

  const hasSplit = row && (row.input_tokens > 0 || row.output_tokens > 0);
  const hasRates = [rates.input, rates.output].every(rate => rate !== null && Number.isFinite(Number(rate)) && Number(rate) >= 0);
  const amount = hasSplit && hasRates ? row.input_tokens / 1000000 * Number(rates.input) + row.output_tokens / 1000000 * Number(rates.output) : null;
  const money = new Intl.NumberFormat('zh-CN', {style:'currency', currency:rates.currency, maximumFractionDigits:6});

  return <Card className="model-cost" title="个人模型成本估算">
    {!current ? <Skeleton active paragraph={{rows:2}}/>
      : current.error ? <Alert type="error" showIcon message="暂时无法读取模型分项用量"/>
      : !row ? <Empty description="此期间暂无个人模型用量"/>
      : <>
        <div className="model-cost-fields">
          <label>模型<Select value={rowKey} onChange={setSelected}
            options={current.rows.map(item => ({value:keyFor(item), label:item.model + ' · ' + item.base_url}))}/></label>
          <label>币种<Select value={rates.currency} onChange={value => updatePrice('currency', value)} options={currencies}/></label>
          <label>每百万输入 Token 价格<InputNumber min={0} stringMode value={rates.input}
            onChange={value => updatePrice('input', value)}/></label>
          <label>每百万输出 Token 价格<InputNumber min={0} stringMode value={rates.output}
            onChange={value => updatePrice('output', value)}/></label>
        </div>
        <dl className="model-cost-totals">
          <div><dt>输入 Token</dt><dd>{format.format(row.input_tokens)}</dd></div>
          <div><dt>输出 Token</dt><dd>{format.format(row.output_tokens)}</dd></div>
          <div><dt>未分项与估算 Token</dt><dd>{format.format(row.unsplit_tokens + row.estimated_tokens)}</dd></div>
          <div><dt>可计价部分估算</dt><dd role="status">{!Number.isFinite(amount) ? '—' : amount > 0 && amount < 0.000001 ? '< ' + money.format(0.000001) : money.format(amount)}</dd></div>
        </dl>
        <p className="usage-note">请输入模型的单价。估算仅包含供应商同时返回两项用量的调用，费用以供应商账单为准。</p>
      </>}
  </Card>;
}

import React, {useEffect, useState} from 'react';
import {Alert, Button, Card, Empty, Input, Select, Space, Tag} from 'antd';

const path = '/api/v1/knowledge/libraries';
const options = libraries => libraries.map(({id,name}) => ({value:id,label:name}));

export function LibrarySelect({read, value, onChange, disabled}) {
  const [libraries,setLibraries] = useState([]);
  const [error,setError] = useState('');
  useEffect(() => {
    let active = true;
    read(path).then(data => { if (active) setLibraries(data); }, e => {
      if (active) setError(e.message);
    });
    return () => { active = false; };
  }, [read]);
  return <div className="field-label">
    <label htmlFor="research-library">私有知识库（可选）</label>
    <Select id="research-library" allowClear value={value || undefined} onChange={id => onChange(id || '')}
      disabled={disabled} options={options(libraries)} placeholder="选择资料库" notFoundContent="请先在「私有知识库」中建库"/>
    {value && <small className="muted">命中的段落会发送给本次研究使用的模型服务。</small>}
    {error && <Alert type="error" message={error} showIcon/>}
  </div>;
}

export function KnowledgeView({request}) {
  const [libraries,setLibraries] = useState([]);
  const [library,setLibrary] = useState('');
  const [documents,setDocuments] = useState([]);
  const [name,setName] = useState('');
  const [error,setError] = useState('');
  const [busy,setBusy] = useState(false);
  const [checking,setChecking] = useState(false);
  const [loadingLibraries,setLoadingLibraries] = useState(true);
  const [reload,setReload] = useState(0);

  useEffect(() => {
    let active = true;
    request(path).then(data => {
      if (active) setLibraries(data);
    }, e => { if (active) setError(e.message); }).finally(() => {
      if (active) setLoadingLibraries(false);
    });
    return () => { active = false; };
  }, [request]);

  useEffect(() => {
    if (!library) return;
    let active = true;
    let timer;
    setChecking(true);
    async function refresh() {
      try {
        const rows = await request(path + '/' + library + '/documents');
        if (!active) return;
        setDocuments(rows);
        if (rows.some(row => row.status === 'QUEUED')) timer = setTimeout(refresh, 2000);
      } catch (e) {
        if (active) setError(e.message);
      } finally {
        if (active) setChecking(false);
      }
    }
    void refresh();
    return () => { active = false; clearTimeout(timer); };
  }, [library,reload,request]);

  function choose(id) {
    setLibrary(id || '');
    setDocuments([]);
    setError('');
  }

  async function create(event) {
    event.preventDefault();
    if (!name.trim() || busy) return;
    setBusy(true);
    setError('');
    try {
      const created = await request(path, {name:name.trim()});
      setLibraries(list => [...list,created]);
      setName('');
      choose(created.id);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function upload(event) {
    const file = event.target.files[0];
    event.target.value = '';
    if (!file || !library || busy) return;
    setBusy(true);
    setError('');
    try {
      if (!/\.(txt|md)$/i.test(file.name) || file.size > 24 * 1024) {
        throw Error('请选择不超过 24 KiB 的 Markdown 或 TXT 文件。');
      }
      let content;
      try {
        content = new TextDecoder('utf-8',{fatal:true}).decode(await file.arrayBuffer());
      } catch {
        throw Error('文件需使用 UTF-8 文本编码。');
      }
      await request(path + '/' + library + '/documents', {name:file.name,content});
      setReload(n => n + 1);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  return <div className="settings-page">
    <h1>私有知识库</h1>
    <p className="page-description">导入 Markdown 或 TXT，索引完成后即可在研究中选用。</p>
    <Card className="settings-card">
      <form onSubmit={create}>
        <label className="field-label" htmlFor="knowledge-name">新建资料库</label>
        <Space.Compact block>
          <Input id="knowledge-name" maxLength={120} value={name} onChange={event => setName(event.target.value)} required/>
          <Button type="primary" htmlType="submit" disabled={loadingLibraries || busy || !name.trim()}>创建</Button>
        </Space.Compact>
      </form>
      <div className="research-attachments">
        <label htmlFor="knowledge-library">资料库</label>
        <Select id="knowledge-library" options={options(libraries)} value={library || undefined}
          onChange={choose} placeholder="选择资料库" disabled={busy}/>
        {library && <>
          <label htmlFor="knowledge-file">导入文件</label>
          <input id="knowledge-file" type="file" accept=".md,.txt" onChange={upload} disabled={busy}/>
          <small className="muted">最多 24 KiB。导入同名文件会创建新版本。</small>
        </>}
      </div>
      {error && <Alert type="error" message={error} showIcon/>}
      {library && (checking ? <p role="status">正在读取文档</p> :
        documents.length ? <ul>
          {documents.map(doc => <li key={doc.id}>
            {doc.name} · 第 {doc.revision} 版 <Tag color={doc.status === 'READY' ? 'green' : 'blue'}>
              {doc.status === 'READY' ? '已索引' : '等待索引'}
            </Tag>
          </li>)}
        </ul> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无文档"/>)}
    </Card>
  </div>;
}

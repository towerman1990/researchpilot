const citationSuffix = /(?: \[\d+\])+\s*$/;
const referenceLine = /^(\d+)\. \[(.*)\]\(<([^\n]+)>\)$/;
const archivedPath = /^\/api\/v1\/research\/tasks\/[a-f0-9]{32}\/sources\/[a-f0-9]{32}$/;
const escapedText = {amp:'&',lt:'<',gt:'>'};

function plainText(value) {
  return value.replace(/\\([\[\]!()])/g, '$1')
    .replace(/&(amp|lt|gt);/g, (_, name) => escapedText[name])
    .trim();
}

export function readComparableReport(markdown) {
  const text = markdown.replaceAll('\r\n', '\n').trim();
  const heading = /^# (研究报告|部分研究成果)\n+/.exec(text);
  const limitations = text.lastIndexOf('\n## 局限\n');
  const referencesAt = text.lastIndexOf('\n## 参考资料');
  if (!heading || limitations < heading[0].length || referencesAt < limitations) return null;
  const references = new Map();
  for (const line of text.slice(referencesAt).split('\n')) {
    const reference = referenceLine.exec(line);
    if (!reference) continue;
    const [, number, rawTitle, url] = reference;
    const title = plainText(rawTitle);
    const key = archivedPath.test(url) ? 'archive:' + title : 'public:' + url;
    references.set(Number(number), {key,title,number:Number(number)});
  }
  const findings = new Map();
  let paragraph = [];
  for (const line of text.slice(heading[0].length, limitations).split('\n')) {
    const suffix = citationSuffix.exec(line);
    if (!suffix) {
      paragraph.push(line);
      continue;
    }
    paragraph.push(line.slice(0, suffix.index));
    const finding = plainText(paragraph.join('\n').trim().replace(/^- /, ''));
    const key = finding.replace(/(\p{Script=Han})\s+(?=\p{Script=Han})/gu, '$1').replace(/\s+/g, ' ');
    paragraph = [];
    if (!key) continue;
    const entry = findings.get(key) || {text:finding, references:[]};
    for (const cite of suffix[0].matchAll(/\[(\d+)\]/g)) {
      const reference = references.get(Number(cite[1]));
      if (reference && !entry.references.some(item => item.key === reference.key)) entry.references.push(reference);
    }
    findings.set(key, entry);
  }
  if (findings.size === 0 || paragraph.some(line => line.trim())) return null;
  return {partial:heading[1] === '部分研究成果',findings};
}

export function compareReports(baseline, current) {
  const added = [];
  const removed = [];
  const referencesChanged = [];
  let unchanged = 0;
  for (const [key, finding] of current.findings) {
    const previous = baseline.findings.get(key);
    if (!previous) {
      added.push(finding);
      continue;
    }
    const previousKeys = new Set(previous.references.map(reference => reference.key));
    if (previousKeys.size !== finding.references.length || finding.references.some(reference => !previousKeys.has(reference.key))) {
      referencesChanged.push({previous,current:finding});
    } else {
      unchanged++;
    }
  }
  for (const [key, finding] of baseline.findings) {
    if (!current.findings.has(key)) removed.push(finding);
  }
  return {added,removed,referencesChanged,unchanged};
}

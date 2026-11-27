import React, {useEffect, useState} from 'react';

export function SiteUsage({read, refreshKey}) {
  const [stats,setStats] = useState(null);
  useEffect(() => {
    let active = true;
    let pending = false;
    async function refresh() {
      if (pending || document.visibilityState === 'hidden') return;
      pending = true;
      try {
        const value = await read('/api/v1/site/usage');
        if (active) setStats(value);
      } catch {
        // Usage statistics are optional information on the workbench.
      } finally {
        pending = false;
      }
    }
    void refresh();
    const timer = setInterval(refresh, 60000);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener('visibilitychange', refresh);
    };
  }, [read, refreshKey]);
  if (!stats) return null;
  const count = value => Number(value).toLocaleString('zh-CN');
  return <p className="site-usage"
    title="自统计功能启用起，按成功提交的研究任务计次。登录按账号、访客按 IP 去重，今日数据按北京时间统计。">
    今日 {count(stats.today_users)} 人 · {count(stats.today_uses)} 次
    {' ｜ '}累计 {count(stats.total_users)} 人 · {count(stats.total_uses)} 次
  </p>;
}

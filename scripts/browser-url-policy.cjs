'use strict';
// Host bridge admission policy; independent from installed plugin modules.
function isAllowedHost(host, allowed) {
  const normalized = String(host || '').toLowerCase().replace(/\.$/, '');
  return allowed.some(rule => {
    const value = rule.replace(/\.$/, '');
    if (value.startsWith('*.')) return normalized.endsWith(value.slice(1)) && normalized !== value.slice(2);
    return normalized === value;
  });
}

function validateURL(raw, allowed) {
  let url;
  try { url = new URL(String(raw || '')); } catch (_) { throw new Error('请提供有效的网页 URL'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('仅允许不含账号信息的 HTTP/HTTPS 网页');
  if (!isAllowedHost(url.hostname, allowed)) throw new Error('目标网站不在浏览器插件的允许列表中');
  return url.href;
}

module.exports = { isAllowedHost, validateURL };

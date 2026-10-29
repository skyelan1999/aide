'use strict';
const {randomUUID} = require('node:crypto');

function createOwnedSessionScope(request, base, token) {
  const origin = new URL(base).origin, tag = 'aide-ui-' + randomUUID() + ':';
  const owned = new Set(), attemptedTitles = new Set(), pending = new Set(), routeErrors = [];
  let closing = false;
  async function api(method, suffix, data) {
    const response = await request.fetch(origin + '/api' + suffix, {method, headers: {Authorization: 'Bearer ' + token},
      ...(data === undefined ? {} : {data}), timeout: 10000, maxRetries: 0});
    if (response.status() === 404) return null;
    if (!response.ok()) throw Error('UI test request failed: ' + method + ' ' + suffix + ' status ' + response.status());
    return response.json();
  }
  function remember(session) {
    if (!session || typeof session.id !== 'string' || !session.id || session.kind === 'assistant') throw Error('UI test did not create an ordinary owned session');
    owned.add(session.id);return session;
  }
  async function create(label) {
    if (closing) throw Error('UI test scope is closing');
    const title = tag + label;attemptedTitles.add(title);
    return remember(await api('POST', '/sessions', {title}));
  }
  async function handle(route) {
    const req = route.request(), url = new URL(req.url()), method = req.method();
    const denied = () => route.fulfill({status: 403, contentType: 'application/json', body: JSON.stringify({error: 'UI test may only mutate its owned sessions'})});
    if (url.origin !== origin) return denied();
    const match = /^\/api\/sessions\/([^/]+)(?:\/|$)/.exec(url.pathname);
    const sessionID = match ? decodeURIComponent(match[1]) : null;
    const creating = method === 'POST' && url.pathname === '/api/sessions';
    if (method !== 'GET' && method !== 'HEAD') {
      if (closing || (!creating && (!sessionID || !owned.has(sessionID)))) return denied();
      if (creating) {
        const title = req.postDataJSON()?.title;
        if (typeof title !== 'string' || !title.startsWith(tag)) return denied();
        attemptedTitles.add(title);
      }
    }
    if (method === 'GET' && sessionID && !owned.has(sessionID)) return denied();
    const response = await route.fetch({timeout: 10000, maxRetries: 0});
    if (creating && response.ok()) remember(await response.json());
    if (method === 'GET' && response.ok() && (url.pathname === '/api/sessions' || url.pathname === '/api/export')) {
      const data = await response.json();
      if (Array.isArray(data)) return route.fulfill({response, json: data.filter(session => owned.has(session.id))});
      if (Array.isArray(data.sessions)) {
        data.sessions = data.sessions.filter(session => owned.has(session.id));data.count = data.sessions.length;
        return route.fulfill({response, json: data});
      }
      throw Error('Unexpected session list shape in UI acceptance scope');
    }
    return route.fulfill({response});
  }
  function route(route) {
    const operation = handle(route).catch(async error => {
      routeErrors.push(error);
      try {await route.fulfill({status: 502, contentType: 'application/json', body: JSON.stringify({error: 'UI test request interception failed'})});} catch (_) {}
    });pending.add(operation);
    return operation.finally(() => pending.delete(operation));
  }
  async function cleanup() {
    closing = true;
    await Promise.allSettled([...pending]);
    const failures = [];
    // A creation can commit before its response is lost. Recover only exact
    // attempted titles bearing this run's random prefix, never user sessions.
    for (const suffix of ['/sessions', '/sessions?archived=1']) {
      try {
        for (const session of await api('GET', suffix) || []) if (attemptedTitles.has(session.title)) remember(session);
      } catch (error) {failures.push(error);}
    }
    for (const id of owned) {
      const prefix = '/sessions/' + encodeURIComponent(id);
      try {
        const session = await api('GET', prefix);
        for (const run of session?.runs || []) {
          if (['completed', 'failed', 'cancelled'].includes(run.status)) continue;
          try {await api('POST', prefix + '/runs/' + encodeURIComponent(run.id) + '/cancel', {});}
          catch (error) {failures.push(error);}
        }
      } catch (error) {failures.push(error);}
      try {
        await api('DELETE', prefix);
        if (await api('GET', prefix)) throw Error('Owned UI test session still visible after deletion: ' + id);
      } catch (error) {failures.push(error);}
    }
    if (failures.length) throw new AggregateError(failures, 'UI test cleanup incomplete; inspect owned session IDs before retry');
  }
  return {tag, owned, routeErrors, create, route, cleanup, stop() {closing = true;}};
}

async function runOwnedUI({chromium, base, token, chrome, label}, check) {
  if (!token) throw Error('AIDE_TOKEN is required for UI acceptance');
  let browser, context, page, scope, failure, result;
  const cleanupErrors = [];
  try {
    browser = await chromium.launch({executablePath: chrome, headless: true, args: ['--no-sandbox']});
    context = await browser.newContext({viewport: {width: 1440, height: 900}});
    const lock = await context.request.fetch(new URL('/api/lock-state', base).href, {headers: {Authorization: 'Bearer ' + token}, timeout: 10000});
    if (!lock.ok() || (await lock.json()).locked !== false) throw Error('UI acceptance requires an already unlocked workbench; it never unlocks or changes the global lock');
    scope = createOwnedSessionScope(context.request, base, token);
    const session = await scope.create(label);
    await context.route('**/api/**', scope.route);
    await context.addInitScript(({token, tag}) => {localStorage.setItem('aide-token', token);window.__aideUITestRunTag = tag;}, {token, tag: scope.tag});
    page = await context.newPage();await page.goto(base, {waitUntil: 'load'});
    await page.evaluate(id => selectSession(id), session.id);
    await page.waitForFunction(id => typeof state !== 'undefined' && state.session?.id === id, session.id);
    result = await check({page, scope, session});
    if (scope.routeErrors.length) throw new AggregateError(scope.routeErrors, 'UI test intercepted requests failed');
  } catch (error) {failure = error;}
  finally {
    scope?.stop();
    if (page) try {await page.close();} catch (error) {cleanupErrors.push(error);}
    if (scope) try {await scope.cleanup();} catch (error) {cleanupErrors.push(error);}
    if (context) try {await context.close();} catch (error) {cleanupErrors.push(error);}
    if (browser) try {await browser.close();} catch (error) {cleanupErrors.push(error);}
  }
  if (cleanupErrors.length) throw new AggregateError([...(failure ? [failure] : []), ...cleanupErrors], 'UI acceptance failed or cleanup incomplete');
  if (failure) throw failure;
  return result;
}
module.exports = {createOwnedSessionScope, runOwnedUI};

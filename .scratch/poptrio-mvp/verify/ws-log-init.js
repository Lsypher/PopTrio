(() => {
  const log = [];
  window.__frames = log;
  const Orig = window.WebSocket;
  const Patched = function (url, protocols) {
    const ws = protocols === undefined ? new Orig(url) : new Orig(url, protocols);
    ws.addEventListener('message', (ev) => {
      if (typeof ev.data !== 'string') return;
      try { log.push({ t: Date.now(), dir: 'in', msg: JSON.parse(ev.data) }); } catch (e) { /* ignore */ }
    });
    const origSend = ws.send.bind(ws);
    ws.send = (data) => {
      try { log.push({ t: Date.now(), dir: 'out', msg: JSON.parse(data) }); } catch (e) { /* ignore */ }
      return origSend(data);
    };
    return ws;
  };
  Patched.prototype = Orig.prototype;
  for (const k of Object.getOwnPropertyNames(Orig)) {
    if (!(k in Patched)) {
      try { Patched[k] = Orig[k]; } catch (e) { /* ignore */ }
    }
  }
  window.WebSocket = Patched;
})();

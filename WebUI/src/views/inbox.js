import { api } from '../api.js'
import { h } from '../dom.js'
import { button, errorBox, header, link, loading, messageList, note } from '../ui.js'

// GET /api/inbox runs POP3 LIST + RETR for every message and saves new ones,
// so each load of this screen is one full POP3 session.
export function inbox(el) {
  const status = h('span', { class: 'font-mono text-xs text-muted' })
  const check = button('Check for new mail', load)
  const body = h('div')
  el.append(header('Inbox', status, check), body)

  async function load() {
    check.disabled = true
    status.textContent = ''
    body.replaceChildren(loading('Downloading from the POP3 server…'))
    const start = performance.now()
    try {
      const items = await api('/inbox')
      status.textContent = `${items.length} on server · ${Math.round(performance.now() - start)} ms`
      body.replaceChildren(items.length
        ? messageList(items.toReversed(), (m) => `#/message/${m.num}`, (m) => m.num)
        : note('No messages on the server. ', link('#/compose', 'Send one'), ' to see it arrive here.'))
    } catch (err) {
      body.replaceChildren(errorBox(err, load))
    } finally {
      check.disabled = false
    }
  }

  load()
}

import { api } from '../api.js'
import { h } from '../dom.js'
import { backLink, errorBox, header, link, loading, messageList, messageView, note, setTitle } from '../ui.js'

// GET /api/local reads data/mailbox.json through the Go server. No mail server involved.
export async function saved(el) {
  const count = h('span', { class: 'font-mono text-xs text-muted' })
  const body = h('div', {}, loading('Loading saved messages…'))
  el.append(header('Saved', count), body)

  try {
    const items = await api('/local')
    count.textContent = `${items.length} saved`
    body.replaceChildren(items.length
      ? messageList(items.toReversed(), (m) => `#/saved/${m.id}`, (m) => m.id)
      : note('Nothing saved yet. Opening the ', link('#/inbox', 'Inbox'), ' downloads every message and saves it here.'))
  } catch (err) {
    body.replaceChildren(errorBox(err))
  }
}

export async function savedMessage(el, id) {
  setTitle(`Saved message ${id}`)
  const body = h('div', {}, loading('Loading…'))
  el.append(backLink('#/saved', 'Saved'), body)

  try {
    const m = (await api('/local')).find((x) => x.id === Number(id))
    body.replaceChildren(m
      ? messageView(m, `Saved copy #${m.id}, read from this computer without contacting the server.`)
      : errorBox(new Error(`No saved message with ID ${id}.`)))
  } catch (err) {
    body.replaceChildren(errorBox(err))
  }
}

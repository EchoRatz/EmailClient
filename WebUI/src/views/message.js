import { api } from '../api.js'
import { h } from '../dom.js'
import { backLink, errorBox, link, loading, messageView, setTitle } from '../ui.js'

// GET /api/messages/{num} does a fresh POP3 RETR for this one message.
export function message(el, num) {
  setTitle(`Message ${num}`)
  const body = h('div')
  el.append(backLink('#/inbox', 'Inbox'), body)

  async function load() {
    body.replaceChildren(loading(`Retrieving message ${num} from the POP3 server…`))
    try {
      const m = await api(`/messages/${num}`)
      body.replaceChildren(messageView(m,
        `POP3 message ${num}. Saved on this computer as `, link(`#/saved/${m.id}`, `#${m.id}`), '.'))
    } catch (err) {
      body.replaceChildren(errorBox(err, load))
    }
  }

  load()
}

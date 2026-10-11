import { api } from './api.js'
import { h } from './dom.js'

function socket(label, s) {
  const up = s.status === 'up'
  return h('span', { title: s.error ?? '' },
    `${label} ${s.addr} `,
    h('span', { class: up ? 'text-ok' : 'text-bad' }, up ? '● up' : '● down'),
  )
}

// Each check opens a real SMTP and POP3 connection, so it runs on page load,
// when the tab comes back into view, on click, and after a failed request,
// not on a timer.
export function watchHealth(bar) {
  async function check() {
    bar.replaceChildren('Checking connection…')
    try {
      const s = await api('/health')
      bar.replaceChildren(
        socket('SMTP', s.smtp),
        socket('POP3', s.pop3),
        h('span', { class: 'ml-auto hidden sm:inline' }, `checked ${new Date().toLocaleTimeString()}`),
      )
    } catch (err) {
      bar.replaceChildren(h('span', { class: 'text-bad' }, err.message))
    }
  }

  bar.addEventListener('click', check)
  document.addEventListener('mail:recheck', check)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') check()
  })
  check()
}

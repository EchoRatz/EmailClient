import { api } from './api.js'
import { h } from './dom.js'

const MAX_BYTES = 2 * 1024 * 1024
let cleanup = null

// The sidebar identity: round picture, "Mail", the address. The picture is a
// button that opens a small menu with Change picture and Sign out.
export function mountProfile(el, me, { onSignOut }) {
  cleanup?.abort()
  cleanup = new AbortController()
  const { signal } = cleanup

  const face = h('button', {
    type: 'button',
    class: 'grid size-10 shrink-0 place-items-center overflow-hidden rounded-full bg-raised ring-1 ring-line hover:ring-accent',
    'aria-expanded': 'false',
    'aria-controls': 'profile-menu',
    'aria-label': `Account menu for ${me.email}`,
  })
  const file = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/gif,image/webp', class: 'hidden' })
  const message = h('p', { class: 'px-3 py-1 text-xs text-bad', role: 'alert' })
  const menu = h('div', {
    id: 'profile-menu',
    class: 'absolute left-0 top-full z-30 mt-2 w-52 rounded-md border border-line bg-surface p-1 shadow-[var(--lift)]',
    hidden: '',
  },
    h('p', { class: 'truncate px-3 py-1.5 font-mono text-xs text-muted' }, me.email),
    h('button', { type: 'button', class: 'menu-item', onClick: () => file.click() }, 'Change picture'),
    h('button', { type: 'button', class: 'menu-item', onClick: onSignOut }, 'Sign out'),
    message,
  )

  function drawFace() {
    face.replaceChildren(me.has_avatar
      ? h('img', { src: `/api/avatar?v=${Date.now()}`, alt: '', class: 'size-full object-cover' })
      : h('span', { class: 'text-sm font-semibold text-accent', 'aria-hidden': 'true' }, me.email[0].toUpperCase()))
  }

  function setOpen(open) {
    menu.hidden = !open
    face.setAttribute('aria-expanded', String(open))
    if (!open) message.textContent = ''
  }

  face.addEventListener('click', () => setOpen(menu.hidden))
  document.addEventListener('click', (e) => { if (!el.contains(e.target)) setOpen(false) }, { signal })
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !menu.hidden) {
      setOpen(false)
      face.focus()
    }
  }, { signal })

  file.addEventListener('change', async () => {
    const picked = file.files[0]
    file.value = ''
    if (!picked) return
    if (picked.size > MAX_BYTES) {
      message.textContent = 'Pictures must be 2 MB or smaller.'
      return
    }
    try {
      me = await api('/avatar', { method: 'PUT', body: picked })
      drawFace()
      setOpen(false)
    } catch (err) {
      message.textContent = err.message
    }
  })

  drawFace()
  el.replaceChildren(
    h('div', { class: 'flex items-center gap-3' },
      face,
      h('div', { class: 'min-w-0' },
        h('p', { class: 'text-sm font-semibold' }, 'Mail'),
        h('p', { class: 'truncate font-mono text-xs text-muted' }, me.email),
      ),
    ),
    menu,
    file,
  )
}

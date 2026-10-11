import { h } from './dom.js'

export function setTitle(title) {
  document.title = `${title} · Mail`
}

export function header(title, ...actions) {
  setTitle(title)
  return h('div', { class: 'mb-5 flex flex-wrap items-center justify-between gap-3' },
    h('h1', { class: 'text-lg font-semibold' }, title),
    actions.length > 0 && h('div', { class: 'flex items-center gap-3' }, ...actions),
  )
}

export function note(...children) {
  return h('p', { class: 'text-sm text-muted' }, ...children)
}

export function link(href, text) {
  return h('a', { href, class: 'text-accent hover:underline' }, text)
}

export function backLink(href, text) {
  return h('a', { href, class: 'mb-4 inline-block text-sm text-accent hover:underline' }, `← ${text}`)
}

export function button(label, onClick, { primary = false, type = 'button' } = {}) {
  const look = primary
    ? 'bg-accent text-bg hover:brightness-110'
    : 'border border-line text-fg hover:bg-surface'
  return h('button', {
    type,
    class: `rounded-md px-3 py-1.5 text-sm font-semibold disabled:cursor-wait disabled:opacity-50 ${look}`,
    ...(onClick && { onClick }),
  }, label)
}

export function loading(text) {
  return h('p', { class: 'text-sm text-muted', role: 'status' }, text)
}

// Network failures also re-run the status bar check, so it stops saying "up".
export function errorBox(err, retry) {
  if (err.kind && err.kind !== 'invalid_input') document.dispatchEvent(new Event('mail:recheck'))
  return h('div', { class: 'rounded-md border border-bad/40 bg-bad/10 p-4 text-sm', role: 'alert' },
    h('p', { class: 'font-semibold text-bad' }, err.message),
    err.detail && h('details', { class: 'mt-2 text-muted' },
      h('summary', { class: 'cursor-pointer' }, 'Technical detail'),
      h('pre', { class: 'mt-2 whitespace-pre-wrap break-words font-mono text-xs' }, err.detail),
    ),
    retry && h('div', { class: 'mt-3' }, button('Try again', retry)),
  )
}

// Go sends a missing Date header as year 1.
function parseDate(iso) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) || d.getFullYear() < 1971 ? null : d
}

export function shortDate(iso) {
  const d = parseDate(iso)
  if (!d) return '—'
  const now = new Date()
  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString([], {
    month: 'short', day: 'numeric',
    ...(d.getFullYear() !== now.getFullYear() && { year: 'numeric' }),
  })
}

export function longDate(iso) {
  const d = parseDate(iso)
  return d ? d.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : 'unknown'
}

function subjectText(s) {
  return s || h('span', { class: 'italic' }, '(no subject)')
}

// label(m) is the number shown in the first column: POP3 number or local ID.
export function messageList(items, href, label) {
  return h('ul', { class: 'divide-y divide-line overflow-hidden rounded-md border border-line' },
    items.map((m) => h('li', {},
      h('a', {
        href: href(m),
        class: 'grid grid-cols-[1fr_auto] gap-x-4 gap-y-0.5 px-4 py-2.5 hover:bg-surface md:grid-cols-[3rem_14rem_1fr_auto] md:items-baseline',
      },
        h('span', { class: 'hidden font-mono text-xs text-muted md:block' }, String(label(m))),
        h('span', { class: 'truncate text-sm' }, m.from || '(unknown sender)'),
        h('span', { class: 'col-span-2 row-start-2 truncate text-sm text-muted md:col-span-1 md:row-start-auto md:text-fg' }, subjectText(m.subject)),
        h('span', { class: 'whitespace-nowrap text-right font-mono text-xs text-muted' }, shortDate(m.date)),
      ),
    )),
  )
}

function field(name, value) {
  return [
    h('dt', { class: 'text-muted' }, name),
    h('dd', { class: 'min-w-0 break-words' }, value),
  ]
}

export function messageView(m, ...footer) {
  setTitle(m.subject || '(no subject)')
  return h('article', {},
    h('h1', { class: 'mb-3 text-lg font-semibold break-words' }, subjectText(m.subject)),
    h('dl', { class: 'mb-5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm' },
      field('From', m.from),
      field('To', m.to),
      field('Date', longDate(m.date)),
    ),
    h('pre', { class: 'whitespace-pre-wrap break-words border-t border-line pt-5 font-mono text-sm leading-relaxed' }, m.body),
    footer.length > 0 && h('div', { class: 'mt-6 border-t border-line pt-4 text-sm text-muted' }, ...footer),
  )
}

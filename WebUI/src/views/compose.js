import { api } from '../api.js'
import { h } from '../dom.js'
import { button, errorBox, header, link } from '../ui.js'

const inputClass = 'w-full rounded-md border border-line bg-surface px-3 py-2 text-sm placeholder:text-muted focus:border-accent'

function labelled(id, text, control) {
  return h('div', { class: 'grid gap-1.5' },
    h('label', { for: id, class: 'text-sm text-muted' }, text),
    control,
  )
}

export function compose(el) {
  const to = h('input', { id: 'to', class: inputClass, autocomplete: 'off', required: '', placeholder: 'bob@localhost, carol@localhost' })
  const subject = h('input', { id: 'subject', class: inputClass, autocomplete: 'off' })
  const text = h('textarea', { id: 'body', class: `${inputClass} font-mono leading-relaxed`, rows: '12' })
  const send = button('Send', null, { primary: true, type: 'submit' })
  const result = h('div', { 'aria-live': 'polite' })

  const form = h('form', { class: 'grid max-w-2xl gap-4', onSubmit: submit },
    labelled('to', 'To (separate several with commas)', to),
    labelled('subject', 'Subject', subject),
    labelled('body', 'Message', text),
    h('div', {}, send),
    result,
  )
  el.append(header('Compose'), form)
  to.focus()

  async function submit(event) {
    event.preventDefault()
    send.disabled = true
    send.textContent = 'Sending…'
    result.replaceChildren()
    try {
      const r = await api('/send', {
        method: 'POST',
        body: {
          to: to.value.split(',').map((s) => s.trim()).filter(Boolean),
          subject: subject.value,
          body: text.value,
        },
      })
      form.reset()
      result.replaceChildren(h('p', { class: 'text-sm text-ok' },
        `Sent to ${r.sent_to.join(', ')}. SMTP session took ${r.ms} ms. `,
        link('#/inbox', 'Open Inbox'),
      ))
      to.focus()
    } catch (err) {
      result.replaceChildren(errorBox(err))
    } finally {
      send.disabled = false
      send.textContent = 'Send'
    }
  }
}

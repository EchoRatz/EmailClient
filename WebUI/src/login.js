import { api } from './api.js'
import { h } from './dom.js'
import { button, errorBox, setTitle } from './ui.js'

const inputClass = 'w-full rounded-md border border-line bg-bg px-3 py-2 text-sm placeholder:text-muted focus:border-accent'

// The sign-in card, drawn as an airmail envelope. notice is a line shown above
// the form (e.g. after signing out); problem is an error to show at once
// (e.g. the API is not running).
export function showLogin(container, { onSignedIn, notice, problem }) {
  setTitle('Sign in')
  const email = h('input', { id: 'email', type: 'email', class: inputClass, autocomplete: 'username', required: '', placeholder: 'alice@localhost' })
  const password = h('input', { id: 'password', type: 'password', class: inputClass, autocomplete: 'current-password', required: '' })
  const submit = button('Sign in', null, { primary: true, type: 'submit' })
  const result = h('div', { 'aria-live': 'polite' }, problem && errorBox(problem))

  const form = h('form', { class: 'w-full max-w-sm overflow-hidden rounded-lg border border-line bg-surface shadow-[var(--lift)]', onSubmit: signIn },
    h('div', { class: 'airmail', 'aria-hidden': 'true' }),
    h('div', { class: 'grid gap-4 p-6' },
      h('div', {},
        h('h1', { class: 'text-lg font-semibold' }, 'Sign in to Mail'),
        h('p', { class: 'mt-1 text-sm text-muted' }, 'Use your mail account. The password is checked with the POP3 server.'),
      ),
      notice && h('p', { class: 'text-sm text-ok', role: 'status' }, notice),
      h('div', { class: 'grid gap-1.5' }, h('label', { for: 'email', class: 'text-sm text-muted' }, 'Email address'), email),
      h('div', { class: 'grid gap-1.5' }, h('label', { for: 'password', class: 'text-sm text-muted' }, 'Password'), password),
      result,
      submit,
    ),
  )
  container.replaceChildren(form)
  email.focus()

  async function signIn(event) {
    event.preventDefault()
    submit.disabled = true
    submit.textContent = 'Signing in…'
    result.replaceChildren()
    try {
      onSignedIn(await api('/login', { method: 'POST', body: { email: email.value, password: password.value } }))
    } catch (err) {
      result.replaceChildren(errorBox(err))
      password.value = ''
      password.focus()
    } finally {
      submit.disabled = false
      submit.textContent = 'Sign in'
    }
  }
}

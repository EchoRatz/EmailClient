import './style.css'
import { api } from './api.js'
import { startRouter } from './router.js'
import { watchHealth } from './health.js'
import { mountThemeToggle } from './theme.js'
import { showLogin } from './login.js'
import { mountProfile } from './profile.js'
import { inbox } from './views/inbox.js'
import { message } from './views/message.js'
import { compose } from './views/compose.js'
import { saved, savedMessage } from './views/saved.js'
import { notFound } from './views/not-found.js'

const routes = [
  { pattern: /^\/inbox$/, nav: 'inbox', render: inbox },
  { pattern: /^\/message\/(\d+)$/, nav: 'inbox', render: message },
  { pattern: /^\/compose$/, nav: 'compose', render: compose },
  { pattern: /^\/saved$/, nav: 'saved', render: saved },
  { pattern: /^\/saved\/(\d+)$/, nav: 'saved', render: savedMessage },
]

const app = document.getElementById('app')
const login = document.getElementById('login')
const view = document.getElementById('view')
let render = null

function showApp(me) {
  login.hidden = true
  login.replaceChildren()
  app.hidden = false
  mountProfile(document.getElementById('profile'), me, { onSignOut: signOut })
  if (render) render(false)
  else render = startRouter(routes, view, notFound)
}

// Clearing the view matters: the next person to sign in must not see the
// previous account's messages, even for a moment.
function showSignIn(options = {}) {
  app.hidden = true
  view.replaceChildren()
  login.hidden = false
  showLogin(login, { onSignedIn: showApp, ...options })
}

async function signOut() {
  await api('/logout', { method: 'POST' }).catch(() => {})
  showSignIn({ notice: 'You signed out.' })
}

// Several requests can fail with 401 at once; redraw the form only once.
document.addEventListener('mail:signedout', () => {
  if (login.hidden) showSignIn({ notice: 'Your session ended. Sign in again.' })
})

mountThemeToggle()
watchHealth(document.getElementById('status'))

try {
  showApp(await api('/me'))
} catch (err) {
  showSignIn(err.kind === 'unauthenticated' ? {} : { problem: err })
}

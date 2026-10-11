// Hash routes (#/inbox, #/message/3) need no server support: the Go file
// server only ever sees "/", and the browser handles the rest.
export function startRouter(routes, view, fallback) {
  function render(moveFocus) {
    const path = location.hash.slice(1) || '/inbox'
    let match = null
    const route = routes.find((r) => (match = path.match(r.pattern)))

    document.querySelectorAll('[data-nav]').forEach((a) => {
      if (route && a.dataset.nav === route.nav) a.setAttribute('aria-current', 'page')
      else a.removeAttribute('aria-current')
    })

    // A fresh container per visit: a slow view that finishes after the user
    // has moved on writes into a detached element and changes nothing.
    const el = document.createElement('div')
    view.replaceChildren(el)
    if (moveFocus) view.focus()
    if (route) route.render(el, ...match.slice(1))
    else fallback(el, path)
  }

  window.addEventListener('hashchange', () => render(true))
  render(false)
  return render
}

// Light / dark, ported from the ParkingRecog ThemeToggle + lib/theme.ts.
// Stored preference is "system" | "light" | "dark"; what is painted is only
// ever "light" or "dark". A fresh browser follows the OS; the first press
// writes an explicit choice that outranks the OS from then on.
// The inline script in index.html paints the stored theme before first paint;
// keep its key ("theme") and fallback ("dark") in step with this file.

const KEY = 'theme'
const FADE_MS = 320

const MOON = '<svg viewBox="0 0 16 16" width="19" height="19" aria-hidden="true" focusable="false"><path d="M13.3 9.9A5.9 5.9 0 0 1 6.1 2.7a5.9 5.9 0 1 0 7.2 7.2z"/></svg>'
const SUN = '<svg viewBox="0 0 16 16" width="19" height="19" aria-hidden="true" focusable="false"><circle cx="8" cy="8" r="3.1"/><path class="rays" d="M8 1.1v1.7M8 13.2v1.7M1.1 8h1.7M13.2 8h1.7M3.2 3.2l1.2 1.2M11.6 11.6l1.2 1.2M12.8 3.2l-1.2 1.2M4.4 11.6l-1.2 1.2"/></svg>'

const lightQuery = window.matchMedia('(prefers-color-scheme: light)')

function stored() {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'light' || v === 'dark' || v === 'system' ? v : 'system'
  } catch {
    return 'system'
  }
}

let preference = stored()

const resolve = (t) => (t === 'system' ? (lightQuery.matches ? 'light' : 'dark') : t)
const showing = () => document.documentElement.dataset.theme

function paint(theme) {
  document.documentElement.dataset.theme = theme
  document.documentElement.style.colorScheme = theme
}

// The cross-fade is switched on only for the moment of a switch: a standing
// transition on every element would also slow hovers and loading states.
let disarm
function armFade() {
  const el = document.documentElement
  el.setAttribute('data-theme-anim', '')
  clearTimeout(disarm)
  disarm = setTimeout(() => el.removeAttribute('data-theme-anim'), FADE_MS + 60)
}

// update() paints the new theme. With View Transitions the browser snapshots
// the old frame, runs update, and reveals the new frame in a circle growing
// from origin. Without them the colours cross-fade; with reduced motion they
// just change.
function apply(update, origin) {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return update()
  if (!document.startViewTransition) {
    armFade()
    return update()
  }
  const { innerWidth: w, innerHeight: h } = window
  const radius = Math.hypot(Math.max(origin.x, w - origin.x), Math.max(origin.y, h - origin.y))
  const style = document.documentElement.style
  style.setProperty('--vt-x', `${origin.x}px`)
  style.setProperty('--vt-y', `${origin.y}px`)
  style.setProperty('--vt-r', `${Math.ceil(radius)}px`)
  document.startViewTransition(update)
}

export function mountThemeToggle() {
  const btn = document.createElement('button')
  btn.type = 'button'
  btn.className = 'theme-fab'
  btn.setAttribute('aria-label', 'Dark mode')
  btn.title = 'Dark mode'

  // The icon shows the theme you are in; aria-pressed says whether dark is on.
  function render() {
    const dark = showing() === 'dark'
    btn.setAttribute('aria-pressed', String(dark))
    btn.innerHTML = dark ? MOON : SUN
  }

  btn.addEventListener('click', () => {
    preference = showing() === 'dark' ? 'light' : 'dark'
    try {
      localStorage.setItem(KEY, preference)
    } catch {
      // not saved; this page still switches
    }
    // The element's centre, not the pointer: Enter/Space report 0,0.
    const box = btn.getBoundingClientRect()
    apply(() => {
      paint(preference)
      render()
    }, { x: box.left + box.width / 2, y: box.top + box.height / 2 })
  })

  lightQuery.addEventListener('change', () => {
    if (preference !== 'system') return
    paint(resolve('system'))
    render()
  })

  if (showing() !== resolve(preference)) paint(resolve(preference))
  render()
  document.body.append(btn)
}

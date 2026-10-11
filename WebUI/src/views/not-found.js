import { header, link, note } from '../ui.js'

export function notFound(el, path) {
  el.append(header('Page not found'), note(`Nothing lives at ${path}. `, link('#/inbox', 'Go to Inbox')))
}

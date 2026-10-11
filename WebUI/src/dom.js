// h('a', { href: '#/x', class: 'link' }, 'text', child) builds an element.
// Strings become text nodes, so message content can never inject HTML.
export function h(tag, props = {}, ...children) {
  const el = document.createElement(tag)
  for (const [key, value] of Object.entries(props)) {
    if (key.startsWith('on')) el.addEventListener(key.slice(2).toLowerCase(), value)
    else if (key === 'class') el.className = value
    else el.setAttribute(key, value)
  }
  el.append(...children.flat().filter((c) => c != null && c !== false))
  return el
}

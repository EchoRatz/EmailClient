export class ApiError extends Error {
  constructor(message, kind, detail) {
    super(message)
    this.kind = kind
    this.detail = detail
  }
}

const offline = () =>
  new ApiError('The client API is not running. Start it with: cd Client && go run ./cmd/serve', 'api_down')

// api('/send', { method: 'POST', body: {...} }) returns the parsed JSON,
// or throws ApiError carrying the Go server's {error, kind, detail}.
// A File or Blob body is sent as is (picture upload); anything else as JSON.
export async function api(path, { method = 'GET', body } = {}) {
  const raw = body instanceof Blob
  let res
  try {
    res = await fetch('/api' + path, {
      method,
      headers: body && !raw ? { 'Content-Type': 'application/json' } : undefined,
      body: body && !raw ? JSON.stringify(body) : body,
    })
  } catch {
    throw offline()
  }

  const data = await res.json().catch(() => null)
  if (res.ok) return data
  if (!data) throw offline()

  // The session ended (expired, or signed out in another tab). /login and /me
  // report 401 as their normal answer, so they do not raise the event.
  if (res.status === 401 && path !== '/login' && path !== '/me') {
    document.dispatchEvent(new Event('mail:signedout'))
  }
  throw new ApiError(data.error, data.kind, data.detail)
}

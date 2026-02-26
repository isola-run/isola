export class ApiError extends Error {
  status: number
  statusText: string
  detail: string

  constructor(status: number, statusText: string, detail: string) {
    super(`${status} ${statusText}: ${detail}`)
    this.name = 'ApiError'
    this.status = status
    this.statusText = statusText
    this.detail = detail
  }

  static async fromResponse(res: Response): Promise<ApiError> {
    let detail = ''
    try {
      const body = await res.json()
      detail = body.detail ?? body.title ?? JSON.stringify(body)
    } catch {
      detail = await res.text().catch(() => '')
    }
    return new ApiError(res.status, res.statusText, detail)
  }

  get isNotFound(): boolean { return this.status === 404 }
  get isBadRequest(): boolean { return this.status === 400 }
  get isConflict(): boolean { return this.status === 409 }
  get isServerError(): boolean { return this.status >= 500 }
}

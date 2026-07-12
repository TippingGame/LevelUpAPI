export interface AgentExperienceModel {
  id: string
  owned_by?: string
  display_name?: string
}

export interface AgentExperienceMessage {
  role: 'user' | 'assistant'
  content: string
}

interface ModelsResponse {
  data?: AgentExperienceModel[]
}

interface StreamChatOptions {
  apiKey: string
  model: string
  messages: AgentExperienceMessage[]
  signal?: AbortSignal
  onDelta: (delta: string) => void
}

const MODELS_ENDPOINT = '/v1/models'
const CHAT_ENDPOINT = '/v1/chat/completions'

async function readGatewayError(response: Response): Promise<string> {
  const fallback = `Request failed (${response.status})`
  try {
    const data = await response.json() as {
      error?: string | { message?: string }
      message?: string
      detail?: string
    }
    if (typeof data.error === 'string' && data.error.trim()) return data.error
    if (typeof data.error === 'object' && data.error?.message) return data.error.message
    return data.message || data.detail || fallback
  } catch {
    return fallback
  }
}

export async function fetchAgentExperienceModels(
  apiKey: string,
  signal?: AbortSignal,
): Promise<AgentExperienceModel[]> {
  const response = await fetch(MODELS_ENDPOINT, {
    headers: { Authorization: `Bearer ${apiKey}` },
    signal,
  })
  if (!response.ok) throw new Error(await readGatewayError(response))

  const payload = await response.json() as ModelsResponse
  const uniqueModels = new Map<string, AgentExperienceModel>()
  for (const model of payload.data ?? []) {
    const id = typeof model.id === 'string' ? model.id.trim() : ''
    if (id && !uniqueModels.has(id)) uniqueModels.set(id, { ...model, id })
  }
  return Array.from(uniqueModels.values()).sort((left, right) => left.id.localeCompare(right.id))
}

function extractStreamDelta(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const choices = (payload as { choices?: unknown }).choices
  if (!Array.isArray(choices)) return ''

  return choices.map((choice) => {
    if (!choice || typeof choice !== 'object') return ''
    const delta = (choice as { delta?: unknown }).delta
    if (!delta || typeof delta !== 'object') return ''
    const content = (delta as { content?: unknown }).content
    if (typeof content === 'string') return content
    if (!Array.isArray(content)) return ''
    return content.map((part) => {
      if (!part || typeof part !== 'object') return ''
      const text = (part as { text?: unknown }).text
      return typeof text === 'string' ? text : ''
    }).join('')
  }).join('')
}

function consumeSSEBlock(block: string, onDelta: (delta: string) => void): boolean {
  const data = block
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trimStart())
    .join('\n')

  if (!data) return false
  if (data.trim() === '[DONE]') return true

  try {
    const payload = JSON.parse(data) as {
      error?: string | { message?: string }
      message?: string
    }
    if (payload.error) {
      const message = typeof payload.error === 'string' ? payload.error : payload.error.message
      throw new Error(message || payload.message || 'The model request failed')
    }
    const delta = extractStreamDelta(payload)
    if (delta) onDelta(delta)
  } catch (error) {
    if (error instanceof Error && !error.name.includes('Syntax')) throw error
    // Ignore keep-alive or malformed individual SSE events without ending the stream.
  }
  return false
}

export async function streamAgentExperienceChat(options: StreamChatOptions): Promise<void> {
  const response = await fetch(CHAT_ENDPOINT, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${options.apiKey}`,
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
    },
    body: JSON.stringify({
      model: options.model,
      messages: options.messages,
      stream: true,
    }),
    signal: options.signal,
  })
  if (!response.ok) throw new Error(await readGatewayError(response))
  if (!response.body) throw new Error('Streaming response is unavailable')

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false

  while (!completed) {
    const { done, value } = await reader.read()
    buffer += decoder.decode(value, { stream: !done })

    const blocks = buffer.split(/\r?\n\r?\n/)
    buffer = blocks.pop() ?? ''
    for (const block of blocks) {
      if (consumeSSEBlock(block, options.onDelta)) {
        completed = true
        break
      }
    }
    if (done) break
  }

  if (!completed && buffer.trim()) consumeSSEBlock(buffer, options.onDelta)
}
